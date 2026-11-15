package identity

import (
	"html"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const withdrawalPath = "/portal/requests/" + proposalRequestID + "/withdrawal"

// withdrawalForm reads the client's withdrawal from the receipt: an entry
// that says what it does, the last day, and a separate confirmation button
// (Consumer Contracts Act, section 20 a).
func withdrawalForm(t *testing.T, b *browserFixture, lang string) url.Values {
	t.Helper()
	body, f := receiptOrderForm(t, b, lang)
	labels := map[string][2]string{"da": {"Fortryd aftale", "Bekræft fortrydelse"}, "en": {"Withdraw from contract", "Confirm withdrawal"}}[lang]
	var placed time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT placed_at FROM portal_orders`).Scan(&placed); err != nil {
		t.Fatal(err)
	}
	last := projectOrder{Placed: placed}.WithdrawUntil()
	if !strings.Contains(body, "<summary>"+labels[0]+"</summary>") || !strings.Contains(body, ">"+labels[1]+"</button>") || !strings.Contains(body, " "+last+".") && !strings.Contains(body, " "+last+" ") {
		t.Fatalf("receipt offers no withdrawal until %s", last)
	}
	return f
}

// withdraw confirms the client's withdrawal from the English receipt.
func withdraw(t *testing.T, b *browserFixture) {
	t.Helper()
	if status, _, _ := b.request("POST", withdrawalPath, withdrawalForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatalf("withdrawal refused: %d", status)
	}
}

// stripeCancels copies the subscription end requests Stripe has received.
func stripeCancels(fake *fakeStripe) []url.Values {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]url.Values(nil), fake.cancels...)
}

// withdrawal reports whether the client's withdrawal is recorded, whether it
// left the order's end untouched, and the first billed month.
func withdrawal(t *testing.T, b *browserFixture) (withdrawn, ends bool, from time.Time) {
	t.Helper()
	if err := b.pool.QueryRow(b.f.ctx, `SELECT EXISTS (SELECT 1 FROM portal_order_withdrawals w WHERE w.client_subject IS NOT NULL),EXISTS (SELECT 1 FROM portal_order_ends),(SELECT monthly_from FROM portal_orders)`).Scan(&withdrawn, &ends, &from); err != nil {
		t.Fatal(err)
	}
	return withdrawn, ends, from
}

// TestInvoiceBillingClientWithdraws records the client's withdrawal within
// 14 days: the order then gets no invoice at all, and both the client and the
// operator see it.
func TestInvoiceBillingClientWithdraws(t *testing.T) {
	b, fake, cfg := danishOrderFixture(t)
	f := withdrawalForm(t, b, "da")
	extra := url.Values{}
	copyForm(extra, f)
	extra.Set("reason", "x")
	if status, _, _ := b.request("POST", withdrawalPath, extra, b.site.URL); status != 400 {
		t.Fatalf("extra field admitted: %d", status)
	}
	other := url.Values{}
	copyForm(other, f)
	other.Set("order", strings.Repeat("A", 26))
	if status, _, _ := b.request("POST", withdrawalPath, other, b.site.URL); status != 409 {
		t.Fatalf("another order's withdrawal admitted: %d", status)
	}
	for range 2 {
		if status, _, h := b.request("POST", withdrawalPath, f, b.site.URL); status != 303 || h.Get("Location") != "/portal/requests/"+proposalRequestID+"?lang=da" {
			t.Fatalf("withdrawal or retry refused: %d", status)
		}
	}
	if withdrawn, ends, _ := withdrawal(t, b); !withdrawn || ends {
		t.Fatal("withdrawal not recorded as its own record")
	}
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 0 || len(chargeStates(t, b)) != 0 {
		t.Fatal("a withdrawn order was invoiced", err)
	}
	if syncNextMonth(t, b, cfg); fake.count() != 0 || len(chargeStates(t, b)) != 0 {
		t.Fatal("a withdrawn order was invoiced a month")
	}
	_, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=da", nil, "")
	if !strings.Contains(body, "Du fortrød aftalen den") || strings.Contains(body, "Bekræft fortrydelse") || strings.Contains(body, "Opsig månedlig betaling") {
		t.Fatal("the receipt does not show the withdrawal")
	}
	triageSignIn(t, b, "withdrawal-operator", "operator-workspace", "operator")
	if _, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(body, "The client withdrew from the agreement on") || !strings.Contains(body, "Refund what was paid within 14 days") {
		t.Fatal("the operator page does not show the withdrawal")
	}
}

// TestPrivateCardClientWithdraws ends the card subscription before it first
// charges and removes the payment links. A later payment is still recorded,
// for the operator to refund, but never funds the order.
func TestPrivateCardClientWithdraws(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	withdraw(t, b)
	_, _, from := withdrawal(t, b)
	cancels := stripeCancels(fake)
	if len(cancels) != 1 || cancels[0].Get("subscription") != "sub_1" || cancels[0].Get("cancel_at") != strconv.FormatInt(from.Unix(), 10) || cancels[0].Get("proration_behavior") != "none" {
		t.Fatalf("the subscription does not end before its first charge: %v", cancels)
	}
	var confirmed time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT cancel_at FROM portal_withdrawal_cancellations WHERE subscription_id='sub_1'`).Scan(&confirmed); err != nil || !confirmed.Equal(from) {
		t.Fatal("Stripe's confirmed end was not recorded", err)
	}
	_, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(body, "You withdrew from the agreement on") || strings.Contains(body, "checkout.stripe.invalid") || strings.Contains(body, "End monthly billing") {
		t.Fatal("the receipt still offers payment after the withdrawal")
	}
	if sendEvent(t, b, "evt_late", "checkout.session.completed", map[string]any{"id": "cs_test_1", "mode": "payment", "payment_status": "paid", "amount_total": 375000, "currency": "dkk"}, webhookSecret) != 200 {
		t.Fatal("a late payment was refused")
	}
	renewed := map[string]any{"id": "in_after", "billing_reason": "subscription_cycle", "amount_paid": 12500, "currency": "dkk", "created": from.Add(time.Hour).Unix(), "parent": map[string]any{"subscription_details": map[string]any{"subscription": "sub_1", "metadata": map[string]any{"portal_order": orderID(t, b)}}}}
	if sendEvent(t, b, "evt_renewed", "invoice.paid", renewed, webhookSecret) != 200 || len(chargeStates(t, b)) != 1 {
		t.Fatal("a renewal after the withdrawal funded a month")
	}
}

