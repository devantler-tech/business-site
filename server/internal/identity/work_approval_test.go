package identity

import (
	"errors"
	"testing"
	"time"
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

// approvalFixture enables only fictional operator authority, not a browser or
// worker route. The invoice, acceptance and credit use their real service paths.
func approvalFixture(t *testing.T) (*browserFixture, Principal) {
	t.Helper()
	b := creditFixture(t)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-invoices", "portal-credit-reconciliation", "portal-work-approvals"}, []any{true, true, true, true, true, true, true, true, true})
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
	b.reconnect(t.Context())
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
