package identity

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientHomePutsAvailableTasksBeforeAccountActions(t *testing.T) {
	b := newBrowserFixture(t)
	b.atRoot()
	b.as(customerSubject)
	b.invite(customerSubject, "spruce-design", "client")
	callback := b.startAt("/sign-in?lang=en")
	if status, _, _ := b.request(http.MethodGet, callback, nil, ""); status != http.StatusSeeOther {
		t.Fatal("the fictional customer could not sign in")
	}
	flags := []string{"portal-identity", "portal-requests", "portal-appointment-history"}
	setFixturePortalFlags(t, b, flags, []any{true, false, false})
	status, page, _ := b.request(http.MethodGet, "/?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, `href="/contact?lang=en"`) ||
		strings.Contains(page, `href="/requests?lang=en"`) || strings.Contains(page, `href="/appointments?lang=en"`) {
		t.Fatal("the home page did not keep contact available by itself")
	}

	setFixturePortalFlags(t, b, flags, []any{true, true, true})
	for _, lang := range []string{"en", "da"} {
		t.Run(lang, func(t *testing.T) {
			status, page, _ := b.request(http.MethodGet, "/?lang="+lang, nil, "")
			if status != http.StatusOK || !strings.Contains(page, "spruce-design") || !csrfInput.MatchString(page) {
				t.Fatal("workspace or protected sign-out is missing")
			}
			last := -1
			for _, marker := range []string{
				`href="/requests?lang=` + lang + `"`, `href="/appointments?lang=` + lang + `"`,
				`href="/contact?lang=` + lang + `"`, `action="/logout?lang=` + lang + `"`,
			} {
				next := strings.Index(page, marker)
				if next <= last {
					t.Fatalf("task or account action is missing or out of order: %s", marker)
				}
				last = next
			}
		})
	}
}
