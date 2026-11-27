package identity

import (
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestContactSuggestsOnlyTheCurrentVerifiedAccountEmail(t *testing.T) {
	b := newBrowserFixture(t)
	var lookups atomic.Int32
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{profiles: func(ids []string) map[string]customeridentity.Profile {
		lookups.Add(1)
		if len(ids) != 1 {
			t.Error("contact requested multiple account profiles")
			return nil
		}
		switch ids[0] {
		case "1010":
			return map[string]customeridentity.Profile{"1010": {Email: "first@example.test", EmailVerified: true}}
		case "2020":
			return map[string]customeridentity.Profile{"2020": {Email: "second@example.test"}}
		default:
			t.Error("contact did not request the account bound to the session")
			return nil
		}
	}}); err != nil {
		t.Fatal(err)
	}
	first := customeridentity.DexSubject("1010", customeridentity.ZitadelConnectorID)
	second := customeridentity.DexSubject("2020", customeridentity.ZitadelConnectorID)
	b.as(first)
	b.finish(b.startInvited(first, "email-first", "client"))
	status, body, headers := b.request("GET", "/portal/contact?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(body, `name="email" type="email" value="first@example.test"`) ||
		!strings.Contains(headers.Get("Cache-Control"), "no-store") || strings.Contains(body, "second@example.test") {
		t.Fatalf("the first workspace got no private suggestion for its own verified email: %d", status)
	}
	var stored bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_contacts WHERE client_id='email-first')`).Scan(&stored); err != nil || stored {
		t.Fatalf("viewing the suggestion wrote a contact: %t %v", stored, err)
	}
	form := url.Values{"name": {"First Test"}, "email": {"edited@example.test"}, "company": {""},
		"revision": {"0"}, "csrf_token": {b.csrf("/portal/contact")}}
	if status, _, _ := b.request("POST", "/portal/contact", form, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("saving a deliberate contact edit returned %d", status)
	}
	before := lookups.Load()
	status, body, _ = b.request("GET", "/portal/contact", nil, "")
	if status != http.StatusOK || !strings.Contains(body, `value="edited@example.test"`) ||
		strings.Contains(body, "first@example.test") || lookups.Load() != before {
		t.Fatalf("contact looked up or overwrote the saved address: %d", status)
	}

	b.signOut()
	b.as(second)
	b.finish(b.startInvited(second, "email-second", "client"))
	status, body, _ = b.request("GET", "/portal/contact?lang=da", nil, "")
	if status != http.StatusOK || !strings.Contains(body, `name="email" type="email" value=""`) ||
		strings.Contains(body, "first@example.test") || strings.Contains(body, "second@example.test") {
		t.Fatalf("the second workspace saw an unverified or another workspace's address: %d", status)
	}
}
