package identity

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
)

// finalRequestProvider holds the SDK checkpoint after private rows are read.
// Only the external flag evaluation is controlled; storage and authority stay real.
type finalRequestProvider struct {
	openfeature.FeatureProvider
	key              string
	blockAt          int32
	reads            atomic.Int32
	denied           atomic.Bool
	blocked, release chan struct{}
}

// BooleanEvaluation keeps the ordinary provider behavior around one bounded pause.
func (p *finalRequestProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, evaluation openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if key == p.key && p.reads.Add(1) == p.blockAt {
		close(p.blocked)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	if key == p.key && p.denied.Load() {
		return openfeature.BoolResolutionDetail{Value: false}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, evaluation)
}

// TestRequestReadRechecksAuthorityAfterFinalEvaluation catches stale private reads,
// while retaining an unchanged-authority positive control for both selectors.
func TestRequestReadRechecksAuthorityAfterFinalEvaluation(t *testing.T) {
	for _, selector := range []string{"list", "receipt"} {
		for _, change := range []string{"unchanged", "revocation", "role", "rebind", "expiry", "flag", "cancel", "storage"} {
			t.Run(selector+"/"+change, func(t *testing.T) {
				b := newBrowserFixture(t)
				requestFlag(t, b, true)
				p := enroll(t, b.svc, b.f, "checkpoint-client", "checkpoint-owner", "client")
				request, err := b.svc.persistRequest(b.f.ctx, p, projectRequest{Key: secret(), Product: "website", Title: "Fictional private request", Brief: "Synthetic checkpoint control"})
				if err != nil {
					t.Fatal("fixture request setup failed", err)
				}
				provider := installReadProvider(t, b, "portal-requests", 2)
				id := ""
				if selector == "receipt" {
					id = request.ID
				}
				read := func(ctx context.Context, principal Principal) (bool, error) {
					rows, readErr := b.svc.readRequests(ctx, principal, id)
					if readErr != nil {
						return rows != nil, readErr
					}
					return len(rows) == 1 && rows[0].ID == request.ID && rows[0].Title == "Fictional private request", nil
				}
				checkChangedRead(t, b, p, provider, provider.blocked, provider.release, change, false, true, read)
			})
		}
	}
}

// closeRequestReadAuthority changes real membership or one final read prerequisite.
func closeRequestReadAuthority(ctx context.Context, t *testing.T, b *browserFixture, p Principal, provider *finalRequestProvider, change string, cancel context.CancelFunc) {
	t.Helper()
	switch change {
	case "revocation":
		if err := b.svc.Revoke(ctx, p.issuer, p.subject); err != nil {
			t.Fatal("fixture revocation failed", err)
		}
	case "role", "rebind":
		query := `UPDATE portal_memberships SET role='operator' WHERE issuer=$1 AND subject=$2`
		if change == "rebind" {
			query = `UPDATE portal_memberships SET client_id='checkpoint-other' WHERE issuer=$1 AND subject=$2`
		}
		tag, err := b.pool.Exec(ctx, query, p.issuer, p.subject)
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatal("fixture membership change failed", err)
		}
	case "expiry":
		waitForFixtureExpiry(ctx, t, p.expires)
	case "flag":
		provider.denied.Store(true)
	case "cancel":
		cancel()
	case "storage":
		migrator := migrationDB(t)
		if _, err := migrator.Exec(ctx, `REVOKE SELECT ON portal_memberships FROM portal_runtime`); err != nil {
			t.Fatal("fixture storage refusal setup failed", err)
		}
		t.Cleanup(func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, err := migrator.Exec(cleanup, `GRANT SELECT ON portal_memberships TO portal_runtime`); err != nil {
				t.Error("fixture storage privilege cleanup failed", err)
			}
		})
	}
}
