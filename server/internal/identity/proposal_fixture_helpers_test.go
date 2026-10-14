package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// setFixturePortalFlags preserves missing flags and the native SDK's typed values.
func setFixturePortalFlags(t *testing.T, b *browserFixture, names []string, values []any) {
	t.Helper()
	if len(names) != len(values) {
		t.Fatal("fixture flag names and values differ")
	}
	flags := map[string]memprovider.InMemoryFlag{}
	for i, name := range names {
		if values[i] != nil {
			flags[name] = memprovider.InMemoryFlag{Key: name, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": values[i]}}
		}
	}
	if err := b.flags.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

// blockFixtureProposalWrite uses the real request lock shared by both writers.
func blockFixtureProposalWrite(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx := beginFixtureTransaction(ctx, t, pool)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, proposalRequestID); err != nil {
		t.Fatal(err)
	}
	return tx
}

// waitForFixtureProposalWrite observes the actual blocked database operation.
func waitForFixtureProposalWrite(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`)
}

// waitForFixtureDatabaseLock bounds observation of a real lock with the caller's deadline.
func waitForFixtureDatabaseLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, query, args...).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("fixture did not reach blocked database operation")
		}
	}
}

// releaseFixtureAndRequireDenial observes the real operation after releasing its lock.
func releaseFixtureAndRequireDenial(ctx context.Context, t *testing.T, tx pgx.Tx, result <-chan error, change string) {
	t.Helper()
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("in-flight %s admitted the operation: %v", change, err)
		}
	case <-ctx.Done():
		t.Fatal("fixture operation exceeded its bound")
	}
}

// closeFixtureSharing applies the same in-flight SDK or authority closure to reads and writes.
func closeFixtureSharing(ctx context.Context, t *testing.T, b *browserFixture, p Principal, change string) {
	t.Helper()
	if change == "flag" {
		sharingFlags(t, b, true, true, true, true, false)
	} else {
		alterFixturePrincipal(ctx, t, b, p, change)
	}
}

// alterFixturePrincipal changes real authority while an authorized read or write is paused.
func alterFixturePrincipal(ctx context.Context, t *testing.T, b *browserFixture, p Principal, change string) {
	t.Helper()
	switch change {
	case "revocation":
		revokeFixtureAccess(ctx, t, b, p.subject)
	case "downgrade":
		if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE subject=$1`, p.subject); err != nil {
			t.Fatal(err)
		}
	case "rebind":
		if _, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET client_id='different-client' WHERE subject=$1`, p.subject); err != nil {
			t.Fatal(err)
		}
	case "expiry":
		waitForFixtureExpiry(ctx, t, p.expires)
	default:
		t.Fatal("unsupported fixture authority change")
	}
}

// assertFixtureVisibilityCount checks the persisted ledger, not a mocked writer result.
func assertFixtureVisibilityCount(ctx context.Context, t *testing.T, b *browserFixture, want int) {
	t.Helper()
	var count int
	if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM portal_proposal_visibility`).Scan(&count); err != nil || count != want {
		t.Fatalf("visibility ledger has %d events, want %d: %v", count, want, err)
	}
}
