package identity

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	completionPath = "/portal/operator/requests/" + proposalRequestID + "/order-completion"
	settlementPath = "/portal/operator/requests/" + proposalRequestID + "/withdrawal-settlement"
)

// placeEarlyStartOrder places an invoice order whose client ticked the
// optional request to start work within the withdrawal period.
func placeEarlyStartOrder(t *testing.T, b *browserFixture) {
	t.Helper()
	f := orderForm(t, b, "en")
	f.Set("early_start", "yes")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("early-start order refused: %d", status)
	}
}

// paidInvoiceOrder settles the order's acceptance invoice at Numina.
func paidInvoiceOrder(t *testing.T, b *browserFixture, fake *fakeNumina, cfg InvoiceBilling) {
	t.Helper()
	for _, step := range []func(){func() {}, fake.confirmDenmark} {
		step()
		if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
			t.Fatal(err)
		}
	}
	fake.pay("paid", "0")
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "settled" {
		t.Fatal("acceptance invoice not paid", err)
	}
}

// withdrawnPaidEarlyStart places a paid early-start invoice order and withdraws
// from it, returning the client's session.
func withdrawnPaidEarlyStart(t *testing.T) (*browserFixture, *http.Client) {
	t.Helper()
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	placeEarlyStartOrder(t, b)
	paidInvoiceOrder(t, b, fake, cfg)
	withdraw(t, b)
	return b, b.client
}

// operatorOrderForm signs an operator in and returns the session with the
// operator page and a form for its order actions.
func operatorOrderForm(t *testing.T, b *browserFixture, subject string) (*http.Client, string, url.Values) {
	t.Helper()
	triageSignIn(t, b, subject, "operator-workspace", "operator")
	status, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	if status != 200 || len(csrf) != 2 {
		t.Fatalf("operator page unavailable: %d", status)
	}
	return b.client, body, url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "order": {orderID(t, b)}}
}

// TestOrderRecordsTheEarlyStartRequest offers the request to start early as an
// optional box, not ticked in advance, dates it with the order, and keeps a retry from
// changing it.
func TestOrderRecordsTheEarlyStartRequest(t *testing.T) {
	b := billingFixture(t)
	_, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=da", nil, "")
	if !strings.Contains(body, `<input id="early-start" type="checkbox" name="early_start" value="yes">Begynd arbejdet inden for fortrydelsesfristen på 14 dage.`) || strings.Contains(body, `name="early_start" value="yes" checked`) {
		t.Fatal("the order step does not offer an optional early-start request that is not ticked")
	}
	bad := orderForm(t, b, "en")
	bad.Set("early_start", "on")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", bad, b.site.URL); status != 400 {
		t.Fatalf("an unknown early-start value was admitted: %d", status)
	}
	placeEarlyStartOrder(t, b)
	var same bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT early_start_at=placed_at FROM portal_orders`).Scan(&same); err != nil || !same {
		t.Fatal("the early-start request is not dated with the order", err)
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", orderRetry(t, b), b.site.URL); status != 409 {
		t.Fatalf("a retry without the early-start request was admitted: %d", status)
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, "You asked for work to begin within the withdrawal period") || !strings.Contains(body, "You pay for the work done until you withdraw") {
		t.Fatal("the receipt does not confirm the early-start request")
	}
}

// orderRetry repeats the placed order with the same acceptance and path but
// without the early-start request.
func orderRetry(t *testing.T, b *browserFixture) url.Values {
	t.Helper()
	_, f := receiptOrderForm(t, b, "en")
	var acceptance string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT acceptance_id FROM portal_orders`).Scan(&acceptance); err != nil {
		t.Fatal(err)
	}
	return url.Values{"csrf_token": f["csrf_token"], "lang": {"en"}, "acceptance": {acceptance}, "collection": {"invoice"}}
}

// TestOrderWithoutEarlyStartWaits starts no work within the withdrawal period
// and promises a full refund on withdrawal.
func TestOrderWithoutEarlyStartWaits(t *testing.T) {
	b := billingFixture(t)
	placeInvoiceOrder(t, b)
	var early bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT early_start_at IS NOT NULL FROM portal_orders`).Scan(&early); err != nil || early {
		t.Fatal("an early start was recorded without the client's request", err)
	}
	var placed time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT placed_at FROM portal_orders`).Scan(&placed); err != nil {
		t.Fatal(err)
	}
	from := projectOrder{Placed: placed}.WorkStartsFrom()
	if body := receipt(t, b, "en"); !strings.Contains(body, "Work begins on "+from+" at the earliest") || !strings.Contains(body, "you get back what you have paid without undue delay") {
		t.Fatal("the receipt does not say when work may start")
	}
	_, body, f := operatorOrderForm(t, b, "waiting-operator")
	if !strings.Contains(body, "No early start: work may begin on "+from) || strings.Contains(body, "Mark the work completely finished") {
		t.Fatal("the operator page does not show that work waits")
	}
	if status, _, _ := b.request("POST", completionPath, f, b.site.URL); status != 409 {
		t.Fatalf("work without an early start was marked finished: %d", status)
	}
}

