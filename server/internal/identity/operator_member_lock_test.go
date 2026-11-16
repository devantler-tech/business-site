package identity

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func oneConnectionMemberBrowser(t *testing.T) *browserFixture {
	t.Helper()
	base := fixtureDB(t)
	config := base.Config()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return newBrowserFixtureOnPool(t, pool)
}

// TestOperatorMemberTransitionUsesOneConnection catches authority callbacks
// trying to acquire a second connection while their identity lock retains one.
func TestOperatorMemberTransitionUsesOneConnection(t *testing.T) {
	b := configureOperatorBrowser(t, oneConnectionMemberBrowser(t))
	linkManagedMember(t, b, func(context.Context, string, bool) error { return nil })
	b.client.Timeout = 2 * time.Second
	form := operatorMembershipForm(b, customerSubject, "client-a")
	for _, action := range []string{"deactivate", "reactivate"} {
		if status, _, _ := b.request("POST", operatorAPIRoot+"/members/"+action, form, b.site.URL); status != http.StatusOK {
			t.Fatal("member action could not finish with one available connection", action, status)
		}
	}
}

// TestMemberIdentityLockCleanup catches a cancelled request returning a locked
// connection, and an unconfirmed unlock returning a reusable connection.
func TestMemberIdentityLockCleanup(t *testing.T) {
	for _, scenario := range []string{"cancelled", "unconfirmed-unlock"} {
		t.Run(scenario, func(t *testing.T) {
			b := oneConnectionMemberBrowser(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			connection, release, err := (&browser{foundation: b.svc}).customerIdentityConnection(ctx, customerSubject)
			if err != nil {
				t.Fatal(err)
			}
			originalPID := connection.Conn().PgConn().PID()
			if scenario == "cancelled" {
				cancel()
			} else {
				var unlocked bool
				if err := connection.QueryRow(t.Context(), `SELECT pg_advisory_unlock($1::int,hashtext($2 || ' ' || $3))`, identityLock, b.f.issuer, customerSubject).Scan(&unlocked); err != nil || !unlocked {
					t.Fatal("could not exercise an unconfirmed cleanup result", err)
				}
			}
			release()
			fresh, stop := context.WithTimeout(t.Context(), 2*time.Second)
			defer stop()
			next, err := b.pool.Acquire(fresh)
			if err != nil {
				t.Fatal("lock cleanup did not release its pool slot", err)
			}
			defer next.Release()
			var locked bool
			if err := next.QueryRow(fresh, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid())`).Scan(&locked); err != nil || locked {
				t.Fatal("request cancellation returned a locked connection", locked, err)
			}
			if scenario == "unconfirmed-unlock" && next.Conn().PgConn().PID() == originalPID {
				t.Fatal("an unconfirmed unlock returned its connection to the pool")
			}
		})
	}
}
