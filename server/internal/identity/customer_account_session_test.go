package identity

import (
	"net/http"
	"strings"
	"testing"
	"time"

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
	if err := b.svc.Invite(t.Context(), Invitation{Issuer: b.f.issuer, Subject: subject, ClientID: "new-workspace", Role: "client", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/contact", nil, ""); status != http.StatusOK {
		t.Fatalf("a signed-in customer could not accept an invitation received later: %d", status)
	}
	b.signOut()
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusSeeOther {
		t.Fatalf("customer without membership could not sign out: %d", status)
	}
}
