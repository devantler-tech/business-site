package identity

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestOperatorManagementContract catches actions missing from the API or its
// client-facing description; callers depend on both the method and action ID.
func TestOperatorManagementContract(t *testing.T) {
	paths := operatorDocumentActions(t)
	mux, err := (&browser{foundation: &Foundation{}}).routes()
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct{ method, path, id string }{
		{"POST", "/invitations/revoke", "invitations.revoke"},
		{"POST", "/workspaces", "workspaces.create"},
		{"POST", "/workspaces/rename", "workspaces.rename"},
		{"POST", "/workspaces/delete", "workspaces.delete"},
		{"GET", "/workspaces/members", "workspaces.members.list"},
		{"POST", "/members/deactivate", "members.deactivate"},
		{"POST", "/members/reactivate", "members.reactivate"},
		{"POST", "/members/remove", "members.remove"},
	} {
		if got := paths["/operator/api"+action.path][strings.ToLower(action.method)].OperationID; got != action.id {
			t.Errorf("%s %s describes action %q, want %q", action.method, action.path, got, action.id)
		}
		_, pattern := mux.Handler(httptest.NewRequest(action.method, operatorAPIRoot+action.path, nil))
		if pattern != action.method+" "+operatorAPIRoot+action.path {
			t.Errorf("%s %s has no API handler", action.method, action.path)
		}
	}
}

type operatorDocumentAction struct {
	OperationID string `json:"operationId"`
}

// operatorDocumentActions shares the parsed API inventory without replacing the
// independent route/action expectations in the contract tests.
func operatorDocumentActions(t *testing.T) map[string]map[string]operatorDocumentAction {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]operatorDocumentAction `json:"paths"`
	}
	if err := json.Unmarshal(operatorOpenAPI, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Paths
}

// TestOperatorRevokesPendingInvitation catches a revocation that merely hides
// the invitation in the UI while the identity can still redeem it.
func TestOperatorRevokesPendingInvitation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("invited-customer", "client-a", "client")
	form := url.Values{"subject": {"invited-customer"}, "csrf_token": {b.csrf("/portal/")}}
	status, _, _ := b.request("POST", operatorAPIRoot+"/invitations/revoke", form, b.site.URL)
	if status != http.StatusOK {
		t.Fatalf("revoke invitation status=%d", status)
	}
	b.signOut()
	b.as("invited-customer")
	b.refused(b.start())
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_memberships WHERE subject='invited-customer'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked invitation created membership", count, err)
	}
}

// TestOperatorCreatesEmptyWorkspace catches workspace creation that only
// succeeds after a customer has already signed in.
func TestOperatorCreatesEmptyWorkspace(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	form := url.Values{"workspace": {"client-new"}, "csrf_token": {b.csrf("/portal/")}}
	status, body, _ := b.request("POST", operatorAPIRoot+"/workspaces", form, b.site.URL)
	if status != http.StatusOK {
		t.Fatalf("create workspace status=%d body=%s", status, body)
	}
	status, body, _ = b.request("GET", operatorAPIRoot+"/workspaces", nil, "")
	var views []workspaceView
	if err := json.Unmarshal([]byte(body), &views); err != nil || status != http.StatusOK {
		t.Fatal("workspace inventory failed", status, err)
	}
	found := false
	for _, view := range views {
		if view.ID == "client-new" {
			found = view.Members != nil && len(view.Members) == 0
		}
	}
	if !found {
		t.Fatal("empty workspace is absent or has null members", body)
	}
}

// TestOperatorManagementRefusesClients catches management actions exposed to a
// current customer, including caller-chosen workspace identifiers.
func TestOperatorManagementRefusesClients(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.startInvited("client-a", "client-a", "client"))
	b.invite("pending-customer", "client-b", "client")
	token := b.csrf("/portal/")
	for _, path := range []string{"/invitations/revoke", "/workspaces", "/workspaces/rename", "/workspaces/delete", "/members/deactivate", "/members/reactivate", "/members/remove"} {
		form := url.Values{"workspace": {"client-b"}, "subject": {"pending-customer"}, "csrf_token": {token}}
		status, body, _ := b.request("POST", operatorAPIRoot+path, form, b.site.URL)
		if status != http.StatusForbidden || strings.Contains(body, "pending-customer") {
			t.Fatalf("client management call %s returned %d", path, status)
		}
	}
	var revoked bool
	if err := b.pool.QueryRow(t.Context(), `SELECT revoked FROM portal_invitations WHERE subject='pending-customer'`).Scan(&revoked); err != nil || revoked {
		t.Fatal("client changed another customer's invitation", err)
	}
}

