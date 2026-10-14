package identity

import (
	"context"
	"testing"
	"time"
)

// TestProposalWriteObservationIgnoresUnrelatedLocks keeps authority-race tests
// from proceeding before their own request write reaches the database lock.
func TestProposalWriteObservationIgnoresUnrelatedLocks(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"other namespace", `SELECT pg_advisory_xact_lock(4711)`},
		{"other request", `SELECT pg_advisory_xact_lock(1869820518,hashtext('unrelated-request'))`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			t.Cleanup(cancel)
			pool := migrationDB(t)
			blocker := beginFixtureTransaction(ctx, t, pool)
			if _, err := blocker.Exec(ctx, tc.query); err != nil {
				t.Fatal(err)
			}
			unrelated := beginFixtureTransaction(ctx, t, pool)
			var pid int
			if err := unrelated.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			unrelatedDone := make(chan error, 1)
			go func() { _, err := unrelated.Exec(ctx, tc.query); unrelatedDone <- err }()
			t.Cleanup(func() {
				if err := blocker.Commit(context.Background()); err != nil {
					t.Error(err)
				}
				if err := <-unrelatedDone; err != nil {
					t.Error(err)
				}
			})
			waitForFixtureDatabaseLock(ctx, t, pool, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, pid)

			observed := make(chan struct{})
			go func() { defer close(observed); waitForFixtureProposalWrite(ctx, t, pool, proposalRequestID) }()
			select {
			case <-observed:
				t.Fatal("unrelated advisory lock was accepted as the request write")
			case <-time.After(100 * time.Millisecond):
			}

			requestBlocker := blockFixtureProposalWrite(ctx, t, pool)
			request := beginFixtureTransaction(ctx, t, pool)
			requestDone := make(chan error, 1)
			go func() {
				_, err := request.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, proposalRequestID)
				requestDone <- err
			}()
			t.Cleanup(func() {
				if err := requestBlocker.Commit(context.Background()); err != nil {
					t.Error(err)
				}
				if err := <-requestDone; err != nil {
					t.Error(err)
				}
			})
			select {
			case <-observed:
			case <-ctx.Done():
				t.Fatal("the matching blocked request write was not observed")
			}
		})
	}
}