// TestOrderWithdrawalEndsAfterFourteenDays offers and admits no withdrawal
// once the period has passed.
func TestOrderWithdrawalEndsAfterFourteenDays(t *testing.T) {
	b, _, _ := danishOrderFixture(t)
	f := withdrawalForm(t, b, "en")
	if _, err := migrationDB(t).Exec(t.Context(), `UPDATE portal_orders SET placed_at=placed_at-interval '30 days'`); err != nil {
		t.Fatal(err)
	}
	if _, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, ""); strings.Contains(body, "Withdraw from contract") {
		t.Fatal("an expired withdrawal is still offered")
	}
	if status, _, _ := b.request("POST", withdrawalPath, f, b.site.URL); status != 409 {
		t.Fatalf("an expired withdrawal was admitted: %d", status)
	}
	if withdrawn, _, _ := withdrawal(t, b); withdrawn {
		t.Fatal("an expired withdrawal was recorded")
	}
}

// TestWithdrawalDeadlineCountsFromTheNextDay counts 14 days from the day
// after the order, in Copenhagen, and moves a last day that is a weekend,
// public holiday, Constitution Day, 24 or 31 December to the next working day
// (Consumer Contracts Act, section 19(6)).
func TestWithdrawalDeadlineCountsFromTheNextDay(t *testing.T) {
	for placed, last := range map[string]string{
		"2026-11-16T10:00:00Z": "2026-11-30", // a Monday
		"2026-11-15T23:30:00Z": "2026-11-30", // already 16 November in Copenhagen
		"2026-11-14T10:00:00Z": "2026-11-30", // Saturday 28 November
		"2026-11-15T10:00:00Z": "2026-11-30", // Sunday 29 November
		"2026-03-20T12:00:00Z": "2026-04-07", // Good Friday to Easter Monday, after the switch to summer time
		"2026-12-10T12:00:00Z": "2026-12-28", // 24 December to the weekend
		"2027-12-17T12:00:00Z": "2028-01-03", // 31 December, New Year's Day and the weekend
		"2027-04-22T12:00:00Z": "2027-05-07", // Ascension Day
		"2027-05-03T12:00:00Z": "2027-05-18", // Whit Monday
		"2028-05-22T12:00:00Z": "2028-06-06", // Constitution Day
	} {
		at, _ := time.Parse(time.RFC3339, placed)
		o := projectOrder{Placed: at}
		if o.WithdrawUntil() != last {
			t.Errorf("an order placed %s may be withdrawn until %s, want %s", placed, o.WithdrawUntil(), last)
		}
		if deadline := withdrawalDeadline(at).In(copenhagen); deadline.Hour() != 0 || deadline.Minute() != 0 || deadline.AddDate(0, 0, -1).Format("2006-01-02") != last {
			t.Errorf("the period for %s ends at %v, not at midnight after %s", placed, deadline, last)
		}
	}
	for year, want := range map[int]string{2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28", 2028: "2028-04-16"} {
		if got := easterSunday(year).Format("2006-01-02"); got != want {
			t.Errorf("Easter %d is %s, want %s", year, got, want)
		}
	}
}

