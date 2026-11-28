package identity

import (
	"net/http"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestCustomerAccountCanSignInWithoutWorkspace(t *testing.T) {
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("customer-without-workspace", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.start())
	status, page, _ := b.request("GET", "/portal/?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "No workspaces yet") || !strings.Contains(page, "Sign out") {
		t.Fatalf("customer without membership could not use their account: %d", status)
	}
	for _, path := range []string{"/portal/contact", "/portal/operator", customerAccountsPath} {
		if status, _, _ := b.request("GET", path, nil, ""); status < 400 {
			t.Fatalf("an account without membership opened %s: %d", path, status)
		}
	}
	b.signOut()
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusSeeOther {
		t.Fatalf("customer without membership could not sign out: %d", status)
	}
}
