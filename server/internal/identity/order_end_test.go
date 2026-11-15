package identity

import (
	"html"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const orderEndPath = "/portal/operator/requests/" + proposalRequestID + "/order-end"

// operatorEndForm signs in an operator and reads the request page's own end
// form: its CSRF token and order.
func operatorEndForm(t *testing.T, b *browserFixture, subject string) (url.Values, string) {
	t.Helper()
	_, body, f := operatorOrderForm(t, b, subject)
	if !strings.Contains(body, "End monthly billing") {
		t.Fatal("operator page offers no end form")
	}
	return f, body
}

// endForm returns the form with a last billed month.
func endForm(f url.Values, month time.Time) url.Values {
	g := url.Values{}
	for k, v := range f {
		g[k] = v
	}
	g.Set("last_month", month.Format("2006-01"))
	return g
}

// bookedDanishOrder places a Danish invoice order and books its acceptance
// invoice, with the client still signed in.
func bookedDanishOrder(t *testing.T) (*browserFixture, *fakeNumina, InvoiceBilling) {
	t.Helper()
	b, fake, cfg := danishOrderFixture(t)
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "booked" {
		t.Fatal("acceptance invoice not booked", err)
	}
	return b, fake, cfg
}

// subscribedCardOrder places a card order with a one-time and a monthly
// amount whose subscription sub_1 has completed, with the client signed in.
func subscribedCardOrder(t *testing.T) (*browserFixture, *fakeStripe) {
	t.Helper()
	b, fake := cardOrderFixture(t, 375000, 12500)
	placeCardOrder(t, b)
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	return b, fake
}

// TestInvoiceBillingStopsAfterTheLastBilledMonth ends an invoice order's
// monthly billing in the month it was placed: no monthly invoice follows.
func TestInvoiceBillingStopsAfterTheLastBilledMonth(t *testing.T) {
	b, fake, cfg := bookedDanishOrder(t)
	f, _ := operatorEndForm(t, b, "end-operator")
	now := currentMonth(time.Now())
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, now.AddDate(0, -1, 0)), b.site.URL); status != 409 {
		t.Fatalf("a new end in a past month admitted: %d", status)
	}
	for name, bad := range map[string]url.Values{
		"distant month":  endForm(f, now.AddDate(11, 0, 0)),
		"no month":       f,
		"extra field":    func() url.Values { g := endForm(f, now); g.Set("amount", "1"); return g }(),
		"repeated month": func() url.Values { g := endForm(f, now); g.Add("last_month", now.Format("2006-01")); return g }(),
	} {
		if status, _, _ := b.request("POST", orderEndPath, bad, b.site.URL); status != 400 {
			t.Fatalf("%s admitted: %d", name, status)
		}
	}
	other := endForm(f, now)
	other.Set("order", strings.Repeat("A", 26))
	if status, _, _ := b.request("POST", orderEndPath, other, b.site.URL); status != 409 {
		t.Fatalf("another order's end admitted: %d", status)
	}
	for range 2 {
		if status, _, h := b.request("POST", orderEndPath, endForm(f, now), b.site.URL); status != 303 || !strings.HasSuffix(h.Get("Location"), "?lang=en") {
			t.Fatalf("end or retry refused: %d", status)
		}
	}
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, now.AddDate(0, 1, 0)), b.site.URL); status != 409 {
		t.Fatalf("a second, different end admitted: %d", status)
	}
	month := syncNextMonth(t, b, cfg)
	if states := chargeStates(t, b); len(states) != 1 || fake.count() != 1 {
		t.Fatalf("billing continued after the last month (%s): %v", month, states)
	}
	_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=da", nil, "")
	if !strings.Contains(body, "Månedlig betaling slutter efter "+now.Format("2006-01")) || strings.Contains(body, "Afslut månedlig betaling") {
		t.Fatal("operator page does not show the end")
	}
	billingFlags(t, b, false)
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, now), b.site.URL); status != 403 {
		t.Fatalf("closed gate admitted an end: %d", status)
	}
}

