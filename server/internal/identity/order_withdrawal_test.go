package identity

import (
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
	last := placed.In(copenhagen).AddDate(0, 0, withdrawalDays).Format("2006-01-02")
	if !strings.Contains(body, "<summary>"+labels[0]+"</summary>") || !strings.Contains(body, ">"+labels[1]+"</button>") || !strings.Contains(body, " "+last+".") && !strings.Contains(body, " "+last+" ") {
		t.Fatalf("receipt offers no withdrawal until %s", last)
	}
	return f
}

func withdrawal(t *testing.T, b *browserFixture) (withdrawn bool, last time.Time, from time.Time) {
	t.Helper()
	if err := b.pool.QueryRow(b.f.ctx, `SELECT EXISTS (SELECT 1 FROM portal_order_withdrawals w WHERE w.client_subject IS NOT NULL),coalesce((SELECT last_month FROM portal_order_ends WHERE source='client'),'0001-01-01'),(SELECT monthly_from FROM portal_orders)`).Scan(&withdrawn, &last, &from); err != nil {
		t.Fatal(err)
	}
	return withdrawn, last, from
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
	withdrawn, last, from := withdrawal(t, b)
	if !withdrawn || !last.Equal(from.AddDate(0, -1, 0)) {
		t.Fatalf("withdrawal not recorded, or monthly billing does not end before %v (%v)", from, last)
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
	if status, _, _ := b.request("POST", withdrawalPath, withdrawalForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatalf("withdrawal refused: %d", status)
	}
	_, _, from := withdrawal(t, b)
	fake.mu.Lock()
	cancels := append([]url.Values(nil), fake.cancels...)
	fake.mu.Unlock()
	if len(cancels) != 1 || cancels[0].Get("subscription") != "sub_1" || cancels[0].Get("cancel_at") != strconv.FormatInt(from.Unix(), 10) {
		t.Fatalf("the subscription does not end before its first charge: %v", cancels)
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
	if _, err := migrationDB(t).Exec(t.Context(), `UPDATE portal_orders SET placed_at=placed_at-interval '15 days'`); err != nil {
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

func TestWithdrawalDeadlineCountsFromTheNextDay(t *testing.T) {
	for placed, last := range map[string]string{
		"2026-11-15T10:00:00Z": "2026-11-29",
		"2026-11-15T23:30:00Z": "2026-11-30", // already 16 November in Copenhagen
		"2026-03-20T12:00:00Z": "2026-04-03", // across the switch to summer time
	} {
		at, _ := time.Parse(time.RFC3339, placed)
		o := projectOrder{Placed: at}
		if o.WithdrawUntil() != last {
			t.Errorf("an order placed %s may be withdrawn until %s, want %s", placed, o.WithdrawUntil(), last)
		}
		if deadline := withdrawalDeadline(at); deadline.In(copenhagen).Hour() != 0 || deadline.In(copenhagen).Format("2006-01-02") <= last {
			t.Errorf("the period for %s ends at %v, not at midnight after %s", placed, deadline, last)
		}
	}
}
