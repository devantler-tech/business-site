package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

type workApprovalInput struct {
	Key, Acceptance, EvidenceHash string
	Invoice                       int
	Expected                      int64
}

type workRevocationInput struct {
	Key, Approval string
	Expected      int64
}

type workApprovalReceipt struct {
	ID, Request, Client, Key, Acceptance, Hash                           string
	Expected                                                             int64
	Invoice, Version, Event                                              int
	Net, VAT, Amount                                                     int64
	ClientIssuer, ClientSubject, ClientIncarnation                       string
	OperatorIssuer, OperatorSubject, OperatorClient, OperatorIncarnation string
	Evidence                                                             []byte
	EvidenceHash                                                         string
	Decided, Expires                                                     time.Time
}

type workPointer struct {
	ID         string
	Generation int64
}

// workApprovalGate permits revocation even when old funding or sharing is no
// longer valid. Every operation still needs identity, triage and this decision.
func (s *Foundation) workApprovalGate(ctx context.Context) bool {
	if !s.triageReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-work-approvals", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx) && ctx.Err() == nil
}

func (s *Foundation) approvalsReady(ctx context.Context) bool {
	return s.creditsReady(ctx) && s.workApprovalGate(ctx)
}

// beginWorkRequest derives both workspaces before deterministic membership
// locking. The actual private query independently requires current operator access.
func (s *Foundation) beginWorkRequest(ctx context.Context, p Principal, id string, ready func(context.Context) bool) (pgx.Tx, string, workMember, error) {
	if !ready(ctx) || s.triageOperator(ctx, p) != nil {
		return nil, "", workMember{}, ErrDenied
	}
	if !opaqueRequestID.MatchString(id) {
		return nil, "", workMember{}, pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", workMember{}, errTriageStorage
	}
	handedOff := false
	defer func() {
		if !handedOff {
			rollbackProposalWrite(ctx, tx)
		}
	}()
	if err := lockProposalRequest(ctx, tx, id); err != nil {
		return nil, "", workMember{}, err
	}
	viewer := workMember{Issuer: p.issuer, Subject: p.subject, Role: "operator"}
	var client string
	err = tx.QueryRow(ctx, `SELECT r.client_id,m.client_id FROM portal_requests r JOIN portal_memberships m ON m.issuer=$2 AND m.subject=$3 AND m.role='operator' AND NOT m.revoked WHERE r.id=$1`, id, p.issuer, p.subject).Scan(&client, &viewer.Client)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", workMember{}, ErrDenied
		}
		return nil, "", workMember{}, errTriageStorage
	}
	handedOff = true
	return tx, client, viewer, nil
}

func readWorkPointer(ctx context.Context, tx pgx.Tx, id, client string) (workPointer, error) {
	var pointer workPointer
	err := tx.QueryRow(ctx, `SELECT COALESCE(approval_id,''),generation FROM portal_work_current WHERE request_id=$1 AND client_id=$2`, id, client).Scan(&pointer.ID, &pointer.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return pointer, nil
	}
	if err != nil {
		return workPointer{}, errTriageStorage
	}
	return pointer, nil
}

// workReview owns the common bounded read transaction and its cleanup order.
// Authorization and every final release checkpoint remain operation-specific.
type workReview struct {
	ctx    context.Context
	cancel context.CancelFunc
	tx     pgx.Tx
	client string
	viewer workMember
}

func (s *Foundation) beginWorkReview(ctx context.Context, p Principal, id string) (*workReview, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	tx, client, viewer, err := s.beginWorkRequest(ctx, p, id, s.approvalsReady)
	if err != nil {
		cancel()
		return nil, err
	}
	return &workReview{ctx: ctx, cancel: cancel, tx: tx, client: client, viewer: viewer}, nil
}

func (r *workReview) close() {
	defer r.cancel()
	rollbackProposalWrite(r.ctx, r.tx)
}

