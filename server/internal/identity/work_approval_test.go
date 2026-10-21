package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// TestWorkApprovalSchema requires all three durable authority relations, not a
// ledger version or an in-memory approval that disappears across reconnects.
func TestWorkApprovalSchema(t *testing.T) {
	pool := fixtureDB(t)
	for _, name := range []string{"portal_work_approvals", "portal_work_revocations", "portal_work_current"} {
		var present bool
		if err := pool.QueryRow(t.Context(), `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&present); err != nil {
			t.Fatal("approval schema read failed", err)
		}
		if !present {
			t.Errorf("durable approval relation %s is absent", name)
		}
	}
}

// An allocated transfer without its first observation is unknown evidence, not
// an absent zero that matching funds from another identity may silently mask.
func TestWorkApprovalRefusesIncompleteTransferHistory(t *testing.T) {
	for _, gap := range []string{"reservation without revision", "missing middle revision"} {
		t.Run(gap, func(t *testing.T) {
			b, op := approvalFixture(t)
			input := preparedApproval(t, b, op)
			if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); err != nil {
				t.Fatal(err)
			}
			if gap == "reservation without revision" {
				tx, client, err := b.svc.beginProposalWrite(t.Context(), op, proposalRequestID)
				if err != nil {
					t.Fatal(err)
				}
				defer rollbackProposalWrite(t.Context(), tx)
				if err := reserveCreditTransfer(t.Context(), tx, proposalRequestID, client, 1, creditRevision{Source: "fixture-bank", Reference: "unobserved-transfer"}); err != nil {
					t.Fatal("owned malformed reservation fixture failed", err)
				}
				if err := tx.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				for i, state := range []string{"disputed", "confirmed"} {
					if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: state, Expected: i + 1, AmountMinor: 123456, Note: "Fictional correction"}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := migrationDB(t).Exec(t.Context(), `DELETE FROM portal_credit_revisions WHERE source='fixture-bank' AND reference='transfer-01' AND revision=2`); err != nil {
					t.Fatal("owned malformed history fixture failed", err)
				}
			}
			if _, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1); !errors.Is(err, errTriageStorage) {
				t.Errorf("incomplete history admitted a fresh approval review: %v", err)
			}
			if _, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); !errors.Is(err, errTriageStorage) || valid {
				t.Errorf("incomplete history was not unavailable: valid=%v err=%v", valid, err)
			}
		})
	}
}

// approvalFixture owns real storage, a TLS broker and independent SDK state.
// Domain controls need no HTTP application or session store; native journeys
// retain their separate browser fixtures and complete actor coverage.
func approvalFixture(t *testing.T) (*browserFixture, Principal) {
	t.Helper()
	b := &browserFixture{t: t, pool: fixtureDB(t), f: fixtureBroker(t), flags: flagAPI(t, true)}
	b.svc = New(b.pool, b.f.broker, b.flags.NewClient())
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(workApprovalFlags())); err != nil {
		t.Fatal(err)
	}
	seedTriageRequest(t, b, proposalRequestID, "Fictional proposal request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	author := enroll(t, b.svc, b.f, "proposal-operator", "operator-workspace", "operator")
	proposal := projectProposal{Key: secret(), Scope: "A fictional first iteration", Deliverables: "A website and accessible contact page", Currency: "DKK", OneTimeMinor: 299500, MonthlyMinor: 9900, OneTimeTerms: "One agreed iteration", MonthlyTerms: "Hosting and routine maintenance"}
	if err := b.svc.persistProposal(t.Context(), author, proposalRequestID, proposal); err != nil {
		t.Fatal("fixture proposal unavailable", err)
	}
	if err := b.svc.persistVisibility(t.Context(), author, proposalRequestID, visibilityDecision{ExpectedProposal: 1, Proposal: 1, Action: "share", Key: secret()}); err != nil {
		t.Fatal("fixture sharing unavailable", err)
	}
	client := enroll(t, b.svc, b.f, "credit-fixture-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), client, proposalRequestID)
	if err != nil {
		t.Fatal("fixture consent review unavailable", err)
	}
	acceptance, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key)
	if err != nil {
		t.Fatal("fixture consent unavailable", err)
	}
	invoice := projectInvoice{Key: secret(), Acceptance: acceptance.ID, Reference: "TEST-2026-01", Purpose: "Fictional agreed iteration installment", AmountMinor: 123456, NetMinor: pgtype.Int8{Int64: 100000, Valid: true}, VATMinor: pgtype.Int8{Int64: 23456, Valid: true}}
	if err := b.svc.persistInvoice(t.Context(), author, proposalRequestID, invoice); err != nil {
		t.Fatal("fixture invoice unavailable", err)
	}
	op := enroll(t, b.svc, b.f, "approval-operator", "operator-workspace", "operator")
	if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: "confirmed", AmountMinor: 123456, Note: "Fictional credited funds"}); err != nil {
		t.Fatal("fixture funding unavailable", err)
	}
	return b, op
}

// TestWorkApprovalExactBindingAndTerminalHistory exercises real persistence,
// historical retry, permanent supersession and revocation without resurrection.
func TestWorkApprovalExactBindingAndTerminalHistory(t *testing.T) {
	b, op := approvalFixture(t)
	input, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1)
	if err != nil {
		t.Fatal("funded approval review unavailable", err)
	}
	a, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if err != nil || a.ID == "" || a.Expires.Sub(a.Decided) != 168*time.Hour {
		t.Fatal("exact funded approval or fixed deadline failed", err)
	}
	current, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || !valid || current.ID != a.ID {
		t.Fatal("stored approval was not current", err)
	}
	pool, err := pgxpool.New(t.Context(), b.pool.Config().ConnString())
	if err != nil {
		t.Fatal("approval reconnect failed", err)
	}
	t.Cleanup(pool.Close)
	b.pool.Close()
	b.pool = pool
	b.svc = New(pool, b.f.broker, b.flags.NewClient())
	retry, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if err != nil || retry.ID != a.ID || !retry.Decided.Equal(a.Decided) || !retry.Expires.Equal(a.Expires) {
		t.Fatal("retry renewed or lost the original approval", err)
	}
	next, err := b.svc.prepareWorkApproval(t.Context(), op, proposalRequestID, 1)
	if err != nil || next.Expected != 1 {
		t.Fatal("next comparison token unavailable", err)
	}
	c, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, next)
	if err != nil || c.ID == a.ID {
		t.Fatal("fresh decision did not supersede the previous approval", err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, workRevocationInput{Key: secret(), Approval: a.ID, Expected: 1}); !errors.Is(err, errProposalConflict) {
		t.Fatal("stale revocation cleared a newer selection", err)
	}
	changed := input
	changed.EvidenceHash = c.EvidenceHash + "0"
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, changed); !errors.Is(err, errProposalConflict) {
		t.Fatal("changed retry replaced original evidence", err)
	}
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input); err != nil {
		t.Fatal("historical retry lost its original receipt", err)
	}
	current, valid, err = b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || !valid || current.ID != c.ID {
		t.Fatal("old retry revived a superseded pointer", err)
	}
	revocation := workRevocationInput{Key: secret(), Approval: c.ID, Expected: 2}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, revocation); err != nil {
		t.Fatal("revocation failed", err)
	}
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, revocation); err != nil {
		t.Fatal("identical revocation retry failed", err)
	}
	if _, err := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, next); err != nil {
		t.Fatal("revoked approval receipt retry failed", err)
	}
	current, valid, err = b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
	if err != nil || valid || current.ID != "" {
		t.Fatal("revoked or superseded approval was resurrected", err)
	}
	var terminals int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_revocations`).Scan(&terminals); err != nil || terminals != 2 {
		t.Fatal("terminal history was lost or duplicated", err)
	}
}
