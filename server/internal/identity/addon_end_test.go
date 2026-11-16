package identity

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// addonEndForm reads the receipt's form that ends one add-on.
func addonEndForm(t *testing.T, b *browserFixture, addon string) (url.Values, string) {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(body)
	order := regexp.MustCompile(`name="order" value="([A-Z2-7]{26})"><input type="hidden" name="addon" value="` + addon + `"`).FindStringSubmatch(body)
	if status != 200 || len(csrf) != 2 || len(order) != 2 {
		t.Fatalf("the receipt offers no end of %s: %d", addon, status)
	}
	return url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "order": {order[1]}, "addon": {addon}}, body
}

// TestAddonEndOnTheInvoicePath ends one monthly add-on where the client
// ordered it: it is billed through this month, later months leave it out, and
// ending Express drops the client's level once its last month has passed.
func TestAddonEndOnTheInvoicePath(t *testing.T) {
	b := billingFixture(t)
	f := orderForm(t, b, "en")
	f["addon"] = []string{"express:1:1000", "statistics:1:75"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("order=%d", status)
	}
	this := currentMonth(time.Now()).Format("2006-01")
	end, body := addonEndForm(t, b, "statistics")
	if !strings.Contains(body, ">End this add-on after "+this+"</button>") {
		t.Fatal("the end form does not name the last billed month")
	}
	path := "/portal/requests/" + proposalRequestID + "/addon-end"
	for range 2 {
		if status, _, _ := b.request("POST", path, end, b.site.URL); status != 303 {
			t.Fatalf("add-on end/retry=%d", status)
		}
	}
	bad := url.Values{"csrf_token": end["csrf_token"], "lang": {"en"}, "order": end["order"], "addon": {"seo"}}
	if status, _, _ := b.request("POST", path, bad, b.site.URL); status != 409 {
		t.Fatalf("an add-on the order does not bill monthly was ended: %d", status)
	}
	_, receipt, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, "")
	if !strings.Contains(receipt, "Visitor statistics: 75 DKK a month, included above, ended after "+this) || strings.Contains(receipt, `name="addon" value="statistics"`) {
		t.Fatal("the receipt does not show the ended add-on, or still offers to end it")
	}
	var order string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT id FROM portal_orders`).Scan(&order); err != nil {
		t.Fatal(err)
	}
	addons, err := readOrderAddons(b.f.ctx, b.pool, order)
	if err != nil {
		t.Fatal(err)
	}
	next := currentMonth(time.Now()).AddDate(0, 1, 0).Format("2006-01")
	if this := periodMonthly(117400, addons, this); this != 117400 {
		t.Fatalf("this month charges %d, want the full 1174 DKK", this)
	}
	if later := periodMonthly(117400, addons, next); later != 109900 {
		t.Fatalf("next month charges %d, want 1174 DKK less 75 DKK", later)
	}
	_, cfg := newFakeNumina(t)
	items, err := b.svc.invoiceItems(t.Context(), cfg, billingCharge{Order: billingOrder{ID: order, Scope: "Fictional scope"}, Period: next, GrossMinor: 109900})
	if err != nil || len(items) != 2 || items[1].Description != "Ekspres-ønsker (månedligt)" {
		t.Fatalf("next month's invoice still lists the ended add-on: %+v %v", items, err)
	}
	// Ending Express drops the level after its last month.
	express, _ := addonEndForm(t, b, "express")
	if status, _, _ := b.request("POST", path, express, b.site.URL); status != 303 {
		t.Fatalf("Express end=%d", status)
	}
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key", time.Now()); err != nil || level != "express" {
		t.Fatalf("Express ended before its last month: %q %v", level, err)
	}
	if _, err := migrationDB(t).Exec(b.f.ctx, `UPDATE portal_addon_ends SET last_month=last_month - interval '1 month'`); err != nil {
		t.Fatal(err)
	}
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key", time.Now()); err != nil || level != "standard" {
		t.Fatalf("the level after Express ended: %q %v", level, err)
	}
}

// TestAddonEndOnTheCardPath removes the add-on's subscription item before the
// end is recorded, and records nothing while Stripe fails.
func TestAddonEndOnTheCardPath(t *testing.T) {
	b, fake := emailCardOrder(t)
	var marked bool
	for _, s := range fake.sessions() {
		if s.Get("mode") == "subscription" && s.Get("line_items[0][price_data][product_data][metadata][portal_line]") == "order" && s.Get("line_items[1][price_data][product_data][metadata][portal_addon]") == "email" {
			marked = true
		}
	}
	if !marked {
		t.Fatal("the subscription's lines do not name what they charge")
	}
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	fake.mu.Lock()
	fake.items = map[string][]string{"sub_1": {"email"}}
	fake.mu.Unlock()
	end, _ := addonEndForm(t, b, "email")
	path := "/portal/requests/" + proposalRequestID + "/addon-end"
	// A closed card gate never changes the subscription.
	setFixturePortalFlags(t, b, append(acceptanceFlagNames(), "portal-invoice-billing", "portal-card-billing"), []any{true, true, true, true, true, true, true, false})
	status, _, _ := b.request("POST", path, end, b.site.URL)
	fake.mu.Lock()
	untouched := len(fake.removed) == 0
	fake.mu.Unlock()
	if status != 403 || !untouched {
		t.Fatalf("an add-on was ended with the card gate closed: %d", status)
	}
	cardFlags(t, b, true)
	// An item that names nothing may be the add-on: nothing is removed, and
	// nothing is confirmed. Then Stripe fails on the client's retry. The
	// client's end still stands, and the operator's page offers to ask Stripe
	// again until it confirms.
	fake.mu.Lock()
	fake.items = map[string][]string{"sub_1": {"email", "untagged"}}
	fake.mu.Unlock()
	if status, _, _ := b.request("POST", path, end, b.site.URL); status != 303 {
		t.Fatalf("an end beside an untagged item=%d", status)
	}
	fake.mu.Lock()
	fake.items = map[string][]string{"sub_1": {"email"}}
	fake.failRemove = true
	untouched = len(fake.removed) == 0
	fake.mu.Unlock()
	if status, _, _ := b.request("POST", path, end, b.site.URL); status != 303 || !untouched {
		t.Fatalf("an end Stripe did not confirm=%d, removed beside an untagged item: %v", status, !untouched)
	}
	var ends, removals int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT (SELECT count(*) FROM portal_addon_ends),(SELECT count(*) FROM portal_addon_removals)`).Scan(&ends, &removals); err != nil || ends != 1 || removals != 0 {
		t.Fatalf("ends=%d removals=%d %v", ends, removals, err)
	}
	client := b.client
	triageSignIn(t, b, "addon-end-operator", "operator-workspace", "operator")
	_, page, _ := b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, "")
	csrf := csrfInput.FindStringSubmatch(page)
	if len(csrf) != 2 || !strings.Contains(page, ">Ask Stripe again</button>") {
		t.Fatal("the operator is not offered to ask Stripe again")
	}
	retry := url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {"en"}, "order": end["order"]}
	if status, _, _ := b.request("POST", "/portal/operator/requests/"+proposalRequestID+"/addon-removals", retry, b.site.URL); status != 303 {
		t.Fatalf("asking Stripe again=%d", status)
	}
	fake.mu.Lock()
	removed := append([]string(nil), fake.removed...)
	fake.mu.Unlock()
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_addon_removals`).Scan(&removals); err != nil || removals != 1 || len(removed) != 1 || removed[0] != "si_email" {
		t.Fatalf("removals=%d removed=%v %v", removals, removed, err)
	}
	if _, page, _ = b.request("GET", "/portal/operator/requests/"+proposalRequestID+"?lang=en", nil, ""); strings.Contains(page, "Ask Stripe again") {
		t.Fatal("a confirmed removal is still offered again")
	}
	b.client = client
}

// TestAddonEndBeforeTheSubscription ends an add-on while the monthly Checkout
// is still open: the subscription it then creates loses the add-on before the
// event counts as processed, and Stripe's retry converges after a failure.
func TestAddonEndBeforeTheSubscription(t *testing.T) {
	b, fake := emailCardOrder(t)
	end, _ := addonEndForm(t, b, "email")
	path := "/portal/requests/" + proposalRequestID + "/addon-end"
	if status, _, _ := b.request("POST", path, end, b.site.URL); status != 303 {
		t.Fatalf("add-on end before the subscription=%d", status)
	}
	fake.mu.Lock()
	fake.items = map[string][]string{"sub_1": {"email"}}
	fake.failRemove = true
	fake.mu.Unlock()
	completed := map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}
	if status := sendEvent(t, b, "evt_sub", "checkout.session.completed", completed, webhookSecret); status == 200 {
		t.Fatal("a completion whose ended add-on Stripe still charges was accepted")
	}
	if status := sendEvent(t, b, "evt_sub", "checkout.session.completed", completed, webhookSecret); status != 200 {
		t.Fatalf("Stripe's retry of the completion=%d", status)
	}
	fake.mu.Lock()
	removed, left := append([]string(nil), fake.removed...), len(fake.items["sub_1"])
	fake.mu.Unlock()
	if len(removed) != 1 || removed[0] != "si_email" || left != 0 {
		t.Fatalf("the new subscription kept the ended add-on: removed=%v left=%d", removed, left)
	}
}

// TestAddonEndUsesTheBilledMonth ends a card order's add-on after the UTC
// month Stripe bills, which differs from Copenhagen's for an hour or two on
// the 1st. The receipt offers that month, the end records it, and the
// client's level counts the add-on through it.
func TestAddonEndUsesTheBilledMonth(t *testing.T) {
	late := time.Date(2026, 11, 30, 23, 30, 0, 0, time.UTC) // 00:30 on 1 December in Copenhagen
	if card, invoice := addonLastMonth("card", late).Format("2006-01"), addonLastMonth("invoice", late).Format("2006-01"); card != "2026-11" || invoice != "2026-12" {
		t.Fatalf("card=%s invoice=%s", card, invoice)
	}
	b, _ := emailCardOrder(t)
	db := migrationDB(t)
	if _, err := db.Exec(b.f.ctx, `INSERT INTO portal_order_addons(order_id,addon_id,kind,quantity,unit_minor) SELECT id,'express','monthly',1,100000 FROM portal_orders`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(b.f.ctx, `INSERT INTO portal_addon_ends(order_id,addon_id,last_month,client_issuer,client_subject) SELECT id,'express','2026-11-01',client_issuer,client_subject FROM portal_orders`); err != nil {
		t.Fatal(err)
	}
	for now, want := range map[time.Time]string{late: "express", late.Add(time.Hour): "standard"} {
		if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key", now); err != nil || level != want {
			t.Fatalf("level at %s=%q %v, want %s", now.Format(time.RFC3339), level, err, want)
		}
	}
}

// emailCardOrder places a card order with email sending as a monthly add-on.
func emailCardOrder(t *testing.T) (*browserFixture, *fakeStripe) {
	t.Helper()
	b, fake := cardOrderFixture(t, 600000, 30000)
	f, _ := launchOrderForm(t, b, "en", "card")
	f["addon"] = []string{"email:1:100"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("card order=%d", status)
	}
	return b, fake
}
