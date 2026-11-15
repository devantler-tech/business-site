package identity

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/stripe"
)

const webhookSecret = "whsec_fixture"

// fakeStripe answers Checkout Session creation like Stripe, honouring the
// idempotency key, and records every request for inspection.
type fakeStripe struct {
	mu         sync.Mutex
	requests   []url.Values
	keys       map[string]string
	bodies     map[string]string         // a key's request, so a changed reuse is refused like Stripe does
	loseReply  bool                      // create the session but answer 500, cached under its key like Stripe
	intents    map[string]string         // PaymentIntent → the Checkout Session or invoice it paid
	invoices   map[string]map[string]any // invoices by ID, for GET /v1/invoices/{id}
	cancels    []url.Values              // subscription end requests, with "subscription" added
	failCancel bool                      // answer the next subscription end with 500
	taxRate    map[string]any            // the configured tax rate, for GET /v1/tax_rates/{id}
	taxReads   int
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer rk_test_fixture" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == "GET" || r.Method == "DELETE" || strings.HasPrefix(r.URL.Path, "/v1/subscriptions/") {
		f.serveLookup(w, r)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/v1/checkout/sessions" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	_ = r.ParseForm()
	key, body := r.Header.Get("Idempotency-Key"), r.PostForm.Encode()
	id, ok := f.keys[key]
	switch {
	case ok && f.bodies[key] != body:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"idempotency_error"}}`))
		return
	case ok && id == "":
		// Stripe replays a cached failure, even a 500, for the same key.
		w.WriteHeader(http.StatusInternalServerError)
		return
	case !ok:
		f.requests = append(f.requests, r.PostForm)
		id = fmt.Sprintf("cs_test_%d", len(f.requests))
		f.bodies[key] = body
		if f.loseReply {
			// The session exists, but the caller only ever sees a 500.
			f.loseReply = false
			f.keys[key] = ""
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.keys[key] = id
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "url": "https://checkout.stripe.invalid/c/pay/" + id, "mode": r.PostForm.Get("mode"), "status": "open", "expires_at": time.Now().Add(24 * time.Hour).Unix()})
}

// serveLookup answers the PaymentIntent lookups and subscription ends.
func (f *fakeStripe) serveLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/checkout/sessions":
		data := []map[string]string{}
		if ref := f.intents[q.Get("payment_intent")]; strings.HasPrefix(ref, "cs_") {
			data = append(data, map[string]string{"id": ref})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.Method == "GET" && r.URL.Path == "/v1/invoice_payments" && q.Get("payment[type]") == "payment_intent":
		data := []map[string]string{}
		if ref := f.intents[q.Get("payment[payment_intent]")]; strings.HasPrefix(ref, "in_") {
			data = append(data, map[string]string{"invoice": ref})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/invoices/"):
		invoice, ok := f.invoices[strings.TrimPrefix(r.URL.Path, "/v1/invoices/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(invoice)
	case r.Method == "GET" && r.URL.Path == "/v1/tax_rates/txr_moms25":
		f.taxReads++
		_ = json.NewEncoder(w).Encode(f.taxRate)
	case r.Method == "DELETE":
		subscription := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		f.cancels = append(f.cancels, url.Values{"subscription": {subscription}, "immediately": {"true"}})
		_ = json.NewEncoder(w).Encode(map[string]any{"id": subscription, "canceled_at": time.Now().Unix()})
	case r.Method == "POST":
		_ = r.ParseForm()
		form := r.PostForm
		subscription := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		form.Set("subscription", subscription)
		f.cancels = append(f.cancels, form)
		if f.failCancel {
			f.failCancel = false
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		at, _ := strconv.ParseInt(form.Get("cancel_at"), 10, 64)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": subscription, "cancel_at": at})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeStripe) sessions() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.requests...)
}

// cardOrderFixture accepts a fictional proposal with the given prices and
// enables both collection paths, then signs the client in.
func cardOrderFixture(t *testing.T, oneTime, monthly int64) (*browserFixture, *fakeStripe) {
	t.Helper()
	b := newBrowserFixture(t)
	cardFlags(t, b, true)
	seedTriageRequest(t, b, proposalRequestID, "Fictional card request", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	operator := enroll(t, b.svc, b.f, "card-operator", "operator-workspace", "operator")
	if err := b.svc.persistProposal(t.Context(), operator, proposalRequestID, projectProposal{Key: secret(), Scope: "Fictional card scope", Deliverables: "Fictional deliverables", Currency: "DKK", OneTimeMinor: oneTime, MonthlyMinor: monthly, OneTimeTerms: "One iteration", MonthlyTerms: "Hosting"}); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.persistVisibility(t.Context(), operator, proposalRequestID, visibilityDecision{Key: secret(), ExpectedProposal: 1, Proposal: 1, Action: "share"}); err != nil {
		t.Fatal(err)
	}
	triageSignIn(t, b, "card-client", "private-client-key", "client")
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", acceptanceForm(t, b), b.site.URL); status != 303 {
		t.Fatalf("acceptance=%d", status)
	}
	if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES('private-client-key','Fictional Client','client@example.invalid','',1)`); err != nil {
		t.Fatal(err)
	}
	fake := &fakeStripe{keys: map[string]string{}, bodies: map[string]string{}, intents: map[string]string{}, invoices: map[string]map[string]any{},
		taxRate: map[string]any{"id": "txr_moms25", "active": true, "inclusive": true, "percentage": 25.0}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	api, err := stripe.New(server.URL, "rk_test_fixture", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.svc.EnableCardBilling(CardBilling{Stripe: api, WebhookSecret: webhookSecret, TaxRate: "txr_moms25", Now: time.Now}); err != nil {
		t.Fatal(err)
	}
	return b, fake
}

func cardFlags(t *testing.T, b *browserFixture, card any) {
	t.Helper()
	setFixturePortalFlags(t, b, append(acceptanceFlagNames(), "portal-invoice-billing", "portal-card-billing"), []any{true, true, true, true, true, true, true, card})
}

// placeCardOrder submits the receipt's own order form with the card path.
func placeCardOrder(t *testing.T, b *browserFixture) {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, "")
	acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	csrf := csrfInput.FindStringSubmatch(body)
	if status != 200 || len(acceptance) != 2 || len(csrf) != 2 || !strings.Contains(body, `id="collection-card" type="radio" name="collection" value="card"`) {
		t.Fatalf("receipt offers no card path: %d", status)
	}
	f := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "acceptance": {acceptance[1]}, "collection": {"card"}}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("card order refused: %d", status)
	}
}

