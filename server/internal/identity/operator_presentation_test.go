package identity

import (
	"net/http"
	"strings"
	"testing"
)

func TestOperatorWorkspaceNavigation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("pending-customer", "client-a", "client")
	for _, page := range []struct {
		path, active, current, heading, detail string
	}{
		{"/", "/operator", "page", "Operator workspace", "Invite a customer"},
		{"/operator/customers", "/operator/customers", "page", "Kunder og arbejdsområder", "pending-customer"},
		{"/operator/invitations/new", "/operator/customers", "location", "Invite a customer", `aria-describedby="invite-workspace-help"`},
		{"/operator/audit", "/operator/audit", "page", "Revisionslog", "customers.view"},
	} {
		lang := "en"
		if page.path == "/operator/customers" || page.path == "/operator/audit" {
			lang = "da"
		}
		status, body, _ := b.request("GET", "/portal"+page.path+"?lang="+lang, nil, "")
		if status != http.StatusOK || !strings.Contains(body, "<h1>"+page.heading+"</h1>") {
			t.Fatalf("%s did not show its own heading: %d", page.path, status)
		}
		for _, fragment := range []string{
			`class="operator-navigation"`,
			`href="/portal` + page.active + "?lang=" + lang + `" aria-current="` + page.current + `"`,
			`action="/portal/logout?lang=` + lang + `"`,
			page.detail,
		} {
			if !strings.Contains(body, fragment) {
				t.Errorf("%s missing %q", page.path, fragment)
			}
		}
		if strings.Contains(body, `href="/portal/operator/requests?lang=`) {
			t.Errorf("%s showed a closed inbox", page.path)
		}
	}
	triageFlags(t, b, true, true, true)
	status, body, _ := b.request("GET", "/portal/operator/requests?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(body, `class="operator-navigation"`) ||
		!strings.Contains(body, `href="/portal/operator/requests?lang=en" aria-current="location"`) {
		t.Fatal("the active request inbox has no operator navigation")
	}
	b.signOut()
	b.as("private-client")
	b.finish(b.startInvited("private-client", "client-c", "client"))
	if status, body, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK || !strings.Contains(body, "client-c") {
		t.Fatal("client fixture did not sign in to its own workspace")
	}
	for _, path := range []string{"/", "/operator/customers", "/operator/invitations/new", "/operator/audit", "/operator/requests"} {
		_, body, _ := b.request("GET", "/portal"+path, nil, "")
		if strings.Contains(body, `class="operator-navigation"`) {
			t.Errorf("client could see operator navigation on %s", path)
		}
	}
}
