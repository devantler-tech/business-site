package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// These controls change only the clock result. The order, consent and funding
// are still read under the real restricted PostgreSQL transaction.
type fundingClockFixture struct {
	pgx.Tx
	now *time.Time
}

func (tx fundingClockFixture) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == "SELECT clock_timestamp()" {
		return tx.Tx.QueryRow(ctx, `SELECT $1::timestamptz`, tx.now)
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestWorkFundingUsesDatabaseTimeForWithdrawalDeadline(t *testing.T) {
	b, _ := approvalFixture(t)
	ctx := t.Context()
	owner := migrationDB(t)
	const client = "private-client-key"
	var acceptance string
	if err := b.pool.QueryRow(ctx, `SELECT id FROM portal_proposal_acceptances WHERE request_id=$1 AND client_id=$2`,
		proposalRequestID, client).Scan(&acceptance); err != nil {
		t.Fatal(err)
	}

	previous := time.Date(2020, time.January, 2, 11, 0, 0, 0, time.UTC)
	beforeDeadline := withdrawalDeadline(previous).Add(-time.Microsecond)
	current := time.Now().UTC()
	atDeadline := withdrawalDeadline(current)
	cases := []struct {
		name   string
		placed time.Time
		early  *time.Time
		now    *time.Time
		want   error
	}{
		{"application ahead of database", previous, nil, &beforeDeadline, errProposalConflict},
		{"application behind database", current, nil, &atDeadline, nil},
		{"clock unavailable", previous, nil, nil, errTriageStorage},
		{"recorded early request", current, &current, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := owner.Exec(t.Context(), `UPDATE portal_orders SET placed_at=$1,early_start_at=$2 WHERE request_id=$3`,
				tc.placed, tc.early, proposalRequestID); err != nil {
				t.Fatal(err)
			}
			err := pgx.BeginFunc(t.Context(), b.pool, func(tx pgx.Tx) error {
				if err := lockProposalRequest(t.Context(), tx, proposalRequestID); err != nil {
					return err
				}
				_, _, err := readWorkFunding(t.Context(), fundingClockFixture{Tx: tx, now: tc.now}, proposalRequestID, client, acceptance)
				return err
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("work readiness with database time: got %v, want %v", err, tc.want)
			}
		})
	}
}
