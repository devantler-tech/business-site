package identity

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// newRequestForm reads the client's new-request form, filled in.
func newRequestForm(t *testing.T, b *browserFixture, title string) url.Values {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/new?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	key := regexp.MustCompile(`name="submission_key" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	if status != 200 || len(csrf) != 2 || len(key) != 2 {
		t.Fatalf("no new-request form: %d", status)
	}
	return url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "submission_key": {key[1]}, "product": {"app"}, "title": {title}, "brief": {"A fictional feature request."}}
}

// newRequest submits the client's new-request form and returns its status.
func newRequest(t *testing.T, b *browserFixture, title string) (int, string) {
	t.Helper()
	status, body, _ := b.request("POST", "/portal/requests", newRequestForm(t, b, title), b.site.URL)
	return status, body
}

// expressClient is a hosting client whose order includes Express.
func expressClient(t *testing.T) *browserFixture {
	t.Helper()
	b := billingFixture(t)
	f := orderForm(t, b, "en")
	f["addon"] = []string{"express:1:1000"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("order with Express=%d", status)
	}
	return b
}

// TestFairUseOneRequestInProgress gives a hosting client with Express one
// request in progress at a time: the next waits until the operator marks the
// current one finished, and the operator sees the client's level.
func TestFairUseOneRequestInProgress(t *testing.T) {
	b := expressClient(t)
	if status, body := newRequest(t, b, "Fictional second request"); status != 409 || !strings.Contains(body, "You already have a request in progress.") {
		t.Fatalf("a second request in progress was admitted: %d", status)
	}
	client := b.client
	triageSignIn(t, b, "fair-use-operator", "operator-workspace", "operator")
	_, page, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(page)
	if len(csrf) != 2 || !strings.Contains(page, "Feature requests: Express · this request is in progress") || !strings.Contains(page, ">Mark the request finished</button>") {
		t.Fatal("the operator does not see the client's level and the request in progress")
	}
	finished := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}}
	for range 2 {
		if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/finished", finished, b.site.URL); status != 303 {
			t.Fatalf("marking the request finished=%d", status)
		}
	}
	if _, page, _ = b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(page, "this request finished on") || strings.Contains(page, "Mark the request finished") {
		t.Fatal("the finished request is not shown as finished")
	}
	b.client = client
	if status, _ := newRequest(t, b, "Fictional second request"); status != 303 {
		t.Fatalf("the next request was refused after the first finished: %d", status)
	}
	if status, _ := newRequest(t, b, "Fictional third request"); status != 409 {
		t.Fatalf("a third request was admitted while the second is in progress: %d", status)
	}
}

// TestFeatureLevels names the highest tier among the client's orders that
// still bill, and Standard without one.
func TestFeatureLevels(t *testing.T) {
	b := billingFixture(t)
	f := orderForm(t, b, "en")
	f["addon"] = []string{"priority:1:400"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("order with Priority=%d", status)
	}
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key"); err != nil || level != "priority" {
		t.Fatalf("level=%q %v", level, err)
	}
	if _, err := migrationDB(t).Exec(b.f.ctx, `INSERT INTO portal_order_withdrawals(order_id,client_issuer,client_subject) SELECT id,client_issuer,client_subject FROM portal_orders`); err != nil {
		t.Fatal(err)
	}
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key"); err != nil || level != "standard" {
		t.Fatalf("a withdrawn order kept its tier: %q %v", level, err)
	}
	if status, _ := newRequest(t, b, "Fictional request without hosting"); status != 303 {
		t.Fatalf("a client without hosting was held to one request: %d", status)
	}
}

// TestFairUseKeepsRetries returns a finished request's receipt to a retry of
// its own submission while another request is in progress.
func TestFairUseKeepsRetries(t *testing.T) {
	b := expressClient(t)
	if _, err := migrationDB(t).Exec(b.f.ctx, `INSERT INTO portal_request_closures(request_id,client_id,operator_issuer,operator_subject) VALUES($1,'private-client-key','https://fixture.invalid','fixture-operator')`, proposalRequestID); err != nil {
		t.Fatal(err)
	}
	first := newRequestForm(t, b, "Fictional first request")
	if status, _, _ := b.request("POST", "/portal/requests", first, b.site.URL); status != 303 {
		t.Fatalf("first request=%d", status)
	}
	if _, err := migrationDB(t).Exec(b.f.ctx, `INSERT INTO portal_request_closures(request_id,client_id,operator_issuer,operator_subject) SELECT id,client_id,'https://fixture.invalid','fixture-operator' FROM portal_requests WHERE submission_key=$1`, first.Get("submission_key")); err != nil {
		t.Fatal(err)
	}
	if status, _ := newRequest(t, b, "Fictional second request"); status != 303 {
		t.Fatalf("second request=%d", status)
	}
	if status, _, _ := b.request("POST", "/portal/requests", first, b.site.URL); status != 303 {
		t.Fatalf("the retry of a finished request's submission was held to fair use: %d", status)
	}
}
