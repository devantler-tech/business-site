package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixture transactions retain failing cleanup diagnostics, including after an
// assertion aborts. They never add a test lifecycle method to production code.
func beginFixtureTransaction(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("fixture transaction cleanup failed")
		}
	})
	return tx
}

func lockFixtureRead(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string) (pgx.Tx, uint32) {
	t.Helper()
	var query string
	switch table {
	case "portal_requests":
		query = `LOCK TABLE portal_requests IN ACCESS EXCLUSIVE MODE`
	case "portal_proposals":
		query = `LOCK TABLE portal_proposals IN ACCESS EXCLUSIVE MODE`
	default:
		t.Fatal("unsupported fixture read lock")
	}
	var relation uint32
	if err := pool.QueryRow(ctx, `SELECT $1::regclass::oid`, table).Scan(&relation); err != nil {
		t.Fatal(err)
	}
	tx := beginFixtureTransaction(ctx, t, pool)
	if _, err := tx.Exec(ctx, query); err != nil {
		t.Fatal(err)
	}
	return tx, relation
}

func waitForFixtureExpiry(ctx context.Context, t *testing.T, expires time.Time) {
	t.Helper()
	timer := time.NewTimer(time.Until(expires) + time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("fixture expiry exceeded its bound")
	}
}

func revokeFixtureAccess(ctx context.Context, t *testing.T, b *browserFixture, subject string) {
	t.Helper()
	if err := b.svc.Revoke(ctx, b.f.issuer, subject); err != nil {
		t.Fatal(err)
	}
}