// TestInvoiceBillingInvoicesNothingAfterAWithdrawal leaves a charge recorded
// before the withdrawal without an invoice, even when its hold is later lifted.
func TestInvoiceBillingInvoicesNothingAfterAWithdrawal(t *testing.T) {
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	placeInvoiceOrder(t, b)
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "held" {
		t.Fatal("acceptance charge not held for the contact's country", err)
	}
	withdraw(t, b)
	fake.confirmDenmark()
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 0 || fake.sends != 0 || chargeStates(t, b)["acceptance"] != "held" {
		t.Fatal("a charge recorded before the withdrawal was invoiced after it", err)
	}
}

// TestPrivateCardWithdrawalOverridesALaterEnd ends the subscription before its
// first charge even when an operator had already scheduled a later end, and
// keeps that earlier record as it was.
func TestPrivateCardWithdrawalOverridesALaterEnd(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	client := b.client
	f, _ := operatorEndForm(t, b, "later-end-operator")
	operator := b.client
	later := currentMonth(time.Now()).AddDate(0, 3, 0)
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, later), b.site.URL); status != 303 {
		t.Fatalf("operator end refused: %d", status)
	}
	b.client = client
	withdraw(t, b)
	_, _, from := withdrawal(t, b)
	cancels := stripeCancels(fake)
	if len(cancels) != 2 || cancels[0].Get("cancel_at") != strconv.FormatInt(later.AddDate(0, 1, 0).Unix(), 10) || cancels[1].Get("cancel_at") != strconv.FormatInt(from.Unix(), 10) {
		t.Fatalf("the withdrawal did not bring the subscription's end forward: %v", cancels)
	}
	var operatorEnd, withdrawalEnd time.Time
	if err := b.pool.QueryRow(b.f.ctx, `SELECT (SELECT cancel_at FROM portal_card_cancellations),(SELECT cancel_at FROM portal_withdrawal_cancellations)`).Scan(&operatorEnd, &withdrawalEnd); err != nil || !operatorEnd.Equal(later.AddDate(0, 1, 0)) || !withdrawalEnd.Equal(from) {
		t.Fatal("the earlier end was changed or the withdrawal's end not recorded", err)
	}
	b.client = operator
	if _, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(body, "Stripe ends the subscription on "+from.Format("2006-01-02")) {
		t.Fatal("the operator page does not show the withdrawal's end")
	}
}

// TestPrivateCardWithdrawalKeepsAnEarlierEnd asks Stripe nothing more when the
// subscription already ends no later than the withdrawal would end it.
func TestPrivateCardWithdrawalKeepsAnEarlierEnd(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	if status, _, _ := b.request("POST", clientEndPath, clientEndForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatalf("client end refused: %d", status)
	}
	withdraw(t, b)
	cancels := len(stripeCancels(fake))
	var recorded bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT EXISTS (SELECT 1 FROM portal_withdrawal_cancellations x JOIN portal_card_cancellations c USING(subscription_id) WHERE x.cancel_at=c.cancel_at)`).Scan(&recorded); err != nil || cancels != 1 || !recorded {
		t.Fatalf("an earlier end was asked again (%d requests) or not taken over: %v", cancels, err)
	}
}

// TestPrivateCardWithdrawalKeepsItsCutoff refuses a later end of monthly
// billing after a withdrawal, so Stripe's earlier end is never pushed back.
func TestPrivateCardWithdrawalKeepsItsCutoff(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	withdraw(t, b)
	order := orderID(t, b)
	triageSignIn(t, b, "after-withdrawal-operator", "operator-workspace", "operator")
	_, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	if len(csrf) != 2 || strings.Contains(body, `name="last_month"`) {
		t.Fatal("the operator page offers to end a withdrawn order's billing")
	}
	f := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "order": {order}}
	if status, _, _ := b.request("POST", orderEndPath, endForm(f, currentMonth(time.Now()).AddDate(0, 3, 0)), b.site.URL); status != 409 {
		t.Fatalf("a later end after the withdrawal was admitted: %d", status)
	}
	var ends int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_order_ends`).Scan(&ends); err != nil || ends != 0 || len(stripeCancels(fake)) != 1 {
		t.Fatal("the withdrawal's end was changed", ends, err)
	}
}