// prepareWorkApproval supplies a content-bound comparison, not approval or a
// mutable review context. It does not write a session, receipt or current pointer.
func (s *Foundation) prepareWorkApproval(ctx context.Context, p Principal, id string, invoice int) (workApprovalInput, error) {
	r, err := s.beginWorkReview(ctx, p, id)
	if err != nil {
		return workApprovalInput{}, err
	}
	defer r.close()
	evidence, _, hash, err := collectWorkEvidence(r.ctx, r.tx, id, r.client, invoice, r.viewer, r.viewer)
	if err != nil {
		return workApprovalInput{}, err
	}
	pointer, err := readWorkPointer(r.ctx, r.tx, id, r.client)
	if err != nil {
		return workApprovalInput{}, err
	}
	if err := s.commitWork(r.ctx, p, r.tx, s.approvalsReady, time.Time{}); err != nil {
		return workApprovalInput{}, err
	}
	return workApprovalInput{Key: secret(), Acceptance: evidence.Acceptance, Invoice: invoice, EvidenceHash: hash, Expected: pointer.Generation}, nil
}

const workApprovalColumns = `id,request_id,client_id,submission_key,expected_generation,acceptance_id,invoice_sequence,proposal_version,visibility_event,content_hash,net_minor,vat_minor,amount_minor,client_issuer,client_subject,client_incarnation::text,operator_issuer,operator_subject,operator_client,operator_incarnation::text,evidence,evidence_hash,decided_at,expires_at`

func scanWorkApproval(row pgx.Row) (workApprovalReceipt, error) {
	var d workApprovalReceipt
	err := row.Scan(&d.ID, &d.Request, &d.Client, &d.Key, &d.Expected, &d.Acceptance, &d.Invoice, &d.Version, &d.Event, &d.Hash, &d.Net, &d.VAT, &d.Amount, &d.ClientIssuer, &d.ClientSubject, &d.ClientIncarnation, &d.OperatorIssuer, &d.OperatorSubject, &d.OperatorClient, &d.OperatorIncarnation, &d.Evidence, &d.EvidenceHash, &d.Decided, &d.Expires)
	return d, err
}

func requireWorkViewer(ctx context.Context, tx pgx.Tx, viewer workMember) error {
	_, matches, err := lockWorkMembers(ctx, tx, []workMember{viewer})
	if err != nil {
		return err
	}
	if !matches[0] {
		return ErrDenied
	}
	return nil
}

