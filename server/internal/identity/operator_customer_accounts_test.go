package identity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/devantler-tech/business-site/server/internal/zitadel"
)

func TestCustomerAccountsShowsCustomersAfterWorkspaceAccessEnds(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	current := customeridentity.DexSubject("north-customer", customeridentity.ZitadelConnectorID)
	former := customeridentity.DexSubject("retired-customer", customeridentity.ZitadelConnectorID)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships (issuer,subject,client_id,role)
 VALUES ($1,$2,'client-a','client'),($1,$3,'client-b','client')`, b.f.issuer, current, former); err != nil {
		t.Fatal(err)
	}
	const activeAccount = `{"userId":"north-customer","state":"USER_STATE_ACTIVE","details":{"resourceOwner":"customers"},"human":{"profile":{"displayName":"North Customer"},"email":{"email":"north@example.test","isVerified":true}}}`
	const formerAccount = `{"userId":"retired-customer","state":"USER_STATE_INACTIVE","details":{"resourceOwner":"customers"},"human":{"profile":{"displayName":"Retired Customer"},"email":{"email":"retired@example.test"}}}`
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-zitadel-orgid") != "customers" {
			t.Error("customer organization was not bound")
			http.Error(w, "", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v2/users":
			var query json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
				t.Error(err)
			}
			if strings.Contains(string(query), "retired@example.test") {
				_, _ = fmt.Fprintf(w, `{"result":[%s]}`, formerAccount)
				return
			}
			_, _ = fmt.Fprintf(w, `{"result":[%s,%s]}`, activeAccount, formerAccount)
		case "/v2/users/retired-customer":
			_, _ = fmt.Fprintf(w, `{"user":%s}`, formerAccount)
		case "/v2/users/north-customer":
			_, _ = fmt.Fprintf(w, `{"user":%s}`, activeAccount)
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	client, err := zitadel.New(provider.URL, "customers", func() (string, error) { return "fixture-token", nil }, provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkCustomerDirectory(client); err != nil {
		t.Fatal(err)
	}
	const directory = "/portal/operator/customer-accounts"
	form := url.Values{"subject": {former}, "workspace": {"client-b"}, "csrf_token": {b.csrf("/portal/operator/customers")}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", form, b.site.URL); status != http.StatusOK {
		t.Fatalf("closing the fixture customer's access returned %d", status)
	}

	status, page, _ := b.request("GET", directory+"?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "North Customer") || !strings.Contains(page, "Retired Customer") ||
		!strings.Contains(page, "Former customer") || !strings.Contains(page, "north@example.test") {
		t.Fatalf("customer directory did not show both accounts: %d", status)
	}
	status, page, _ = b.request("GET", directory+"?lang=en&q=retired%40example.test", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Retired Customer") || strings.Contains(page, "North Customer") {
		t.Fatalf("email search returned the wrong customers: %d", status)
	}
	status, page, _ = b.request("GET", directory+"?lang=en&account="+former, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Account retention deadline") || strings.Contains(page, "/members/reactivate") {
		t.Fatalf("former account exposed the wrong management actions: %d", status)
	}

	b.signOut()
	b.as(current)
	b.finish(b.start())
	status, page, _ = b.request("GET", directory, nil, "")
	if status != http.StatusForbidden || strings.Contains(page, "retired@example.test") {
		t.Fatalf("client could read the customer directory: %d", status)
	}
	b.signOut()
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))

	detail := directory + "?account=" + current + "&lang=en"
	status, page, _ = b.request("GET", detail, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Suspend access") || !strings.Contains(page, "Remove access") {
		t.Fatalf("current account is missing access controls: %d", status)
	}
	confirmation := detail + "&view=remove"
	status, page, _ = b.request("GET", confirmation, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Remove workspace access") || !strings.Contains(page, "client-a") {
		t.Fatalf("account removal did not confirm the exact workspace: %d", status)
	}
	form = url.Values{"subject": {current}, "workspace": {"client-a"}, "csrf_token": {b.csrf(confirmation)}}
	request, err := http.NewRequestWithContext(t.Context(), "POST", b.site.URL+operatorAPIRoot+"/members/remove?lang=en&focus=customer", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Origin", b.site.URL)
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err = io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != detail {
		t.Fatalf("native removal did not return to the account: %d", response.StatusCode)
	}
	status, page, _ = b.request("GET", detail, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Former customer") || strings.Contains(page, "Suspend access") {
		t.Fatalf("removed customer still shows live workspace access: %d", status)
	}
}
