//go:build contactintegration

package identity

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// contactAuthorityProvider waits inside the real SDK, not the HTTP handler,
// maintained session store or database. It can close one decision during a wait.
type contactAuthorityProvider struct {
	finalRequestProvider
	deniedKey atomic.Pointer[string]
}

func (p *contactAuthorityProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, values openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	p.wait(ctx, key)
	if denied := p.deniedKey.Load(); denied != nil && *denied == key {
		return openfeature.BoolResolutionDetail{Value: false}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, values)
}

// TestGoContactAuthority keeps both post-read SDK checkpoints on the ordinary
// Go composition. Losing the final durable recheck must expose a stale response
// to these controls, including when the optional confirmation gate is closed.
func TestGoContactAuthority(t *testing.T) {
	for _, checkpoint := range []struct {
		name string
		key  string
		at   int32
	}{
		{"post-frame identity", "portal-identity", 7},
		{"post-frame verification", "portal-contact-verification", 2},
		{"initial verification", "portal-contact-verification", 1},
	} {
		changes := []string{"unchanged", "durable logout", "deleted session", "expired session", "revoked membership", "operator role", "client rebind", "identity closed", "verification closed", "cancelled"}
		if checkpoint.at == 1 {
			changes = []string{"unchanged", "deleted session"}
		}
		for _, change := range changes {
			t.Run(fmt.Sprintf("%s/%s", checkpoint.name, change), func(t *testing.T) {
				b := savedVerificationContact(t)
				logoutCSRF := b.csrf("/portal/contact")
				p := &contactAuthorityProvider{finalRequestProvider: finalRequestProvider{
					FeatureProvider: memprovider.NewInMemoryProvider(map[string]memprovider.InMemoryFlag{
						"portal-identity":             {Key: "portal-identity", State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}},
						"portal-contact-verification": {Key: "portal-contact-verification", State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}},
					}),
					key: checkpoint.key, blockAt: checkpoint.at, blocked: make(chan struct{}), release: make(chan struct{}),
				}}
				if err := b.flags.SetProviderAndWait(t.Context(), p); err != nil {
					t.Fatal(err)
				}
				release := onceRelease(p.release)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if change == "cancelled" {
					b.mu.Lock()
					h := b.handler
					b.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r.WithContext(ctx)) })
					b.mu.Unlock()
				}
				result := startContactRead(t, b, release)
				select {
				case <-p.blocked:
				case <-time.After(5 * time.Second):
					t.Fatal("ordinary Go contact missed its real post-read SDK checkpoint")
				}
				query := ""
				switch change {
				case "durable logout":
					if status, _, _ := b.request("POST", "/portal/logout", url.Values{"csrf_token": {logoutCSRF}}, b.site.URL); status != http.StatusSeeOther {
						t.Fatal("actual durable logout failed", status)
					}
				case "deleted session":
					query = `DELETE FROM portal_sessions`
				case "expired session":
					query = `UPDATE portal_sessions SET expiry=now()-interval '1 minute'`
				case "revoked membership":
					query = `UPDATE portal_memberships SET revoked=true WHERE subject='client-a'`
				case "operator role":
					query = `UPDATE portal_memberships SET role='operator' WHERE subject='client-a'`
				case "client rebind":
					query = `UPDATE portal_memberships SET client_id='client-b' WHERE subject='client-a'`
				case "identity closed":
					key := "portal-identity"
					p.deniedKey.Store(&key)
				case "verification closed":
					key := "portal-contact-verification"
					p.deniedKey.Store(&key)
				case "cancelled":
					cancel()
				}
				if query != "" {
					if _, err := b.pool.Exec(t.Context(), query); err != nil {
						t.Fatal("fixture change failed", err)
					}
				}
				release()
				select {
				case got := <-result:
					if change == "unchanged" {
						if got.status != http.StatusOK || !strings.Contains(got.body, "Fictional Client") || !strings.Contains(got.body, `class="contact-form"`) {
							t.Fatal("unchanged ordinary Go response failed")
						}
					} else if got.status != http.StatusServiceUnavailable || strings.Contains(got.body, "Fictional Client") || strings.Contains(got.body, "a@example.invalid") || strings.Contains(got.body, `class="contact-form"`) {
						t.Fatalf("%s released private output or wrong failure: %d", change, got.status)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("ordinary Go contact response missing")
				}
				if change == "durable logout" || change == "deleted session" {
					var count int
					if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_sessions`).Scan(&count); err != nil || count != 0 {
						t.Fatal("contact read resurrected a deleted session", err)
					}
				}
			})
		}
	}
}
