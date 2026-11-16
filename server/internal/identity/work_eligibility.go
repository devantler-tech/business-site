package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

type workEligibilityState string

const (
	eligibilityApprovalRequired    workEligibilityState = "approval-required"
	eligibilityApprovalPaused      workEligibilityState = "approval-paused"
	eligibilityConfirmationPending workEligibilityState = "confirmation-pending"
	eligibilityClosed              workEligibilityState = "closed"
	eligibilityEligible            workEligibilityState = "eligible"
)

// A workEligibilityItem is one timestamped observation. It grants no lease and
// makes no promise that a later request will find the same evidence current.
type workEligibilityItem struct {
	Request, Approval string
	Generation        int64
	Submitted         time.Time
	ObservedAt        time.Time
	State             workEligibilityState
}

type workEligibilityPage struct {
	Items []workEligibilityItem
	Next  triageCursor
}

// This access and operations gate remains default-off independently of the
// approval interface. It grants neither machine access nor execution rights.
func (s *Foundation) workEligibilityReady(ctx context.Context) bool {
	if !s.approvalsReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-work-eligibility", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx) && ctx.Err() == nil
}

func (s *Foundation) readWorkEligibility(ctx context.Context, p Principal, cursor triageCursor) (workEligibilityPage, error) {
	return s.readWorkEligibilityChecked(ctx, p, cursor, nil)
}

func (s *Foundation) eligibilityOperator(ctx context.Context, p Principal) error {
	if p.membershipIncarnation == "" || !s.workEligibilityReady(ctx) || !time.Now().Before(p.expires) || ctx.Err() != nil {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

// The page has a bounded original-submission order. Each row retains its own
// request/evidence locks through the final private-response checkpoint; rows are
// observations at distinct instants, rather than one atomic queue snapshot.
func (s *Foundation) readWorkEligibilityChecked(ctx context.Context, p Principal, cursor triageCursor, check workResponseCheck) (workEligibilityPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.eligibilityOperator(ctx, p); err != nil {
		return workEligibilityPage{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT r.id,r.submitted_at FROM portal_requests r
 WHERE EXISTS(SELECT 1 FROM portal_memberships m WHERE m.issuer=$1 AND m.subject=$2 AND m.role='operator' AND NOT m.revoked AND m.incarnation::text=$5)
 AND ($3='' OR (r.submitted_at,r.id COLLATE "C") > ($4::timestamptz,$3 COLLATE "C"))
 ORDER BY r.submitted_at,r.id COLLATE "C" LIMIT 21`, p.issuer, p.subject, cursor.ID, cursor.Submitted, p.membershipIncarnation)
	if err != nil {
		return workEligibilityPage{}, errTriageStorage
	}
	items, err := scanEligibilityCandidates(rows)
	if err != nil {
		return workEligibilityPage{}, err
	}
	page := workEligibilityPage{Items: items}
	if len(items) > triagePageSize {
		last := items[triagePageSize-1]
		page.Next = triageCursor{ID: last.Request, Submitted: last.Submitted}
		page.Items = items[:triagePageSize]
	}
	for i := range page.Items {
		if err := s.observeWorkEligibility(ctx, p, &page.Items[i], check); err != nil {
			return workEligibilityPage{}, err
		}
	}
	// An empty page also needs a fresh operator check after all SDK waits.
	if err := s.eligibilityOperator(ctx, p); err != nil {
		return workEligibilityPage{}, err
	}
	return page, nil
}

func scanEligibilityCandidates(rows pgx.Rows) ([]workEligibilityItem, error) {
	defer rows.Close()
	items := make([]workEligibilityItem, 0, triagePageSize+1)
	for rows.Next() {
		var item workEligibilityItem
		if err := rows.Scan(&item.Request, &item.Submitted); err != nil {
			return nil, errTriageStorage
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errTriageStorage
	}
	return items, nil
}

func (s *Foundation) observeWorkEligibility(ctx context.Context, p Principal, item *workEligibilityItem, check workResponseCheck) error {
	tx, client, viewer, err := s.beginWorkRequest(ctx, p, item.Request, s.workEligibilityReady)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	observation, deadline, err := inspectWorkEligibility(ctx, tx, item.Request, client, viewer)
	if err != nil {
		return err
	}
	observation.Submitted = item.Submitted
	*item = observation
	return s.finishWorkResponse(ctx, p, tx, s.workEligibilityReady, deadline, viewer.Client, check)
}

// inspectWorkEligibility is the shared in-transaction start-policy reader. Its
// caller owns the request lock, release checks and the returned exclusive
// deadline. No adapter may turn this observation into an execution lease.
func inspectWorkEligibility(ctx context.Context, tx pgx.Tx, id, client string, viewer workMember) (workEligibilityItem, time.Time, error) {
	item := workEligibilityItem{Request: id}
	pointer, err := readWorkPointer(ctx, tx, id, client)
	if err != nil {
		return workEligibilityItem{}, time.Time{}, err
	}
	current, valid, err := inspectWorkCurrent(ctx, tx, id, client, pointer, viewer)
	if err != nil {
		return workEligibilityItem{}, time.Time{}, err
	}
	item.Approval, item.Generation, item.State = current.ID, pointer.Generation, eligibilityApprovalRequired
	deadline := time.Time{}
	if current.ID != "" {
		item.State = eligibilityApprovalPaused
	}
	if valid {
		item.State, deadline = eligibilityConfirmationPending, current.Expires
		confirmation, err := readOriginalConfirmation(ctx, tx, id, client, current.Acceptance, current.Order)
		if err != nil {
			return workEligibilityItem{}, time.Time{}, err
		}
		if confirmation.State == confirmationSenderAccepted {
			item.State = eligibilityEligible
		}
	}
	var closed bool
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp(),
 EXISTS(SELECT 1 FROM portal_request_closures WHERE request_id=$1 AND client_id=$2)
 OR EXISTS(SELECT 1 FROM portal_order_completions WHERE order_id=$3)`, id, client, current.Order).Scan(&item.ObservedAt, &closed); err != nil {
		return workEligibilityItem{}, time.Time{}, errTriageStorage
	}
	if closed {
		item.State = eligibilityClosed
	}
	return item, deadline, nil
}
