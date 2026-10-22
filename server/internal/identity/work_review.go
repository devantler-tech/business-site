package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const workHistorySize = 20

type workReviewState string

const (
	workAbsent   workReviewState = "absent"
	workCurrent  workReviewState = "current"
	workPaused   workReviewState = "paused"
	workExpired  workReviewState = "expired"
	workSelected workReviewState = "selected"
)

type workHistoryItem struct {
	ID, Acceptance, Terminal string
	Invoice, Version         int
	Decided, Expires         time.Time
	Ended                    *time.Time
}

type workCandidate struct {
	Evidence workEvidence
	Input    workApprovalInput
	Funding  creditHistory
}

type workReviewData struct {
	Request, OperatorClient string
	Pointer                 workPointer
	Current                 workApprovalReceipt
	State                   workReviewState
	CanReview               bool
	History                 []workHistoryItem
	Next                    triageCursor
	Invoices                []projectInvoice
	Candidate               *workCandidate
}

// readWorkHistory is a bounded, descending keyset read. A storage/scan failure
// never becomes an empty history; original database instants and IDs break ties.
func readWorkHistory(ctx context.Context, tx pgx.Tx, id, client string, cursor triageCursor) ([]workHistoryItem, error) {
	rows, err := tx.Query(ctx, `SELECT a.id,a.acceptance_id,a.invoice_sequence,a.proposal_version,a.decided_at,a.expires_at,COALESCE(r.reason,''),r.recorded_at
 FROM portal_work_approvals a LEFT JOIN portal_work_revocations r ON r.approval_id=a.id AND r.request_id=a.request_id AND r.client_id=a.client_id
 WHERE a.request_id=$1 AND a.client_id=$2 AND ($3='' OR (a.decided_at,a.id COLLATE "C") < ($4::timestamptz,$3 COLLATE "C"))
 ORDER BY a.decided_at DESC,a.id COLLATE "C" DESC LIMIT 21`, id, client, cursor.ID, cursor.Submitted)
	if err != nil {
		return nil, errTriageStorage
	}
	defer rows.Close()
	var history []workHistoryItem
	for rows.Next() {
		var item workHistoryItem
		if err := rows.Scan(&item.ID, &item.Acceptance, &item.Invoice, &item.Version, &item.Decided, &item.Expires, &item.Terminal, &item.Ended); err != nil {
			return nil, errTriageStorage
		}
		history = append(history, item)
	}
	if rows.Err() != nil {
		return nil, errTriageStorage
	}
	return history, nil
}

// readWorkReview keeps history/revocation available without old funding gates.
// Overview validates the selected receipt through the existing policy. A fresh
// invoice review instead captures candidate evidence under the same request lock;
// it deliberately does not claim to have revalidated an older selected decision.
// Each path takes a single globally ordered membership set, never two sets in
// different orders across requests.
func (s *Foundation) readWorkReview(ctx context.Context, p Principal, id string, invoice int, cursor triageCursor) (workReviewData, error) {
	return s.readWorkReviewChecked(ctx, p, id, invoice, cursor, nil)
}

// readWorkReviewChecked retains one evidence transaction through the caller's
// final private-response checkpoint, including overview and candidate reads.
func (s *Foundation) readWorkReviewChecked(ctx context.Context, p Principal, id string, invoice int, cursor triageCursor, checkpoint func(context.Context, pgx.Tx, workReviewData) error) (workReviewData, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, client, viewer, err := s.beginWorkRequest(ctx, p, id, s.workApprovalGate)
	if err != nil {
		return workReviewData{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	page := workReviewData{Request: id, OperatorClient: viewer.Client, State: workAbsent, CanReview: s.approvalsReady(ctx)}
	page.Pointer, err = readWorkPointer(ctx, tx, id, client)
	if err != nil {
		return workReviewData{}, err
	}
	page.History, err = readWorkHistory(ctx, tx, id, client, cursor)
	if err != nil {
		return workReviewData{}, err
	}
	if len(page.History) > workHistorySize {
		last := page.History[workHistorySize-1]
		page.Next = triageCursor{Submitted: last.Decided, ID: last.ID}
		page.History = page.History[:workHistorySize]
	}
	if page.CanReview {
		page.Invoices, err = readWorkInvoices(ctx, tx, id, client)
		if err != nil {
			return workReviewData{}, err
		}
	}
	deadline := time.Time{}
	switch {
	case invoice == 0 && page.CanReview:
		var valid bool
		page.Current, valid, err = inspectWorkCurrent(ctx, tx, id, client, page.Pointer, viewer)
		if err != nil {
			return workReviewData{}, err
		}
		if valid {
			page.State, deadline = workCurrent, page.Current.Expires
		} else if page.Current.ID != "" {
			page.State = workPaused
			var at time.Time
			if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
				return workReviewData{}, errTriageStorage
			}
			if !workDeadlineValid(at, page.Current.Expires) {
				page.State = workExpired
			}
		}
	default:
		if page.Pointer.ID != "" {
			page.Current, err = scanWorkApproval(tx.QueryRow(ctx, `SELECT `+workApprovalColumns+` FROM portal_work_approvals WHERE id=$1 AND request_id=$2 AND client_id=$3`, page.Pointer.ID, id, client))
			if err != nil {
				return workReviewData{}, errTriageStorage
			}
			page.State = workSelected
			if !page.CanReview {
				page.State = workPaused
			}
		}
		if invoice > 0 && page.CanReview {
			evidence, _, hash, evidenceErr := collectWorkEvidence(ctx, tx, id, client, invoice, viewer, viewer)
			if evidenceErr != nil && !errors.Is(evidenceErr, errProposalConflict) {
				return workReviewData{}, evidenceErr
			}
			if evidenceErr == nil {
				funding, err := readCreditHistory(ctx, tx, id, client, invoice)
				if err != nil {
					return workReviewData{}, err
				}
				page.Candidate = &workCandidate{Evidence: evidence, Funding: funding, Input: workApprovalInput{Key: secret(), Acceptance: evidence.Acceptance, EvidenceHash: hash, Invoice: invoice, Expected: page.Pointer.Generation}}
			}
		}
		if err := requireWorkViewer(ctx, tx, viewer); err != nil {
			return workReviewData{}, err
		}
	}
	gate := s.workApprovalGate
	if page.CanReview {
		gate = s.approvalsReady
	}
	var final func(context.Context) error
	if checkpoint != nil {
		final = func(ctx context.Context) error { return checkpoint(ctx, tx, page) }
	}
	if err := s.commitWorkChecked(ctx, p, tx, gate, deadline, final); err != nil {
		return workReviewData{}, err
	}
	return page, nil
}
