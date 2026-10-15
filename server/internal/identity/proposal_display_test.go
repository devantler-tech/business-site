package identity

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

type displayResponse struct {
	status int
	body   string
}

// closedDisplayCheckpoint delays only the real SDK's optional presentation check.
func closedDisplayCheckpoint(t *testing.T, b *browserFixture, key, outcome string) (*finalRequestProvider, func()) {
	t.Helper()
	provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(acceptanceMemoryFlags()), key: key, blockAt: 1, blocked: make(chan struct{}), release: make(chan struct{})}
	provider.denied.Store(true)
	if outcome == "error" {
		provider.resolutionError = openfeature.NewGeneralResolutionError("synthetic unavailable evaluation")
	}
	return provider, installAcceptanceWait(t, b, provider, provider.release)
}

// A closed optional display gate must not bypass current request authority.
// The real TLS/session/SQL handler waits inside the SDK, not a mocked read.
func TestConsentReceiptRechecksAuthorityAfterClosedDisplayEvaluation(t *testing.T) {
	for _, outcome := range []string{"false", "error"} {
		for _, change := range []string{"unchanged", "revocation", "rebind", "role", "expiry"} {
			t.Run(outcome+"/"+change, func(t *testing.T) {
				b := acceptanceFixture(t)
				var title, brief string
				if err := b.pool.QueryRow(t.Context(), `SELECT title,brief FROM portal_requests WHERE id=$1`, proposalRequestID).Scan(&title, &brief); err != nil {
					t.Fatal("fictional request unavailable")
				}
				expires := time.Now().Add(3 * time.Second).Truncate(time.Second)
				if change == "expiry" {
					b.configure(map[string]any{"exp": expires.Unix()}, false)
				}
				triageSignIn(t, b, "display-client", "private-client-key", "client")
				provider, release := closedDisplayCheckpoint(t, b, "portal-proposal-acceptance", outcome)
				ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
				defer cancel()
				result := make(chan displayResponse, 1)
				go func() {
					status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
					result <- displayResponse{status, body}
				}()
				select {
				case <-provider.blocked:
				case <-ctx.Done():
					t.Fatal("optional display checkpoint not reached")
				}
				principal := Principal{issuer: b.f.issuer, subject: "display-client", audience: b.f.broker.clientID, expires: expires}
				closeRequestReadAuthority(ctx, t, b, principal, provider, change, cancel)
				release()
				select {
				case got := <-result:
					visible := strings.Contains(got.body, title) || strings.Contains(got.body, brief) || strings.Contains(got.body, "A fictional first iteration")
					if (change == "unchanged" && (got.status != 200 || !visible)) || (change != "unchanged" && (got.status == 200 || visible)) {
						t.Fatal("closed display gate bypassed current private-read authority")
					}
				case <-ctx.Done():
					t.Fatal("receipt did not finish after SDK release")
				}
			})
		}
	}
}

// Optional operator presentation checks must finish before private data reads,
// even when a disabled or unavailable feature renders no extra controls.
func TestOperatorDetailRechecksAuthorityAfterClosedDisplayEvaluation(t *testing.T) {
	for _, key := range []string{"portal-proposals", "portal-proposal-sharing"} {
		for _, outcome := range []string{"false", "error"} {
			for _, change := range []string{"unchanged", "revocation", "role", "rebind", "expiry"} {
				t.Run(key+"/"+outcome+"/"+change, func(t *testing.T) {
					b := newBrowserFixture(t)
					proposalFlags(t, b, true, true, true, true)
					seedTriageRequest(t, b, proposalRequestID, "Fictional proposal request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
					expires := time.Now().Add(3 * time.Second).Truncate(time.Second)
					if change == "expiry" {
						b.configure(map[string]any{"exp": expires.Unix()}, false)
					}
					triageSignIn(t, b, "proposal-operator", "operator-workspace", "operator")
					form := proposalForm(t, b)
					if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/proposals", form, b.site.URL); status != 303 {
						t.Fatal("fictional proposal setup failed")
					}
					provider, release := closedDisplayCheckpoint(t, b, key, outcome)
					ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
					defer cancel()
					result := make(chan displayResponse, 1)
					go func() {
						path := "/portal/operator/requests/" + proposalRequestID
						if outcome == "error" {
							path += "?lang=da"
						}
						status, body, _ := b.request("GET", path, nil, "")
						result <- displayResponse{status, body}
					}()
					select {
					case <-provider.blocked:
					case <-ctx.Done():
						t.Fatal("operator display checkpoint not reached")
					}
					principal := Principal{issuer: b.f.issuer, subject: "proposal-operator", audience: b.f.broker.clientID, expires: expires}
					if change == "role" {
						tag, err := b.pool.Exec(ctx, `UPDATE portal_memberships SET role='client' WHERE issuer=$1 AND subject=$2`, principal.issuer, principal.subject)
						if err != nil || tag.RowsAffected() != 1 {
							t.Fatal("fictional operator downgrade failed", err)
						}
					} else {
						closeRequestReadAuthority(ctx, t, b, principal, provider, change, cancel)
					}
					release()
					select {
					case got := <-result:
						visible := strings.Contains(got.body, "Fictional proposal request") || strings.Contains(got.body, "hostile-brief") || strings.Contains(got.body, "A fictional first iteration")
						allowed := change == "unchanged" || change == "rebind"
						if (allowed && (got.status != 200 || !visible)) || (!allowed && (got.status == 200 || visible)) {
							t.Fatal("closed operator display check bypassed current private-read authority")
						}
					case <-ctx.Done():
						t.Fatal("operator detail did not finish after SDK release")
					}
				})
			}
		}
	}
}
