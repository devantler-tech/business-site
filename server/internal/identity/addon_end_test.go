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
	f["addon"] = []string{"express:1", "statistics:1"}
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
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key"); err != nil || level != "express" {
		t.Fatalf("Express ended before its last month: %q %v", level, err)
	}
	if _, err := migrationDB(t).Exec(b.f.ctx, `UPDATE portal_addon_ends SET last_month=last_month - interval '1 month'`); err != nil {
		t.Fatal(err)
	}
	if level, err := featureLevel(b.f.ctx, b.pool, "private-client-key"); err != nil || level != "standard" {
		t.Fatalf("the level after Express ended: %q %v", level, err)
	}
}

// TestAddonEndOnTheCardPath removes the add-on's subscription item before the
// end is recorded, and records nothing while Stripe fails.
func TestAddonEndOnTheCardPath(t *testing.T) {
	b, fake := cardOrderFixture(t, 600000, 30000)
	f, _ := launchOrderForm(t, b, "en", "card")
	f["addon"] = []string{"email:1"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("card order=%d", status)
	}
	var marked bool
	for _, s := range fake.sessions() {
		if s.Get("mode") == "subscription" && s.Get("line_items[1][price_data][product_data][metadata][portal_addon]") == "email" {
			marked = true
		}
	}
	if !marked {
		t.Fatal("the subscription's add-on line does not name its add-on")
	}
	if sendEvent(t, b, "evt_sub", "checkout.session.completed", map[string]any{"id": "cs_test_2", "mode": "subscription", "payment_status": "no_payment_required", "amount_total": 0, "currency": "dkk", "subscription": "sub_1"}, webhookSecret) != 200 {
		t.Fatal("subscription completion refused")
	}
	fake.mu.Lock()
	fake.items = map[string][]string{"sub_1": {"email"}}
	fake.failRemove = true
	fake.mu.Unlock()
	end, _ := addonEndForm(t, b, "email")
	path := "/portal/requests/" + proposalRequestID + "/addon-end"
	if status, _, _ := b.request("POST", path, end, b.site.URL); status != 503 {
		t.Fatalf("an end Stripe did not confirm=%d", status)
	}
	var ends int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_addon_ends`).Scan(&ends); err != nil || ends != 0 {
		t.Fatal("an end was recorded while Stripe still charges the add-on", err)
	}
	if status, _, _ := b.request("POST", path, end, b.site.URL); status != 303 {
		t.Fatalf("the retried end=%d", status)
	}
	fake.mu.Lock()
	removed := append([]string(nil), fake.removed...)
	fake.mu.Unlock()
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_addon_ends`).Scan(&ends); err != nil || ends != 1 || len(removed) != 1 || removed[0] != "si_email" {
		t.Fatalf("ends=%d removed=%v %v", ends, removed, err)
	}
}
