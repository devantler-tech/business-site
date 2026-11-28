package identity

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestCustomerCanChooseBetweenWorkspaceMemberships(t *testing.T) {
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("multi-workspace-customer", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "first-workspace", "client"))
	if err := b.svc.Invite(t.Context(), Invitation{Issuer: b.f.issuer, Subject: subject, ClientID: "second-workspace", Role: "client", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("a current member could not receive a second invitation: %v", err)
	}
	const choices = "/portal/workspaces?lang=en"
	status, page, _ := b.request("GET", choices, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "first-workspace") || !strings.Contains(page, "second-workspace") {
		t.Fatalf("workspace choice omitted a membership or invitation: %d", status)
	}
	open := func(workspace string) int {
		form := url.Values{"workspace": {workspace}, "csrf_token": {b.csrf(choices)}}
		status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", form, b.site.URL)
		return status
	}
	if status := open("unrelated-workspace"); status != http.StatusForbidden {
		t.Fatalf("opened a workspace without a membership or invitation: %d", status)
	}
	oldToken := b.csrf(choices)
	if status := open("second-workspace"); status != http.StatusSeeOther {
		t.Fatalf("could not accept the second invitation and open its workspace: %d", status)
	}
	staleForm := url.Values{"workspace": {"first-workspace"}, "csrf_token": {oldToken}}
	if status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", staleForm, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("a form rendered before switching retained write authority: %d", status)
	}
	if status, page, _ := b.request("GET", "/portal/?lang=en", nil, ""); status != http.StatusOK || !strings.Contains(page, "second-workspace") {
		t.Fatalf("the session did not enter the selected workspace: %d", status)
	}
	if got := b.memberships(subject, "client"); got != 2 {
		t.Fatalf("the second invitation replaced the original membership: %d", got)
	}
	if status := open("first-workspace"); status != http.StatusSeeOther {
		t.Fatalf("could not switch back to the first workspace: %d", status)
	}
	customerJar := b.client.Jar
	b.client.Jar, _ = cookiejar.New(nil)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	remove := operatorMembershipForm(b, subject, "first-workspace")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", remove, b.site.URL); status != http.StatusOK {
		t.Fatalf("operator could not end one workspace membership: %d", status)
	}
	var deadline bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_customer_account_closures)`).Scan(&deadline); err != nil || deadline {
		t.Fatalf("removing one workspace scheduled an account that still has a membership: %v", err)
	}
	b.client.Jar = customerJar
	if status := open("first-workspace"); status != http.StatusForbidden {
		t.Fatalf("removed membership remained selectable: %d", status)
	}
	if status := open("second-workspace"); status != http.StatusSeeOther {
		t.Fatalf("membership removal blocked access to the remaining workspace: %d", status)
	}
}
