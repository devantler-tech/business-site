package identity

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestContactConfirmationRetainsRealExpiry retains both original expiry and
// delayed-expiry controls at the uncommitted SQL and final SDK checkpoints.
// Only the wall-clock wait is shared: each write has its own broker, client row,
// actual provider/traced pool, live-before-expiry barrier and unchanged-row check.
func TestContactConfirmationRetainsRealExpiry(t *testing.T) {
	pool := fixtureDB(t)
	var probes []expiryProbe
	var fixtures []*browserFixture
	var clients []string
	for _, checkpoint := range []string{"sql", "sdk"} {
		for _, delayed := range []bool{false, true} {
			client := fmt.Sprintf("expiry-confirmation-%s-%t", checkpoint, delayed)
			b := newBrowserFixtureOnPool(t, pool)
			b.client.Timeout = 10 * time.Second
			b.finish(b.start(b.invite("client-a", client, "client")))
			contactVerificationFlags(t, b, true)
			status, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "0", "Fictional Client", "a@example.invalid", ""), b.site.URL)
			if status != http.StatusSeeOther {
				t.Fatal("expiry contact preparation failed")
			}
			p := Principal{b.f.issuer, "client-a", "portal-fixture", time.Now().Add(time.Hour)}
			d, raw, nonce := prepareContactConfirmation(t, b, p, delayed)
			var blocked <-chan struct{}
			var release func()
			if checkpoint == "sql" {
				tracer := installCompletedRead(t, b, "UPDATE portal_contacts SET email_verified=true")
				blocked, release = tracer.blocked, onceRelease(tracer.release)
			} else {
				provider := installReadProvider(t, b, "portal-contact-verification", 2)
				blocked, release = provider.blocked, onceRelease(provider.release)
			}
			if err := b.svc.pool.Ping(t.Context()); err != nil {
				t.Fatal("expiry confirmation pool warmup failed", err)
			}
			probes = append(probes, expiryProbe{
				name:    fmt.Sprintf("%s/delayed-%t", checkpoint, delayed),
				arm:     func(expires time.Time) { p.expires = expires },
				blocked: blocked,
				release: release,
				read: func(ctx context.Context) error {
					_, err := b.svc.confirmContact(ctx, p, d, raw, nonce)
					return err
				},
			})
			fixtures, clients = append(fixtures, b), append(clients, client)
		}
	}
	if len(probes) != 4 {
		t.Fatal("contact expiry cohort lost an original selector")
	}
	// Retain the original real two-second expiry and three-second write bound.
	// All preparation (including both 750ms delays) precedes this clock.
	runExpiryCohort(t, probes, time.Now().Add(2*time.Second), 3*time.Second)
	for index, b := range fixtures {
		var unchanged bool
		if err := b.pool.QueryRow(t.Context(), `SELECT revision=1 AND NOT email_verified AND email_verified_at IS NULL FROM portal_contacts WHERE client_id=$1`, clients[index]).Scan(&unchanged); err != nil || !unchanged {
			t.Errorf("%s: refused proof changed the saved contact: %v", probes[index].name, err)
		}
	}
}