// TestOperatorManagementRejectsAmbiguousInput catches writes made from
// duplicate, unknown or malformed fields rather than one deliberate choice.
func TestOperatorManagementRejectsAmbiguousInput(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	token := b.csrf("/portal/")
	for name, form := range map[string]url.Values{
		"empty":     {"workspace": {""}},
		"long":      {"workspace": {strings.Repeat("a", 101)}},
		"duplicate": {"workspace": {"client-a", "client-b"}},
		"control":   {"workspace": {"client\nother"}},
		"unknown":   {"workspace": {"client-a"}, "role": {"operator"}},
	} {
		t.Run(name, func(t *testing.T) {
			form.Set("csrf_token", token)
			status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces", form, b.site.URL)
			if status != http.StatusBadRequest {
				t.Fatalf("invalid workspace form status=%d", status)
			}
		})
	}
	status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces", url.Values{"workspace": {"client-a"}}, b.site.URL)
	if status != http.StatusForbidden {
		t.Fatalf("workspace created without CSRF: %d", status)
	}
}

// TestOperatorManagementPageUsesNativeActions catches a phone-facing page that
// omits the management actions or renders a destructive action as a GET link.
func TestOperatorManagementPageUsesNativeActions(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("pending-customer", "client-a", "client")
	for lang, labels := range map[string]struct{ heading, searchLabel string }{
		"en": {"Customers and workspaces", "Find a workspace, customer or invitation"},
		"da": {"Kunder og arbejdsområder", "Find et arbejdsområde, en kunde eller en invitation"},
	} {
		status, body, _ := b.request("GET", "/portal/operator/customers?lang="+lang, nil, "")
		if status != http.StatusOK || !strings.Contains(body, labels.heading) || !strings.Contains(body, "client-a") {
			t.Fatalf("management page unavailable in %s: %d", lang, status)
		}
		if !strings.Contains(body, `role="search" method="get"`) ||
			!strings.Contains(body, `for="directory-query">`+labels.searchLabel) ||
			!strings.Contains(body, `id="directory-query" name="q" type="search"`) {
			t.Fatalf("accessible workspace search is missing in %s", lang)
		}
		_, detail, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace=client-a", nil, "")
		_, closePage, _ := b.request("GET", "/portal/operator/customers?lang="+lang+"&workspace=client-a&view=close", nil, "")
		for action, page := range map[string]string{"/invitations/revoke": detail, "/workspaces": body, "/workspaces/rename": detail, "/workspaces/delete": closePage} {
			if !strings.Contains(page, `method="post" action="`+operatorAPIRoot+action+`?lang=`+lang) || !csrfInput.MatchString(page) {
				t.Fatalf("%s native action missing in %s", action, lang)
			}
		}
		if strings.Contains(body, "<details") || strings.Contains(detail, "<details") {
			t.Fatal("workspace management still hides rows in disclosures")
		}
	}
}

// TestOperatorWorkspaceJSON catches a route that works only for browser forms,
// or accepts unknown JSON authority fields from an API caller.
func TestOperatorWorkspaceJSON(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	token := b.csrf("/portal/")
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"workspace":"client-json"}`, http.StatusOK},
		{`{"workspace":"client-other","role":"operator"}`, http.StatusBadRequest},
		{`{"workspace":"client-other","subject":""}`, http.StatusBadRequest},
		{`{"workspace":"client-a"} {"workspace":"client-b"}`, http.StatusBadRequest},
	} {
		req, err := http.NewRequest("POST", b.site.URL+operatorAPIRoot+"/workspaces", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Origin", b.site.URL)
		req.Header.Set("X-CSRF-Token", token)
		response, err := b.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if response.StatusCode != tc.want || readErr != nil || closeErr != nil {
			t.Fatal("JSON management response", response.StatusCode, readErr, closeErr)
		}
	}
}

// TestOperatorWorkspaceMemberInventory catches a workspace query that returns
// another workspace's members, or accepts a client as an operator.
func TestOperatorWorkspaceMemberInventory(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,'member-a','client-a','client'),($1,'member-b','client-b','client')`, b.f.issuer); err != nil {
		t.Fatal(err)
	}
	status, body, _ := b.request("GET", operatorAPIRoot+"/workspaces/members?workspace=client-a", nil, "")
	var members []workspaceMember
	if err := json.Unmarshal([]byte(body), &members); status != http.StatusOK || err != nil || len(members) != 1 || members[0].Subject != "member-a" {
		t.Fatal("workspace query lost its scope", status, body, err)
	}
	b.signOut()
	if status, _, _ := b.request("GET", operatorAPIRoot+"/workspaces/members?workspace=client-a", nil, ""); status != http.StatusUnauthorized {
		t.Fatal("anonymous workspace-member access", status)
	}
	b.as("member-a")
	b.finish(b.start())
	if status, body, _ := b.request("GET", operatorAPIRoot+"/workspaces/members?workspace=client-b", nil, ""); status != http.StatusForbidden || strings.Contains(body, "member-b") {
		t.Fatal("customer reached another workspace's members", status)
	}
}
