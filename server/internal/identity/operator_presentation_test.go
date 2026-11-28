package identity

import (
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestOperatorWorkspaceNavigation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("pending-customer", "client-a", "client")
	for _, page := range []struct {
		path, active, current, heading, detail string
	}{
		{"/", "/operator", "page", "Operator workspace", "Workspaces"},
		{"/operator/customers", "/operator/customers", "page", "Arbejdsområder", "Aktive arbejdsområder"},
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

func TestOperatorCustomersShowPeopleAndCurrentOperators(t *testing.T) {
	b := newBrowserFixture(t)
	shared := customeridentity.DexSubject("operator-shared", customeridentity.ZitadelConnectorID)
	if err := b.svc.LinkOperators([]string{shared}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES
($1,$2,$3,'operator'),($1,$4,'client-a','client')`, b.f.issuer, operatorSubject, operatorWorkspace, customerSubject); err != nil {
		t.Fatal(err)
	}
	pending := customeridentity.DexSubject("pending-account", customeridentity.ZitadelConnectorID)
	b.invite(pending, "client-b", "client")
	var calls atomic.Int32
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{profiles: func(ids []string) map[string]customeridentity.Profile {
		calls.Add(1)
		if !slices.Equal(ids, []string{customeridentity.DexUser(customerSubject, customeridentity.ZitadelConnectorID), "pending-account"}) &&
			!slices.Equal(ids, []string{customeridentity.DexUser(customerSubject, customeridentity.ZitadelConnectorID)}) &&
			!slices.Equal(ids, []string{"pending-account"}) {
			t.Error("profile lookup did not use only the current customer and invite")
		}
		return map[string]customeridentity.Profile{
			customeridentity.DexUser(customerSubject, customeridentity.ZitadelConnectorID): {Name: "Ada <Example>", Email: "ada@example.test"},
			"pending-account": {Email: "pending@example.test"},
		}
	}}); err != nil {
		t.Fatal(err)
	}
	b.as(shared)
	b.finish(b.start())
	for _, lang := range []string{"en", "da"} {
		status, index, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&q=ada%40example.test", nil, "")
		if status != http.StatusOK || !strings.Contains(index, "client-a") || strings.Contains(index, "client-b") {
			t.Fatalf("search did not select the exact matching workspace: status=%d", status)
		}
		status, body, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace=client-a", nil, "")
		label := "You"
		if lang == "da" {
			label = "Dig"
		}
		if status != http.StatusOK || strings.Contains(body, operatorSubject) || strings.Contains(body, shared) ||
			strings.Contains(body, "<code>") || strings.Contains(body, "Ada <Example>") {
			t.Fatalf("directory showed an opaque account or unescaped data: status=%d", status)
		}
		for _, fragment := range []string{"<strong>Ada &lt;Example&gt;</strong>", "<span>ada@example.test</span>", "view=remove&amp;member=" + customerSubject} {
			if !strings.Contains(body, fragment) {
				t.Errorf("%s directory missing %q", lang, fragment)
			}
		}
		_, removalPage, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace=client-a&view=remove&member="+customerSubject, nil, "")
		if !strings.Contains(removalPage, `name="subject" value="`+customerSubject+`"`) {
			t.Errorf("%s confirmation did not bind the selected customer", lang)
		}
		_, pendingPage, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace=client-b", nil, "")
		_, operatorPage, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace="+operatorWorkspace, nil, "")
		if !strings.Contains(pendingPage, "<strong>pending@example.test</strong>") || !strings.Contains(operatorPage, "<strong>"+label+"</strong>") || strings.Contains(operatorPage, operatorSubject) {
			t.Errorf("%s missing an invitation or current operator label", lang)
		}
	}
	b.signOut()
	b.as(operatorSubject)
	b.refused(b.start())
	b.as(customerSubject)
	b.finish(b.start())
	callsBeforeDenied := calls.Load()
	status, body, _ := b.request("GET", "/portal/operator/customers", nil, "")
	if status != http.StatusForbidden || calls.Load() != callsBeforeDenied || strings.Contains(body, "pending@example.test") {
		t.Fatalf("a customer could read profiles from the operator directory: status=%d calls=%d", status, calls.Load())
	}
}