// TestPrivateCardEndsMonthlyBillingAtTheNextRenewal ends a card order: Stripe
// is asked to end the subscription when it would next renew, a failed request
// can be repeated, and a renewal after the end never funds a month.
func TestPrivateCardEndsMonthlyBillingAtTheNextRenewal(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	order := orderID(t, b)
	f, _ := operatorEndForm(t, b, "card-end-operator")
	now := currentMonth(time.Now())
	fake.mu.Lock()
	fake.failCancel = true
	fake.mu.Unlock()
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, now), b.site.URL); status != 303 {
		t.Fatalf("card end refused: %d", status)
	}
	_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(body, "Stripe has not confirmed the subscription") || !strings.Contains(body, "Ask Stripe again") {
		t.Fatal("an unconfirmed Stripe end is not offered again")
	}
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, now), b.site.URL); status != 303 {
		t.Fatalf("retry refused: %d", status)
	}
	fake.mu.Lock()
	cancels := append([]url.Values(nil), fake.cancels...)
	fake.mu.Unlock()
	renewal := now.AddDate(0, 1, 0)
	last := cancels[len(cancels)-1]
	if len(cancels) != 2 || last.Get("subscription") != "sub_1" || last.Get("cancel_at") != strconv.FormatInt(renewal.Unix(), 10) || last.Get("proration_behavior") != "none" {
		t.Fatalf("subscription end request differs: %v", cancels)
	}
	_, body, _ = b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(body, "Stripe ends the subscription on "+renewal.Format("2006-01-02")) || strings.Contains(body, "Ask Stripe again") {
		t.Fatal("the confirmed end is not shown")
	}
	renewed := map[string]any{"id": "in_after", "billing_reason": "subscription_cycle", "amount_paid": 12500, "currency": "dkk", "created": renewal.Add(time.Hour).Unix(), "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_1", "metadata": map[string]string{"portal_order": order}}}}
	if sendEvent(t, b, "evt_after", "invoice.paid", renewed, webhookSecret) != 200 || len(chargeStates(t, b)) != 0 {
		t.Fatal("a renewal after the last billed month funded a month")
	}
	if sendEvent(t, b, "evt_deleted", "customer.subscription.deleted", map[string]any{"id": "sub_1", "ended_at": renewal.Unix()}, webhookSecret) != 200 {
		t.Fatal("subscription end refused")
	}
	var source string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT source FROM portal_order_ends WHERE order_id=$1`, order).Scan(&source); err != nil || source != "operator" {
		t.Fatal("Stripe's end replaced the operator's", source, err)
	}
}

// TestPrivateCardRefundsAndDisputesStopFunding reverses a funded one-time
// payment and a funded month through Stripe's own PaymentIntent references.
func TestPrivateCardRefundsAndDisputesStopFunding(t *testing.T) {
	b, fake := cardOrderFixture(t, 375000, 12500)
	placeCardOrder(t, b)
	order := orderID(t, b)
	fake.mu.Lock()
	fake.intents["pi_one"], fake.intents["pi_month"] = "cs_test_1", "in_1"
	fake.mu.Unlock()
	if sendEvent(t, b, "evt_early_refund", "charge.refunded", map[string]any{"id": "ch_1", "payment_intent": "pi_one", "amount_refunded": 375000}, webhookSecret) != 500 {
		t.Fatal("a refund of the portal's own unfunded payment was not deferred")
	}
	if sendEvent(t, b, "evt_one", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "paid", "amount_total": 375000, "currency": "dkk"}, webhookSecret) != 200 {
		t.Fatal("one-time payment refused")
	}
	anchor := firstAnchor(firstOfNextMonth(time.Now()), time.Now())
	monthly := map[string]any{"id": "in_1", "billing_reason": "subscription_cycle", "amount_paid": 12500, "currency": "dkk", "created": anchor.Add(time.Hour).Unix(), "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_1", "metadata": map[string]string{"portal_order": order}}}}
	fake.mu.Lock()
	fake.invoices["in_1"] = monthly
	fake.mu.Unlock()
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	if sendEvent(t, b, "evt_dispute", "charge.dispute.created", map[string]any{"id": "du_1", "payment_intent": "pi_month"}, webhookSecret) != 500 {
		t.Fatal("a dispute of the portal's own unfunded month was not deferred")
	}
	if sendEvent(t, b, "evt_month", "invoice.paid", monthly, webhookSecret) != 200 {
		t.Fatal("subscription funding refused")
	}
	for _, e := range []struct {
		id, kind string
		object   map[string]any
	}{
		{"evt_early_refund", "charge.refunded", map[string]any{"id": "ch_1", "payment_intent": "pi_one", "amount_refunded": 375000}},
		{"evt_dispute", "charge.dispute.created", map[string]any{"id": "du_1", "payment_intent": "pi_month"}},
		{"evt_foreign", "charge.refunded", map[string]any{"id": "ch_x", "payment_intent": "pi_other", "amount_refunded": 100}},
		{"evt_zero", "charge.refunded", map[string]any{"id": "ch_y", "payment_intent": "pi_one", "amount_refunded": 0}},
	} {
		if sendEvent(t, b, e.id, e.kind, e.object, webhookSecret) != 200 {
			t.Fatalf("%s refused", e.id)
		}
	}
	var reversals string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT string_agg(period||':'||reason||':'||reference,',' ORDER BY period) FROM portal_charge_reversals`).Scan(&reversals); err != nil {
		t.Fatal(err)
	}
	month := monthPeriod(anchor.In(copenhagen))
	if reversals != month+":disputed:du_1,acceptance:refunded:ch_1" && reversals != "acceptance:refunded:ch_1,"+month+":disputed:du_1" {
		t.Fatalf("reversals differ: %s", reversals)
	}
	for lang, want := range map[string][]string{"en": {"3750 DKK — refunded", "125 DKK — disputed"}, "da": {"3750 DKK — refunderet", "125 DKK — bestridt"}} {
		body := receipt(t, b, lang)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("receipt (%s) does not show %q", lang, w)
			}
		}
	}
	_, body := operatorEndForm(t, b, "reversal-operator")
	if strings.Count(body, "no longer funds the order") != 2 {
		t.Fatal("operator page does not show both reversals")
	}
}

