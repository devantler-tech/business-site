package identity

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

const clientEndPath = "/portal/requests/" + proposalRequestID + "/order-end"

// receiptOrderForm reads the receipt in lang with the CSRF token and order
// that the client's own order forms post.
func receiptOrderForm(t *testing.T, b *browserFixture, lang string) (string, url.Values) {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang="+lang, nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	order := regexp.MustCompile(`name="order" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	if status != 200 || len(csrf) != 2 || len(order) != 2 {
		t.Fatalf("receipt has no client order form: %d", status)
	}
	return body, url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {lang}, "order": {order[1]}}
}

// clientEndForm reads the client's own end form from the receipt, under a
// button that names the action and the last month.
func clientEndForm(t *testing.T, b *browserFixture, lang string) url.Values {
	t.Helper()
	body, f := receiptOrderForm(t, b, lang)
	label := map[string]string{"da": "Opsig månedlig betaling", "en": "End monthly billing"}[lang]
	if !strings.Contains(body, ">"+label+"</button>") || !strings.Contains(body, " "+currentMonth(time.Now()).Format("2006-01")+",") {
		t.Fatal("receipt offers the client no end form")
	}
	return f
}

func clientEnd(t *testing.T, b *browserFixture) (source string, last time.Time, operator bool) {
	t.Helper()
	if err := b.pool.QueryRow(b.f.ctx, `SELECT source,last_month,operator_issuer IS NOT NULL OR client_subject IS NULL FROM portal_order_ends`).Scan(&source, &last, &operator); err != nil {
		t.Fatal("order end not recorded", err)
	}
	return source, last, operator
}

// TestInvoiceBillingClientEndsMonthlyBilling lets the client end an invoice
// order's monthly billing after the current month, in the portal where they
// ordered (Consumer Contracts Act, section 28 a). No monthly invoice follows.
func TestInvoiceBillingClientEndsMonthlyBilling(t *testing.T) {
	b, fake, cfg := bookedDanishOrder(t)
	f := clientEndForm(t, b, "da")
	for name, bad := range map[string]url.Values{
		"chosen month":   func() url.Values { g := url.Values{}; copyForm(g, f); g.Set("last_month", "2030-01"); return g }(),
		"no order":       func() url.Values { g := url.Values{}; copyForm(g, f); g.Del("order"); return g }(),
		"repeated order": func() url.Values { g := url.Values{}; copyForm(g, f); g.Add("order", f.Get("order")); return g }(),
	} {
		if status, _, _ := b.request("POST", clientEndPath, bad, b.site.URL); status != 400 {
			t.Fatalf("%s admitted: %d", name, status)
		}
	}
	other := url.Values{}
	copyForm(other, f)
	other.Set("order", strings.Repeat("A", 26))
	if status, _, _ := b.request("POST", clientEndPath, other, b.site.URL); status != 409 {
		t.Fatalf("another order's end admitted: %d", status)
	}
	for range 2 {
		if status, _, h := b.request("POST", clientEndPath, f, b.site.URL); status != 303 || h.Get("Location") != "/portal/requests/"+proposalRequestID+"?lang=da" {
			t.Fatalf("end or retry refused: %d", status)
		}
	}
	now := currentMonth(time.Now())
	if source, last, operator := clientEnd(t, b); source != "client" || !last.Equal(now) || operator {
		t.Fatalf("end recorded as %s after %v", source, last)
	}
	month := syncNextMonth(t, b, cfg)
	if states := chargeStates(t, b); len(states) != 1 || fake.count() != 1 {
		t.Fatalf("billing continued after the client's last month (%s): %v", month, states)
	}
	_, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=da", nil, "")
	if !strings.Contains(body, "Månedlig betaling slutter") || !strings.Contains(body, "efter "+now.Format("2006-01")) || strings.Contains(body, "Opsig månedlig betaling") {
		t.Fatal("the receipt does not show the client's end")
	}
}

// TestPrivateCardClientEndsTheSubscription ends a card order's subscription
// from the client's receipt when it would next renew. An operator cannot use
// the client's form, and a closed card gate offers and admits nothing.
func TestPrivateCardClientEndsTheSubscription(t *testing.T) {
	b, fake := subscribedCardOrder(t)
	f := clientEndForm(t, b, "en")
	cardFlags(t, b, false)
	if _, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, ""); strings.Contains(body, "End monthly billing") {
		t.Fatal("a closed card gate offers the end form")
	}
	if status, _, _ := b.request("POST", clientEndPath, f, b.site.URL); status != 403 {
		t.Fatalf("a closed card gate admitted the end: %d", status)
	}
	cardFlags(t, b, true)
	clientSession := b.client
	g, _ := operatorEndForm(t, b, "card-end-operator")
	operatorSession := b.client
	if status, _, _ := b.request("POST", clientEndPath, g, b.site.URL); status == 303 {
		t.Fatal("an operator ended the order through the client's form")
	}
	var ends int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_order_ends`).Scan(&ends); err != nil || ends != 0 {
		t.Fatal("a refused end was recorded", err)
	}
	b.client = clientSession
	// Stripe's first answer fails: the client's end still stands, and the
	// operator's page offers to ask Stripe again.
	fake.mu.Lock()
	fake.failCancel = true
	fake.mu.Unlock()
	if status, _, _ := b.request("POST", clientEndPath, clientEndForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatalf("client end refused: %d", status)
	}
	b.client = operatorSession
	if _, body, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(body, "Ask Stripe again") {
		t.Fatal("an unconfirmed client end is not offered again to the operator")
	}
	if status, _, _ := b.request("POST", orderEndPath, endForm(g, currentMonth(time.Now())), b.site.URL); status != 303 {
		t.Fatalf("asking Stripe again refused: %d", status)
	}
	b.client = clientSession
	fake.mu.Lock()
	cancels := append([]url.Values(nil), fake.cancels...)
	fake.mu.Unlock()
	renewal := currentMonth(time.Now()).AddDate(0, 1, 0)
	if len(cancels) != 2 || cancels[1].Get("subscription") != "sub_1" || cancels[1].Get("cancel_at") != strconv.FormatInt(renewal.Unix(), 10) || cancels[1].Get("proration_behavior") != "none" {
		t.Fatalf("subscription end request differs: %v", cancels)
	}
	if source, _, operator := clientEnd(t, b); source != "client" || operator {
		t.Fatalf("end recorded as %s", source)
	}
	if _, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(body, "Monthly billing ends") || strings.Contains(body, "End monthly billing") {
		t.Fatal("the receipt does not show the client's end")
	}
}

// copyForm copies a form so a case can change it without touching the original.
func copyForm(dst, src url.Values) {
	for k, v := range src {
		dst[k] = append([]string(nil), v...)
	}
}
