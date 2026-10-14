package identity

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

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
				provider := &finalRequestProvider{FeatureProvider: memprovider.NewInMemoryProvider(acceptanceMemoryFlags()), key: "portal-proposal-acceptance", blockAt: 1, blocked: make(chan struct{}), release: make(chan struct{})}
				provider.denied.Store(true)
				if outcome == "error" {
					provider.resolutionError = openfeature.NewGeneralResolutionError("synthetic unavailable evaluation")
				}
				release := installAcceptanceWait(t, b, provider, provider.release)
				ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
				defer cancel()
				type response struct {
					status int
					body   string
				}
				result := make(chan response, 1)
				go func() {
					status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
					result <- response{status, body}
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
