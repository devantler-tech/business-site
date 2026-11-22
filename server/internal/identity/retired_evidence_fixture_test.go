package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// These are the retired invoice-record (#117) and bank-credit (#120) writers,
// kept only so work-approval tests can seed the historical evidence that work
// approval still reads until #268. Production code can no longer write it.

var errInvoiceComponents = errors.New("invoice components are required")

// retiredEvidenceOperator replaces the retired portal-invoices and
// portal-credit-reconciliation gates with the acceptance gate they required.
func (s *Foundation) retiredEvidenceOperator(ctx context.Context, p Principal) error {
	if !s.acceptanceReady(ctx) {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

// beginRetiredEvidenceWrite opens the bounded, request-locked operator write
// both retired writers used. done rolls back (if uncommitted) and cancels.
func (s *Foundation) beginRetiredEvidenceWrite(ctx context.Context, p Principal, id string) (context.Context, pgx.Tx, string, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := s.retiredEvidenceOperator(ctx, p); err != nil {
		cancel()
		return ctx, nil, "", nil, err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		cancel()
		return ctx, nil, "", nil, err
	}
	return ctx, tx, client, func() { rollbackProposalWrite(ctx, tx); cancel() }, nil
}

func (s *Foundation) persistInvoice(ctx context.Context, p Principal, id string, d projectInvoice) error {
	ctx, tx, client, done, err := s.beginRetiredEvidenceWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer done()
	var issuer, subject string
	err = tx.QueryRow(ctx, `SELECT operator_issuer,operator_subject FROM portal_invoices WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key).Scan(&issuer, &subject)
	if err == nil {
		saved, readErr := scanInvoice(tx.QueryRow(ctx, `SELECT `+invoiceColumns+` FROM portal_invoices i WHERE i.request_id=$1 AND i.client_id=$2 AND i.submission_key=$3`, id, client, d.Key))
		if readErr != nil {
			return errTriageStorage
		}
		if issuer != p.issuer || subject != p.subject || saved.Expected != d.Expected || saved.Acceptance != d.Acceptance || saved.Reference != d.Reference || saved.Purpose != d.Purpose || saved.AmountMinor != d.AmountMinor || saved.NetMinor != d.NetMinor || saved.VATMinor != d.VATMinor {
			return errProposalConflict
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return errTriageStorage
		}
		if !d.NetMinor.Valid || !d.VATMinor.Valid {
			return errInvoiceComponents
		}
		var sequence int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0) FROM portal_invoices WHERE request_id=$1 AND client_id=$2`, id, client).Scan(&sequence); err != nil {
			return errTriageStorage
		}
		if sequence != d.Expected || sequence >= 50 {
			return errProposalConflict
		}
		review, acceptance, err := currentInvoiceAcceptance(ctx, tx, id, client)
		if err != nil {
			return err
		}
		if acceptance != d.Acceptance {
			return errProposalConflict
		}
		_, err = tx.Exec(ctx, `INSERT INTO portal_invoices(request_id,client_id,sequence,expected_sequence,submission_key,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,reference,purpose,currency,amount_minor,net_minor,vat_minor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'DKK',$14,$15,$16)`, id, client, sequence+1, d.Expected, d.Key, acceptance, review.Proposal.Version, review.Event, review.Hash, p.issuer, p.subject, d.Reference, d.Purpose, d.AmountMinor, d.NetMinor, d.VATMinor)
		if err != nil {
			var conflict *pgconn.PgError
			if errors.As(err, &conflict) && conflict.Code == "23505" {
				return errProposalConflict
			}
			return errTriageStorage
		}
	}
	return commitProposalWrite(ctx, p, tx, s.acceptanceReady)
}

// persistCredit appends one content-bound receipt under the request lock. The
// global transfer PK arbitrates competing invoices even on different requests.
func (s *Foundation) persistCredit(ctx context.Context, p Principal, id string, invoice int, d creditRevision) error {
	ctx, tx, client, done, err := s.beginRetiredEvidenceWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer done()
	if _, err = boundCreditInvoice(ctx, tx, id, client, invoice); err != nil {
		return err
	}
	var issuer, subject string
	var savedInvoice int
	err = tx.QueryRow(ctx, `SELECT operator_issuer,operator_subject,invoice_sequence FROM portal_credit_revisions WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key).Scan(&issuer, &subject, &savedInvoice)
	if err == nil {
		saved, readErr := scanCredit(tx.QueryRow(ctx, `SELECT `+creditColumns+` FROM portal_credit_revisions WHERE request_id=$1 AND client_id=$2 AND submission_key=$3`, id, client, d.Key))
		if readErr != nil {
			return errTriageStorage
		}
		if issuer != p.issuer || subject != p.subject || savedInvoice != invoice || saved.Source != d.Source || saved.Reference != d.Reference || saved.State != d.State || saved.Note != d.Note || saved.AmountMinor != d.AmountMinor || saved.Expected != d.Expected {
			return errProposalConflict
		}
		return commitProposalWrite(ctx, p, tx, s.acceptanceReady)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	if err = reserveCreditTransfer(ctx, tx, id, client, invoice, d); err != nil {
		return err
	}
	var revision int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM portal_credit_revisions WHERE source=$1 AND reference=$2`, d.Source, d.Reference).Scan(&revision); err != nil {
		return errTriageStorage
	}
	if revision != d.Expected || revision >= 16 || (revision == 15 && d.State != "invalidated") {
		return errProposalConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO portal_credit_revisions(source,reference,request_id,client_id,invoice_sequence,revision,expected_revision,submission_key,state,amount_minor,note,operator_issuer,operator_subject) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, d.Source, d.Reference, id, client, invoice, revision+1, d.Expected, d.Key, d.State, d.AmountMinor, d.Note, p.issuer, p.subject)
	if err != nil {
		return creditWriteError(err)
	}
	return commitProposalWrite(ctx, p, tx, s.acceptanceReady)
}

// reserveCreditTransfer never reallocates a previously reserved identity.
func reserveCreditTransfer(ctx context.Context, tx pgx.Tx, id, client string, invoice int, d creditRevision) error {
	var savedID, savedClient string
	var savedInvoice int
	err := tx.QueryRow(ctx, `SELECT request_id,client_id,invoice_sequence FROM portal_credit_transfers WHERE source=$1 AND reference=$2`, d.Source, d.Reference).Scan(&savedID, &savedClient, &savedInvoice)
	if err == nil {
		if savedID != id || savedClient != client || savedInvoice != invoice {
			return errProposalConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return errTriageStorage
	}
	if d.Expected != 0 {
		return errProposalConflict
	}
	var ordinal int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(max(ordinal),0) FROM portal_credit_transfers WHERE request_id=$1 AND client_id=$2 AND invoice_sequence=$3`, id, client, invoice).Scan(&ordinal); err != nil {
		return errTriageStorage
	}
	if ordinal >= 32 {
		return errProposalConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO portal_credit_transfers(source,reference,request_id,client_id,invoice_sequence,ordinal) VALUES($1,$2,$3,$4,$5,$6)`, d.Source, d.Reference, id, client, invoice, ordinal+1)
	return creditWriteError(err)
}

// recordFixtureInvoice writes one historical invoice record (sequence 1) for
// the request's current acceptance: 1,000.00 net + 234.56 VAT = 1,234.56 DKK.
func recordFixtureInvoice(t *testing.T, b *browserFixture) {
	t.Helper()
	var acceptance string
	if err := b.pool.QueryRow(t.Context(), `SELECT id FROM portal_proposal_acceptances WHERE request_id=$1 ORDER BY recorded_at DESC LIMIT 1`, proposalRequestID).Scan(&acceptance); err != nil {
		t.Fatal("fixture invoice has no acceptance", err)
	}
	d := projectInvoice{Key: secret(), Acceptance: acceptance, Reference: "TEST-2026-01", Purpose: "Fictional agreed iteration installment", AmountMinor: 123456, NetMinor: pgtype.Int8{Int64: 100000, Valid: true}, VATMinor: pgtype.Int8{Int64: 23456, Valid: true}}
	if err := b.svc.persistInvoice(t.Context(), evidenceFixtureOperator(t, b), proposalRequestID, d); err != nil {
		t.Fatal("fixture invoice unavailable", err)
	}
}

// recordFixtureCredit appends one revision of bank-credit evidence to invoice 1.
func recordFixtureCredit(t *testing.T, b *browserFixture, expected int, amountMinor int64, note string) {
	t.Helper()
	d := creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: "confirmed", Expected: expected, AmountMinor: amountMinor, Note: note}
	if err := b.svc.persistCredit(t.Context(), evidenceFixtureOperator(t, b), proposalRequestID, 1, d); err != nil {
		t.Fatal("fixture credit unavailable", err)
	}
}

// evidenceFixtureOperator enrolls a fresh current operator for one fixture write.
func evidenceFixtureOperator(t *testing.T, b *browserFixture) Principal {
	t.Helper()
	return enroll(t, b.svc, b.f, "evidence-fixture-"+secret()[:8], "operator-workspace", "operator")
}