// sendEvent posts a genuinely signed Stripe event to the real HTTPS handler.
func sendEvent(t *testing.T, b *browserFixture, id, kind string, object map[string]any, secret string) int {
	t.Helper()
	return sendEventTo(t, b, "/portal/stripe/webhook", id, kind, object, secret)
}

// sendEventTo posts the signed event to one webhook address.
func sendEventTo(t *testing.T, b *browserFixture, path, id, kind string, object map[string]any, secret string) int {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"id": id, "type": kind, "data": map[string]any{"object": object}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", b.site.URL+path, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", stripe.Sign(payload, secret, time.Now()))
	res, err := b.site.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	if err := res.Body.Close(); err != nil {
		t.Error("webhook response cleanup failed")
	}
	return res.StatusCode
}

func orderID(t *testing.T, b *browserFixture) string {
	t.Helper()
	var id string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT id FROM portal_orders`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// receipt returns the client's receipt page in one language.
func receipt(t *testing.T, b *browserFixture, lang string) string {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang="+lang, nil, "")
	if status != 200 {
		t.Fatalf("receipt status=%d", status)
	}
	return body
}

// TestPrivateCardOneOffIsFundedOnlyBySignedEvents covers a one-off order paid
// through Checkout (card or MobilePay), including an asynchronous payment.
func TestPrivateCardOneOffIsFundedOnlyBySignedEvents(t *testing.T) {
	b, fake := cardOrderFixture(t, 299500, 0)
	placeCardOrder(t, b)
	sessions := fake.sessions()
	if len(sessions) != 1 {
		t.Fatalf("checkout sessions=%d", len(sessions))
	}
	s := sessions[0]
	if s.Get("mode") != "payment" || s.Get("line_items[0][price_data][unit_amount]") != "299500" || s.Get("line_items[0][price_data][currency]") != "dkk" || s.Get("client_reference_id") != orderID(t, b) || s.Get("customer_email") != "client@example.invalid" || s.Has("line_items[0][price_data][recurring][interval]") || s.Has("payment_method_types[0]") {
		t.Fatalf("one-off checkout differs: %v", s)
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, `href="https://checkout.stripe.invalid/c/pay/cs_test_1"`) || len(chargeStates(t, b)) != 0 {
		t.Fatal("receipt has no payment link, or a browser return funded the order")
	}
	// Past its expiry time but without Stripe's expiry event, the session may
	// still have been completed, so no replacement is made beside it.
	if _, err := b.pool.Exec(b.f.ctx, `UPDATE portal_card_checkouts SET expires_at=clock_timestamp()-interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.ensureCheckouts(b.f.ctx, orderID(t, b), b.site.URL+"/portal", "en"); err != nil || len(fake.sessions()) != 1 || strings.Contains(receipt(t, b, "en"), "Start card payment") {
		t.Fatal("a session past its expiry time was replaced before Stripe expired it", err)
	}
	session := map[string]any{"id": "cs_test_1", "object": "checkout.session", "mode": "payment", "payment_status": "unpaid", "amount_total": 299500, "currency": "dkk"}
	if sendEvent(t, b, "evt_forged", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "paid", "amount_total": 299500, "currency": "dkk"}, "whsec_other") != 400 {
		t.Fatal("event signed with another secret was accepted")
	}
	if sendEvent(t, b, "evt_1", "checkout.session.completed", session, webhookSecret) != 200 || len(chargeStates(t, b)) != 0 {
		t.Fatal("an unpaid (asynchronous) completion funded the order")
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, "payment processing") || strings.Contains(body, "checkout.stripe.invalid") || strings.Contains(body, "Start card payment") {
		t.Fatal("a processing payment was not shown as processing")
	}
	session["payment_status"] = "paid"
	for range 2 {
		if sendEvent(t, b, "evt_2", "checkout.session.async_payment_succeeded", session, webhookSecret) != 200 {
			t.Fatal("verified payment event refused")
		}
	}
	var state, reference string
	var count int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT state,provider_ref,(SELECT count(*) FROM portal_order_charges) FROM portal_order_charges WHERE period='acceptance'`).Scan(&state, &reference, &count); err != nil || state != "settled" || reference != "cs_test_1" || count != 1 {
		t.Fatal("verified payment did not settle exactly one charge", err)
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, "Accepted proposal: 2995 DKK — paid") || strings.Contains(body, "checkout.stripe.invalid") || strings.Contains(body, "Start card payment") {
		t.Fatal("paid card order still offers payment")
	}
	// The card path never produces a Numina invoice for the same order.
	numinaFake, cfg := newFakeNumina(t)
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || numinaFake.count() != 0 {
		t.Fatal("a card order was invoiced in Numina too", err)
	}
}

// TestPrivateCardSubscriptionChargesMonthlyInAdvance covers an order with a
// one-time and a monthly amount: a one-off Checkout for the first, and a card
// subscription that charges nothing until 00:00 UTC on the first billed 1st.
func TestPrivateCardSubscriptionChargesMonthlyInAdvance(t *testing.T) {
	b, fake := cardOrderFixture(t, 375000, 12500)
	placeCardOrder(t, b)
	sessions := fake.sessions()
	if len(sessions) != 2 {
		t.Fatalf("checkout sessions=%d, want one-off and subscription", len(sessions))
	}
	oneOff, monthly := sessions[0], sessions[1]
	if oneOff.Get("mode") != "payment" || oneOff.Get("line_items[0][price_data][unit_amount]") != "375000" || oneOff.Has("line_items[1][quantity]") {
		t.Fatalf("one-off checkout differs: %v", oneOff)
	}
	anchorUnix, err := strconv.ParseInt(monthly.Get("subscription_data[billing_cycle_anchor]"), 10, 64)
	anchor := time.Unix(anchorUnix, 0).UTC()
	want := firstOfNextMonth(time.Now())
	if err != nil || anchor.Day() != 1 || anchor.Hour() != 0 || anchor.Minute() != 0 || anchor.Month() != want.Month() || anchor.In(copenhagen).Day() != 1 {
		t.Fatalf("subscription anchor %v is not 00:00 UTC on the first billed 1st", anchor)
	}
	if monthly.Get("mode") != "subscription" || monthly.Get("line_items[0][price_data][unit_amount]") != "12500" || monthly.Get("line_items[0][price_data][recurring][interval]") != "month" || monthly.Has("line_items[1][quantity]") || monthly.Get("subscription_data[proration_behavior]") != "none" || monthly.Get("subscription_data[metadata][portal_order]") != orderID(t, b) {
		t.Fatalf("subscription checkout differs: %v", monthly)
	}
	// Both carry Danish VAT inside the agreed price, so Stripe's invoices show
	// net, rate and VAT. The one-off payment asks for its paid invoice.
	for _, s := range sessions {
		if s.Get("line_items[0][tax_rates][0]") != "txr_moms25" || s.Get("billing_address_collection") != "required" || s.Get("tax_id_collection[enabled]") != "true" {
			t.Fatalf("checkout lacks the VAT invoice fields: %v", s)
		}
	}
	if oneOff.Get("invoice_creation[enabled]") != "true" || oneOff.Get("invoice_creation[invoice_data][rendering_options][amount_tax_display]") != "exclude_tax" || monthly.Has("invoice_creation[enabled]") {
		t.Fatal("only the one-off payment should ask Stripe for a paid invoice")
	}
	if fake.taxReads != 1 {
		t.Fatalf("the tax rate was read %d times, want once", fake.taxReads)
	}
	if body := receipt(t, b, "en"); strings.Count(body, `class="checkout-link"`) != 2 {
		t.Fatal("receipt does not offer both payments")
	}
	if sendEvent(t, b, "evt_one", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "paid", "amount_total": 375000, "currency": "dkk"}, webhookSecret) != 200 || chargeStates(t, b)["acceptance"] != "settled" {
		t.Fatal("the one-time payment did not fund the acceptance charge")
	}
	parent := func(sub string, marked bool) map[string]any {
		details := map[string]any{"subscription": sub}
		if marked {
			details["metadata"] = map[string]string{"portal_order": orderID(t, b)}
		}
		return map[string]any{"subscription_details": details}
	}
	month := anchor.Add(time.Hour)
	cycle := map[string]any{"id": "in_1", "billing_reason": "subscription_cycle", "amount_paid": 12500, "currency": "dkk", "created": month.Unix(), "parent": parent("sub_1", true)}
	if sendEvent(t, b, "evt_early", "invoice.paid", cycle, webhookSecret) != 500 || len(chargeStates(t, b)) != 1 {
		t.Fatal("a renewal before the subscription's completion was not deferred")
	}
	if sendEvent(t, b, "evt_foreign", "invoice.paid", map[string]any{"id": "in_x", "billing_reason": "subscription_cycle", "amount_paid": 100, "currency": "dkk", "parent": parent("sub_other", false)}, webhookSecret) != 200 {
		t.Fatal("another integration's subscription was not ignored")
	}
	// A retired session still resolves to its order, so its delayed completion
	// records the subscription.
	if sendEvent(t, b, "evt_sub_expired", "checkout.session.expired", map[string]any{"id": "cs_test_2", "mode": "subscription"}, webhookSecret) != 200 {
		t.Fatal("subscription session expiry refused")
	}
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 || len(chargeStates(t, b)) != 1 {
		t.Fatal("subscription completion funded a month without its invoice")
	}
	if sendEvent(t, b, "evt_early", "invoice.paid", cycle, webhookSecret) != 200 || chargeStates(t, b)[monthPeriod(month.In(copenhagen))] != "settled" {
		t.Fatalf("the retried renewal did not fund its month: %v", chargeStates(t, b))
	}
	wrong := map[string]any{"id": "in_2", "billing_reason": "subscription_cycle", "amount_paid": 9999, "currency": "dkk", "created": anchor.AddDate(0, 1, 0).Add(time.Hour).Unix(), "parent": parent("sub_1", true)}
	if sendEvent(t, b, "evt_wrong", "invoice.paid", wrong, webhookSecret) != 200 || len(chargeStates(t, b)) != 2 {
		t.Fatal("a renewal for a different amount funded a month")
	}
	if body := receipt(t, b, "da"); !strings.Contains(body, "aktiv, trækkes den 1. i måneden") || strings.Contains(body, "Start kortbetaling") {
		t.Fatal("the active subscription is not shown")
	}
}

// TestPrivateCardRetriesKeepEverySession covers a failed asynchronous payment
// that can be restarted, a delayed payment on a replaced session, and the
// closed gate.
func TestPrivateCardRetriesKeepEverySession(t *testing.T) {
	b, fake := cardOrderFixture(t, 299500, 0)
	placeCardOrder(t, b)
	unpaid := map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "unpaid", "amount_total": 299500, "currency": "dkk"}
	if sendEvent(t, b, "evt_unpaid", "checkout.session.completed", unpaid, webhookSecret) != 200 || sendEvent(t, b, "evt_failed", "checkout.session.async_payment_failed", unpaid, webhookSecret) != 200 {
		t.Fatal("asynchronous failure refused")
	}
	// The same unpaid completion delivered again after the failure keeps the
	// attempt closed.
	if sendEvent(t, b, "evt_unpaid_late", "checkout.session.completed", unpaid, webhookSecret) != 200 {
		t.Fatal("late unpaid completion refused")
	}
	body := receipt(t, b, "da")
	csrf := csrfInput.FindStringSubmatch(body)
	acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	if strings.Contains(body, "cs_test_1") || !strings.Contains(body, "Start kortbetaling") || len(csrf) != 2 || len(acceptance) != 2 {
		t.Fatal("a failed payment did not offer a new attempt")
	}
	restart := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"da"}, "acceptance": {acceptance[1]}, "collection": {"card"}}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", restart, b.site.URL); status != 303 || len(fake.sessions()) != 2 {
		t.Fatal("restart did not create a new checkout")
	}
	if body := receipt(t, b, "da"); !strings.Contains(body, "cs_test_2") || !strings.Contains(body, "Fortsæt til betaling") {
		t.Fatal("new checkout link not shown")
	}
	// The replaced session is kept, so a payment that still lands on it funds
	// its own order; an unknown session is another integration's.
	if sendEvent(t, b, "evt_old", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "paid", "amount_total": 299500, "currency": "dkk"}, webhookSecret) != 200 || chargeStates(t, b)["acceptance"] != "settled" {
		t.Fatal("a payment on a replaced session was lost")
	}
	if sendEvent(t, b, "evt_foreign_session", "checkout.session.completed", map[string]any{"id": "cs_test_other", "mode": "payment", "payment_status": "paid", "amount_total": 100, "currency": "dkk", "client_reference_id": orderID(t, b)}, webhookSecret) != 200 {
		t.Fatal("an unknown session was not ignored")
	}
	cardFlags(t, b, false)
	if body := receipt(t, b, "en"); strings.Contains(body, "checkout.stripe.invalid") {
		t.Fatal("closed card gate still shows a payment link")
	}
	if sendEvent(t, b, "evt_off", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "payment", "payment_status": "paid", "amount_total": 299500, "currency": "dkk"}, webhookSecret) != 404 {
		t.Fatal("closed card gate accepted a webhook")
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", restart, b.site.URL); status != 403 {
		t.Fatalf("closed card gate admitted a card order: %d", status)
	}
}

// TestPrivateCardRefusesAnUnconfirmedVATRate proves that no Checkout is made
// with a tax rate that would add VAT on top of the agreed price or put a wrong
// rate on the invoice, and that a corrected rate is then used.
func TestPrivateCardRefusesAnUnconfirmedVATRate(t *testing.T) {
	b, fake := cardOrderFixture(t, 299500, 0)
	rate := func(r map[string]any) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		fake.taxRate = r
	}
	restart := func() {
		t.Helper()
		body := receipt(t, b, "en")
		csrf := csrfInput.FindStringSubmatch(body)
		acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
		if strings.Contains(body, "checkout.stripe.invalid") || len(csrf) != 2 || len(acceptance) != 2 {
			t.Fatal("the receipt does not offer to start the payment again")
		}
		f := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "acceptance": {acceptance[1]}, "collection": {"card"}}
		if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
			t.Fatalf("restart refused: %d", status)
		}
	}
	rate(map[string]any{"id": "txr_moms25", "active": true, "inclusive": false, "percentage": 25.0})
	placeCardOrder(t, b)
	for _, wrong := range []map[string]any{
		{"id": "txr_moms25", "active": true, "inclusive": true, "percentage": 20.0},
		{"id": "txr_moms25", "active": false, "inclusive": true, "percentage": 25.0},
	} {
		if len(fake.sessions()) != 0 {
			t.Fatal("a checkout was created with an unconfirmed tax rate")
		}
		rate(wrong)
		restart()
	}
	if len(fake.sessions()) != 0 {
		t.Fatal("a checkout was created with an inactive tax rate")
	}
	rate(map[string]any{"id": "txr_moms25", "active": true, "inclusive": true, "percentage": 25.0})
	restart()
	if sessions := fake.sessions(); len(sessions) != 1 || sessions[0].Get("line_items[0][tax_rates][0]") != "txr_moms25" {
		t.Fatal("the corrected tax rate was not used")
	}
}

// TestPrivateCardRecoversALostCheckoutReply covers a session Stripe created
// but answered with a 500, which Stripe caches under its key: restarting with
// unchanged details still gets a session of its own.
func TestPrivateCardRecoversALostCheckoutReply(t *testing.T) {
	b, fake := cardOrderFixture(t, 299500, 0)
	fake.mu.Lock()
	fake.loseReply = true
	fake.mu.Unlock()
	placeCardOrder(t, b)
	body := receipt(t, b, "en")
	csrf := csrfInput.FindStringSubmatch(body)
	acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	if !strings.Contains(body, "Start card payment") || len(csrf) != 2 || len(acceptance) != 2 {
		t.Fatal("a lost Checkout reply did not offer a new attempt")
	}
	restart := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "acceptance": {acceptance[1]}, "collection": {"card"}}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", restart, b.site.URL); status != 303 || len(fake.sessions()) != 2 {
		t.Fatalf("the unchanged retry did not create its own session: %d", len(fake.sessions()))
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, `href="https://checkout.stripe.invalid/c/pay/cs_test_2"`) {
		t.Fatal("the recovered session is not offered")
	}
}