// persistWorkApproval atomically appends one decision, permanently supersedes its
// predecessor and advances the compare token. Retries never move that pointer.
func (s *Foundation) persistWorkApproval(ctx context.Context, p Principal, id string, input workApprovalInput) (workApprovalReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !opaqueRequestID.MatchString(input.Key) || !opaqueRequestID.MatchString(input.Acceptance) || input.Invoice < 1 || input.Invoice > 50 || input.Expected < 0 || input.Expected == 1<<63-1 {
		return workApprovalReceipt{}, errProposalConflict
	}
	tx, client, viewer, err := s.beginWorkRequest(ctx, p, id, s.approvalsReady)
	if err != nil {
		return workApprovalReceipt{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	saved, err := scanWorkApproval(tx.QueryRow(ctx, `SELECT `+workApprovalColumns+` FROM portal_work_approvals WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, input.Key))
	if err == nil {
		if err := requireWorkViewer(ctx, tx, viewer); err != nil {
			return workApprovalReceipt{}, err
		}
		if saved.OperatorIssuer != p.issuer || saved.OperatorSubject != p.subject || saved.Expected != input.Expected || saved.Invoice != input.Invoice || saved.Acceptance != input.Acceptance || saved.EvidenceHash != input.EvidenceHash {
			return workApprovalReceipt{}, errProposalConflict
		}
		return saved, s.commitWork(ctx, p, tx, s.approvalsReady, time.Time{})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return workApprovalReceipt{}, errTriageStorage
	}
	evidence, data, hash, err := collectWorkEvidence(ctx, tx, id, client, input.Invoice, viewer, viewer)
	if err != nil {
		return workApprovalReceipt{}, err
	}
	pointer, err := readWorkPointer(ctx, tx, id, client)
	if err != nil {
		return workApprovalReceipt{}, err
	}
	if evidence.Acceptance != input.Acceptance || hash != input.EvidenceHash || pointer.Generation != input.Expected {
		return workApprovalReceipt{}, errProposalConflict
	}
	if !s.approvalsReady(ctx) || !time.Now().Before(p.expires) || ctx.Err() != nil {
		return workApprovalReceipt{}, ErrDenied
	}
	d := evidence.Invoice
	row := tx.QueryRow(ctx, `WITH decision AS (SELECT clock_timestamp() AS at)
 INSERT INTO portal_work_approvals(id,request_id,client_id,submission_key,expected_generation,acceptance_id,invoice_sequence,proposal_version,visibility_event,content_hash,net_minor,vat_minor,amount_minor,client_issuer,client_subject,client_incarnation,operator_issuer,operator_subject,operator_client,operator_incarnation,evidence,evidence_hash,decided_at,expires_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,at,at+interval '168 hours' FROM decision RETURNING `+workApprovalColumns,
		secret(), id, client, input.Key, input.Expected, evidence.Acceptance, input.Invoice, d.Version, d.Event, d.Hash, d.NetMinor.Int64, d.VATMinor.Int64, d.AmountMinor, evidence.Acceptor.Issuer, evidence.Acceptor.Subject, evidence.Acceptor.Incarnation, p.issuer, p.subject, evidence.Actor.Client, evidence.Actor.Incarnation, data, hash)
	saved, err = scanWorkApproval(row)
	if err != nil {
		return workApprovalReceipt{}, creditWriteError(err)
	}
	if pointer.ID != "" {
		if err := appendWorkRevocation(ctx, tx, id, client, p, workRevocationInput{Key: secret(), Approval: pointer.ID, Expected: pointer.Generation}, "superseded"); err != nil {
			return workApprovalReceipt{}, err
		}
	}
	if err := moveWorkPointer(ctx, tx, id, client, saved.ID, pointer.Generation); err != nil {
		return workApprovalReceipt{}, err
	}
	return saved, s.commitWork(ctx, p, tx, s.approvalsReady, saved.Expires)
}

func moveWorkPointer(ctx context.Context, tx pgx.Tx, id, client, approval string, expected int64) error {
	tag, err := tx.Exec(ctx, `INSERT INTO portal_work_current(request_id,client_id,approval_id,generation) VALUES($1,$2,NULLIF($3,''),$4+1)
 ON CONFLICT(request_id) DO UPDATE SET approval_id=EXCLUDED.approval_id,generation=EXCLUDED.generation
 WHERE portal_work_current.client_id=EXCLUDED.client_id AND portal_work_current.generation=$4`, id, client, approval, expected)
	if err != nil {
		return creditWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errProposalConflict
	}
	return nil
}

func appendWorkRevocation(ctx context.Context, tx pgx.Tx, id, client string, p Principal, input workRevocationInput, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO portal_work_revocations(approval_id,request_id,client_id,submission_key,expected_generation,reason,operator_issuer,operator_subject) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, input.Approval, id, client, input.Key, input.Expected, reason, p.issuer, p.subject)
	return creditWriteError(err)
}

// revokeWorkApproval needs current operator access, never valid old funding or
// the former acceptor's membership. It appends history and clears no newer pointer.
func (s *Foundation) revokeWorkApproval(ctx context.Context, p Principal, id string, input workRevocationInput) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !opaqueRequestID.MatchString(input.Key) || !opaqueRequestID.MatchString(input.Approval) || input.Expected < 1 || input.Expected == 1<<63-1 {
		return errProposalConflict
	}
	tx, client, viewer, err := s.beginWorkRequest(ctx, p, id, s.workApprovalGate)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if err := requireWorkViewer(ctx, tx, viewer); err != nil {
		return err
	}
	var approval, issuer, subject, reason string
	var expected int64
	err = tx.QueryRow(ctx, `SELECT approval_id,operator_issuer,operator_subject,expected_generation,reason FROM portal_work_revocations WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, input.Key).Scan(&approval, &issuer, &subject, &expected, &reason)
	if err == nil {
		if approval != input.Approval || issuer != p.issuer || subject != p.subject || expected != input.Expected || reason != "revoked" {
			return errProposalConflict
		}
		return s.commitWork(ctx, p, tx, s.workApprovalGate, time.Time{})
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	pointer, err := readWorkPointer(ctx, tx, id, client)
	if err != nil {
		return err
	}
	if pointer.ID != input.Approval || pointer.Generation != input.Expected {
		return errProposalConflict
	}
	if err := appendWorkRevocation(ctx, tx, id, client, p, input, "revoked"); err != nil {
		return err
	}
	if err := moveWorkPointer(ctx, tx, id, client, "", pointer.Generation); err != nil {
		return err
	}
	return s.commitWork(ctx, p, tx, s.workApprovalGate, time.Time{})
}

// currentWorkApproval never falls back to an older receipt. A historical retry
// cannot revive a revoked, expired or superseded current selection.
func (s *Foundation) currentWorkApproval(ctx context.Context, p Principal, id string) (workApprovalReceipt, bool, error) {
	r, err := s.beginWorkReview(ctx, p, id)
	if err != nil {
		return workApprovalReceipt{}, false, err
	}
	defer r.close()
	ctx, tx := r.ctx, r.tx
	pointer, err := readWorkPointer(ctx, tx, id, r.client)
	if err != nil {
		return workApprovalReceipt{}, false, err
	}
	if pointer.ID == "" {
		if err := requireWorkViewer(ctx, tx, r.viewer); err != nil {
			return workApprovalReceipt{}, false, err
		}
		return workApprovalReceipt{}, false, s.commitWork(ctx, p, tx, s.approvalsReady, time.Time{})
	}
	d, err := scanWorkApproval(tx.QueryRow(ctx, `SELECT `+workApprovalColumns+` FROM portal_work_approvals WHERE id=$1 AND request_id=$2 AND client_id=$3`, pointer.ID, id, r.client))
	if err != nil {
		return workApprovalReceipt{}, false, errTriageStorage
	}
	actor := workMember{Issuer: d.OperatorIssuer, Subject: d.OperatorSubject, Client: d.OperatorClient, Role: "operator", Incarnation: d.OperatorIncarnation}
	_, _, hash, evidenceErr := collectWorkEvidence(ctx, tx, id, r.client, d.Invoice, actor, r.viewer)
	if evidenceErr != nil && !errors.Is(evidenceErr, errProposalConflict) {
		return workApprovalReceipt{}, false, evidenceErr
	}
	// A scope/funding conflict can precede collectWorkEvidence's membership step.
	if err := requireWorkViewer(ctx, tx, r.viewer); err != nil {
		return workApprovalReceipt{}, false, err
	}
	var terminal bool
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_work_revocations WHERE approval_id=$1),clock_timestamp()`, d.ID).Scan(&terminal, &at); err != nil {
		return workApprovalReceipt{}, false, errTriageStorage
	}
	valid := evidenceErr == nil && hash == d.EvidenceHash && !terminal && workDeadlineValid(at, d.Expires)
	deadline := time.Time{}
	if valid {
		deadline = d.Expires
	}
	if err := s.commitWork(ctx, p, tx, s.approvalsReady, deadline); err != nil {
		return workApprovalReceipt{}, false, err
	}
	return d, valid, nil
}

// workDeadlineValid compares the database's observed instant, never the host's
// clock or a local calendar date. Equality is already expired.
func workDeadlineValid(at, expires time.Time) bool {
	return at.Before(expires)
}

// commitWork performs SDK waits before the final database deadline check and
// principal-expiry checkpoint. The held membership locks survive through commit.
func (s *Foundation) commitWork(ctx context.Context, p Principal, tx pgx.Tx, ready func(context.Context) bool, deadline time.Time) error {
	if !ready(ctx) || !time.Now().Before(p.expires) || ctx.Err() != nil {
		return ErrDenied
	}
	if !deadline.IsZero() {
		var at time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
			return errTriageStorage
		}
		if !workDeadlineValid(at, deadline) {
			return ErrDenied
		}
	}
	if !time.Now().Before(p.expires) || ctx.Err() != nil {
		return ErrDenied
	}
	if err := tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}