// TestPrivateCardSubscriptionEndedByStripe records Stripe's own end of a
// subscription, for example after failed renewals, as the last billed month.
func TestPrivateCardSubscriptionEndedByStripe(t *testing.T) {
	b, _ := cardOrderFixture(t, 0, 12500)
	placeCardOrder(t, b)
	ended := time.Date(2027, 3, 14, 9, 0, 0, 0, time.UTC)
	gone := map[string]any{"id": "sub_1", "ended_at": ended.Unix(), "metadata": map[string]string{"portal_order": orderID(t, b)}}
	if sendEvent(t, b, "evt_gone", "customer.subscription.deleted", gone, webhookSecret) != 500 {
		t.Fatal("an end before the subscription's completion was not deferred")
	}
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	if sendEvent(t, b, "evt_gone", "customer.subscription.deleted", gone, webhookSecret) != 200 ||
		sendEvent(t, b, "evt_other", "customer.subscription.deleted", map[string]any{"id": "sub_other", "ended_at": ended.Unix()}, webhookSecret) != 200 {
		t.Fatal("subscription end refused")
	}
	if body := receipt(t, b, "en"); !strings.Contains(body, "Monthly billing ends") || !strings.Contains(body, "after 2027-03") {
		t.Fatal("receipt does not show Stripe's end")
	}
	triageSignIn(t, b, "stripe-end-operator", "operator-workspace", "operator")
	_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(body, "Monthly billing ends after 2027-03 · Stripe ended the subscription") || strings.Contains(body, "End monthly billing") || strings.Contains(body, "Ask Stripe again") {
		t.Fatal("operator page does not show Stripe's end")
	}
}

// TestPrivateCardRetriesAnEndAfterItsMonthPassed covers an operator end that
// Stripe never confirmed before its renewal: asking again ends the
// subscription at once.
func TestPrivateCardRetriesAnEndAfterItsMonthPassed(t *testing.T) {
	b, fake := cardOrderFixture(t, 0, 12500)
	placeCardOrder(t, b)
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	past := currentMonth(time.Now()).AddDate(0, -1, 0)
	if _, err := migrationDB(t).Exec(t.Context(), `INSERT INTO portal_order_ends(order_id,last_month,source,operator_issuer,operator_subject) VALUES($1,$2,'operator','https://fixture.invalid','earlier-operator')`, orderID(t, b), past); err != nil {
		t.Fatal(err)
	}
	triageSignIn(t, b, "late-end-operator", "operator-workspace", "operator")
	_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	if len(csrf) != 2 || !strings.Contains(body, "Ask Stripe again") {
		t.Fatal("an unconfirmed past end is not offered again")
	}
	retry := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "order": {orderID(t, b)}, "last_month": {past.Format("2006-01")}}
	if status, _, _ := b.request("POST", orderEndPath, retry, b.site.URL); status != 303 {
		t.Fatalf("retry of a past end refused: %d", status)
	}
	fake.mu.Lock()
	cancels := append([]url.Values(nil), fake.cancels...)
	fake.mu.Unlock()
	if len(cancels) != 1 || cancels[0].Get("subscription") != "sub_1" || cancels[0].Get("immediately") != "true" {
		t.Fatalf("a passed renewal was not ended at once: %v", cancels)
	}
	_, body, _ = b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(body, "Stripe ends the subscription on "+time.Now().UTC().Format("2006-01-02")) || strings.Contains(body, "Ask Stripe again") {
		t.Fatal("the immediate end is not shown")
	}
}