// TestEarlyStartWithdrawalSettlement records what the client owes for the work
// done before withdrawing, and refunds the rest.
func TestEarlyStartWithdrawalSettlement(t *testing.T) {
	b, client := withdrawnPaidEarlyStart(t)
	if body := receipt(t, b, "en"); !strings.Contains(body, "You pay for the work done until you withdrew") {
		t.Fatal("the receipt does not say the work done is paid for")
	}
	_, body, f := operatorOrderForm(t, b, "settling-operator")
	if !strings.Contains(body, `name="owed_dkk"`) || !strings.Contains(body, `max="3094"`) || !strings.Contains(body, "refund only what was paid beyond it") {
		t.Fatal("the operator page offers no settlement up to the agreed amounts")
	}
	for name, owed := range map[string]string{"negative": "-1", "decimal": "10.5", "leading zero": "01"} {
		g := url.Values{}
		copyForm(g, f)
		g.Set("owed_dkk", owed)
		if status, _, _ := b.request("POST", settlementPath, g, b.site.URL); status != 400 {
			t.Fatalf("%s amount admitted: %d", name, status)
		}
	}
	over := url.Values{}
	copyForm(over, f)
	over.Set("owed_dkk", "3095")
	if status, _, _ := b.request("POST", settlementPath, over, b.site.URL); status != 409 {
		t.Fatalf("more than the agreed amounts admitted: %d", status)
	}
	f.Set("owed_dkk", "1000")
	for range 2 {
		if status, _, _ := b.request("POST", settlementPath, f, b.site.URL); status != 303 {
			t.Fatalf("settlement or retry refused: %d", status)
		}
	}
	f.Set("owed_dkk", "1200")
	if status, _, _ := b.request("POST", settlementPath, f, b.site.URL); status != 409 {
		t.Fatalf("a different settlement admitted: %d", status)
	}
	if _, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(body, "The client owes 1000 DKK for the work done.") || strings.Contains(body, `name="owed_dkk"`) {
		t.Fatal("the operator page does not show the settlement")
	}
	b.client = client
	if body := receipt(t, b, "da"); !strings.Contains(body, "Du betaler 1000 DKK for det udførte arbejde; resten tilbagebetales uden unødig forsinkelse") {
		t.Fatal("the receipt does not show the amount owed and the refund")
	}
}

// TestEarlyStartSettlementCoveringThePayment refunds nothing when the work done
// costs what was paid.
func TestEarlyStartSettlementCoveringThePayment(t *testing.T) {
	b, client := withdrawnPaidEarlyStart(t)
	_, _, f := operatorOrderForm(t, b, "covering-operator")
	f.Set("owed_dkk", "2995")
	if status, _, _ := b.request("POST", settlementPath, f, b.site.URL); status != 303 {
		t.Fatalf("settlement refused: %d", status)
	}
	b.client = client
	if body := receipt(t, b, "en"); !strings.Contains(body, "You pay 2995 DKK for the work done.") || strings.Contains(body, "the rest is refunded") {
		t.Fatal("the receipt promises a refund of nothing")
	}
}

// TestEarlyStartCompletionEndsTheRight ends the right of withdrawal once the
// early-started work is marked completely finished, and refuses a settlement
// for a withdrawal without an early start.
func TestEarlyStartCompletionEndsTheRight(t *testing.T) {
	b := billingFixture(t)
	placeEarlyStartOrder(t, b)
	client := b.client
	f := withdrawalForm(t, b, "en")
	_, body, g := operatorOrderForm(t, b, "completing-operator")
	if !strings.Contains(body, "The client asked for work to begin within the withdrawal period") || !strings.Contains(body, "Mark the work completely finished") {
		t.Fatal("the operator page does not offer to mark the work finished")
	}
	for range 2 {
		if status, _, _ := b.request("POST", completionPath, g, b.site.URL); status != 303 {
			t.Fatalf("completion or retry refused: %d", status)
		}
	}
	b.client = client
	if body := receipt(t, b, "da"); !strings.Contains(body, "Fortrydelsesretten ophørte den") || strings.Contains(body, "Bekræft fortrydelse") {
		t.Fatal("the receipt still offers the withdrawal")
	}
	if status, _, _ := b.request("POST", withdrawalPath, f, b.site.URL); status != 409 {
		t.Fatalf("a withdrawal after the work was finished was admitted: %d", status)
	}
}

// TestWithdrawalWithoutEarlyStartOwesNothing offers no settlement: the client
// gets back everything they paid.
func TestWithdrawalWithoutEarlyStartOwesNothing(t *testing.T) {
	b := billingFixture(t)
	placeInvoiceOrder(t, b)
	withdraw(t, b)
	_, body, f := operatorOrderForm(t, b, "refunding-operator")
	if strings.Contains(body, `name="owed_dkk"`) {
		t.Fatal("the operator page offers a settlement without an early start")
	}
	f.Set("owed_dkk", "100")
	if status, _, _ := b.request("POST", settlementPath, f, b.site.URL); status != 409 {
		t.Fatalf("a settlement without an early start was admitted: %d", status)
	}
}

// TestPrivateCardRestartKeepsTheEarlyStartRequest lets a client whose payment
// session closed start the payment again without changing their request to
// start early.
func TestPrivateCardRestartKeepsTheEarlyStartRequest(t *testing.T) {
	b, fake := cardOrderFixture(t, 299500, 0)
	body := receipt(t, b, "en")
	csrf := csrfInput.FindStringSubmatch(body)
	acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	if len(csrf) != 2 || len(acceptance) != 2 {
		t.Fatal("receipt has no order form")
	}
	order := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "acceptance": {acceptance[1]}, "collection": {"card"}, "early_start": {"yes"}}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", order, b.site.URL); status != 303 {
		t.Fatalf("early-start card order refused: %d", status)
	}
	if sendEvent(t, b, "evt_expired", "checkout.session.expired", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "unpaid", "amount_total": 299500, "currency": "dkk"}, webhookSecret) != 200 {
		t.Fatal("expiry refused")
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, `name="collection" value="card"><input type="hidden" name="early_start" value="yes">`) {
		t.Fatal("the restart form drops the early-start request")
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", order, b.site.URL); status != 303 || len(fake.sessions()) != 2 {
		t.Fatalf("restart with the same request refused: %d", status)
	}
}
