package identity

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// The SQL pointer cannot select another client's receipt, independently of the
// service's operator checks. No unscoped latest-row fallback is involved.
func TestWorkApprovalPointerClientConfinement(t *testing.T) {
	b, op := approvalFixture(t)
	d := savedApproval(t, b, op)
	cloneCreditInvoice(t)
	_, err := b.pool.Exec(t.Context(), `INSERT INTO portal_work_current(request_id,client_id,approval_id,generation)
 SELECT id,client_id,$1,1 FROM portal_requests WHERE client_id='other-credit-client'`, d.ID)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23503" {
		t.Fatal("cross-client current pointer was not refused by binding", err)
	}
	if current, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || !valid || current.ID != d.ID {
		t.Fatal("failed foreign selection changed current decision", err)
	}
}

// Every actual evidence read must fail unavailable, not silently lose its
// selected receipt or turn a failed census into a valid empty balance.
func TestWorkApprovalFailedReadsUnavailable(t *testing.T) {
	b, op := approvalFixture(t)
	d := savedApproval(t, b, op)
	migrator := migrationDB(t)
	for _, table := range []string{"portal_work_current", "portal_work_approvals", "portal_work_revocations", "portal_invoices", "portal_credit_transfers", "portal_credit_revisions", "portal_membership_transitions"} {
		if _, err := migrator.Exec(t.Context(), "REVOKE SELECT ON "+table+" FROM portal_runtime"); err != nil {
			t.Fatal(err)
		}
		_, valid, readErr := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID)
		if _, err := migrator.Exec(t.Context(), "GRANT SELECT ON "+table+" TO portal_runtime"); err != nil {
			t.Fatal("fixture read permission restoration failed", err)
		}
		if !errors.Is(readErr, errTriageStorage) || valid {
			t.Fatal("failed "+table+" read was not unavailable", readErr)
		}
	}
	if current, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || !valid || current.ID != d.ID {
		t.Fatal("failed reads altered original evidence", err)
	}
}

// Force a late pointer write failure after appending the candidate and its
// supersession. The caller-owned transaction must roll back both evidence rows.
func TestWorkApprovalLateStorageFailureRollsBack(t *testing.T) {
	b, op := approvalFixture(t)
	first := savedApproval(t, b, op)
	input := preparedApproval(t, b, op)
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `REVOKE UPDATE(approval_id,generation) ON portal_work_current FROM portal_runtime`); err != nil {
		t.Fatal(err)
	}
	_, writeErr := b.svc.persistWorkApproval(t.Context(), op, proposalRequestID, input)
	if _, err := migrator.Exec(t.Context(), `GRANT UPDATE(approval_id,generation) ON portal_work_current TO portal_runtime`); err != nil {
		t.Fatal("fixture pointer permission restoration failed", err)
	}
	if !errors.Is(writeErr, errTriageStorage) {
		t.Fatal("late pointer failure not reported unavailable", writeErr)
	}
	var approvals, revocations int
	if err := b.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM portal_work_approvals),(SELECT count(*) FROM portal_work_revocations)`).Scan(&approvals, &revocations); err != nil || approvals != 1 || revocations != 0 {
		t.Fatal("failed pointer write left partial authority", err)
	}
	if current, valid, err := b.svc.currentWorkApproval(t.Context(), op, proposalRequestID); err != nil || !valid || current.ID != first.ID {
		t.Fatal("rollback lost previous current authority", err)
	}
}

// Revocation uses a different prerequisite set but the same request-first
// transaction discipline; waiting must not preserve a revoked or expired actor.
func TestWorkApprovalRevocationRechecksAfterLock(t *testing.T) {
	for _, change := range []string{"revoke", "rebind", "expiry"} {
		t.Run(change, func(t *testing.T) {
			b, op := approvalFixture(t)
			d := savedApproval(t, b, op)
			ctx, migrator, lock := blockApprovalFixture(t, &op, change == "expiry")
			result := make(chan error, 1)
			go func() {
				result <- b.svc.revokeWorkApproval(ctx, op, proposalRequestID, workRevocationInput{Key: secret(), Approval: d.ID, Expected: 1})
			}()
			waitForFixtureProposalWrite(ctx, t, migrator, proposalRequestID)
			changeApprovalFixture(ctx, t, b, op, change, func() {
				if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET client_id='rebound-operator-space' WHERE issuer=$1 AND subject=$2`, b.f.issuer, op.subject); err != nil {
					t.Fatal(err)
				}
			})
			// A workspace rebind remains an authorized operator but changes the review
			// context. Revocation derives current membership after the wait, so it may
			// safely close authority. Role revocation and expiry must deny.
			if err := lock.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if change == "rebind" {
					if err != nil {
						t.Fatal("current rebound operator could not revoke", err)
					}
				} else if !errors.Is(err, ErrDenied) {
					t.Fatal("stale operator revoked after waiting", err)
				}
			case <-ctx.Done():
				t.Fatal("delayed revocation did not finish")
			}
			var count int
			if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_work_revocations`).Scan(&count); err != nil || (change != "rebind" && count != 0) || (change == "rebind" && count != 1) {
				t.Fatal("delayed revocation changed unexpected history", err)
			}
		})
	}
}
