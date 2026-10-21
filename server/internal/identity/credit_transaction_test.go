package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestCreditTransactionReaderRetainsOwnership exercises real request locking,
// complete latest-revision evidence and the caller's final authorized commit.
func TestCreditTransactionReaderRetainsOwnership(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "empty"
		if populated {
			name = "revisions"
		}
		t.Run(name, func(t *testing.T) {
			b := creditFixture(t)
			op := enroll(t, b.svc, b.f, "transaction-credit-reader", "operator-workspace", "operator")
			if populated {
				for i, state := range []string{"confirmed", "disputed", "confirmed"} {
					d := creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-01", State: state, Expected: i, AmountMinor: 50000, Note: "Fictional correction"}
					if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
						t.Fatal("fixture correction failed", err)
					}
				}
				d := creditRevision{Key: secret(), Source: "fixture-bank", Reference: "transfer-02", State: "unverified", AmountMinor: 50000, Note: "Fictional unverified evidence"}
				if err := b.svc.persistCredit(t.Context(), op, proposalRequestID, 1, d); err != nil {
					t.Fatal("fixture unresolved evidence failed", err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			tx, client, err := b.svc.beginProposalWrite(ctx, op, proposalRequestID)
			if err != nil {
				t.Fatal("request transaction unavailable", err)
			}
			defer rollbackProposalWrite(t.Context(), tx)
			h, err := readCreditHistory(ctx, tx, proposalRequestID, client, 1)
			if err != nil || !h.Invoice.Current || h.Invoice.Sequence != 1 || h.Invoice.Reference != "TEST-2026-01" || h.Invoice.AmountMinor != 123456 {
				t.Fatal("owned transaction did not retain the exact current invoice", err)
			}
			if populated {
				if h.Total != 50000 || !h.Unsettled || len(h.Transfers) != 2 || h.Transfers[0].Latest.Revision != 3 || len(h.Transfers[0].History) != 3 || h.Transfers[0].History[1].State != "disputed" {
					t.Fatal("transaction reader lost latest-revision identity or complete evidence")
				}
			} else if h.Total != 0 || h.Unsettled || len(h.Transfers) != 0 {
				t.Fatal("empty ledger was not distinct from a failed read")
			}
			for _, binding := range []struct {
				client  string
				invoice int
			}{{"different-client", 1}, {client, 2}} {
				if _, err := readCreditHistory(ctx, tx, proposalRequestID, binding.client, binding.invoice); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("foreign or absent invoice returned evidence", err)
				}
			}
			other := beginFixtureTransaction(ctx, t, b.pool)
			var acquired bool
			query := `SELECT pg_try_advisory_xact_lock(1869820518,hashtext($1))`
			if err := other.QueryRow(ctx, query, proposalRequestID).Scan(&acquired); err != nil || acquired {
				t.Fatal("reader released its caller's request lock", err)
			}
			if err := commitProposalWrite(ctx, op, tx, b.svc.creditsReady); err != nil {
				t.Fatal("reader consumed its caller's transaction or final authority check", err)
			}
			if err := other.QueryRow(ctx, query, proposalRequestID).Scan(&acquired); err != nil || !acquired {
				t.Fatal("caller commit did not release the request lock", err)
			}
			if _, err := readCreditHistory(ctx, tx, proposalRequestID, client, 1); !errors.Is(err, errTriageStorage) {
				t.Fatal("closed transaction was presented as absent or empty evidence", err)
			}
		})
	}
}
