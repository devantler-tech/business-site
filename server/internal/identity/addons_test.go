package identity

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestAddonChoicesAreValidated(t *testing.T) {
	got, ok := parseAddons([]string{"statistics:1", "language:2", "contact-form:1"}, "app", true)
	want := []orderAddon{{"statistics", "monthly", 1, 7500, nil}, {"language", "one-time", 2, 150000, nil}, {"contact-form", "one-time", 1, 150000, nil}}
	if !ok || !slices.Equal(got, want) {
		t.Fatalf("valid add-ons were not read in catalogue order: %v %v", got, ok)
	}
	for name, values := range map[string][]string{
		"an unknown add-on":                 {"logo-kit:1"},
		"another offering's add-on":         {"news:1"},
		"a repeated add-on":                 {"seo:1", "seo:1"},
		"more units than the add-on allows": {"language:6"},
		"units on a single add-on":          {"seo:2"},
		"no units":                          {"seo:0"},
		"a malformed value":                 {"seo"},
		"two priority tiers":                {"priority:1", "express:1"},
		"a monthly add-on without hosting":  {"statistics:1"},
		"a priority tier without hosting":   {"express:1"},
	} {
		hosting := name != "a monthly add-on without hosting" && name != "a priority tier without hosting"
		if _, ok := parseAddons(values, "app", hosting); ok {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, ok := parseAddons([]string{"news:1", "migration:1"}, "website", false); !ok {
		t.Fatal("one-time website add-ons need no hosting")
	}
	for bad, replacement := range map[string]string{`"kind": "monthly"`: `"kind": "weekly"`, `"tier": "priority"`: `"tier": "gold"`} {
		raw := strings.Replace(string(offeringsJSON), bad, replacement, 1)
		if raw == string(offeringsJSON) {
			t.Fatalf("the mutation %s found nothing to change", bad)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a catalogue with an invalid add-on (%s) loaded", bad)
				}
			}()
			mustOfferings([]byte(raw))
		}()
	}
}

// TestAddonsOrderAndInvoice orders with add-ons the client chose on the
// receipt: the summary, the order, its confirmation and the Numina invoice
// carry one line per add-on, and the prices add up.
func TestAddonsOrderAndInvoice(t *testing.T) {
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	query := url.Values{"lang": {"en"}, "addon": {"contact-form", "language", "statistics"}, "units-language": {"2"}}
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?"+query.Encode(), nil, "")
	_, summary, _ := strings.Cut(body, `id="order-boundary"`)
	button := strings.Index(summary, ">Order with obligation to pay</button>")
	if status != 200 || button < 0 || !strings.Contains(summary[:button], "<dt>Extra language × 2</dt><dd>3000 DKK</dd>") ||
		!strings.Contains(summary[:button], "<dt>Total</dt><dd>7495 DKK once and 174 DKK a month</dd>") {
		t.Fatal("the order summary does not show the chosen add-ons and the total before the button")
	}
	f, _ := orderFormFields(t, b, "en")
	f.Set("collection", "invoice")
	f["addon"] = regexp.MustCompile(`name="addon" value="([a-z-]+:[0-9]+)"`).FindAllString(summary, -1)
	for i, v := range f["addon"] {
		f["addon"][i] = strings.TrimSuffix(strings.TrimPrefix(v, `name="addon" value="`), `"`)
	}
	if !slices.Equal(f["addon"], []string{"language:2", "contact-form:1", "statistics:1"}) && !slices.Equal(f["addon"], []string{"statistics:1", "language:2", "contact-form:1"}) {
		t.Fatalf("the order form does not carry the chosen add-ons: %v", f["addon"])
	}
	path := "/portal/requests/" + proposalRequestID + "/orders"
	wrong := url.Values{}
	for k, v := range f {
		wrong[k] = slices.Clone(v)
	}
	wrong["addon"] = append(wrong["addon"], "news:1")
	if status, _, _ := b.request("POST", path, wrong, b.site.URL); status != 409 {
		t.Fatalf("an add-on outside the offering was ordered: %d", status)
	}
	for range 2 {
		if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
			t.Fatalf("order/retry=%d", status)
		}
	}
	changed := url.Values{}
	for k, v := range f {
		changed[k] = slices.Clone(v)
	}
	changed["addon"] = []string{"seo:1"}
	if status, _, _ := b.request("POST", path, changed, b.site.URL); status != 409 {
		t.Fatalf("a retry with other add-ons was admitted: %d", status)
	}
	// A later catalogue price leaves the order and its retry as they were.
	saved := offerings.Addons
	t.Cleanup(func() { offerings.Addons = saved })
	offered := slices.Clone(saved)
	for i := range offered {
		if offered[i].ID == "contact-form" {
			offered[i].Price = 2000
		}
	}
	offered = slices.DeleteFunc(offered, func(a addonOffer) bool { return a.ID == "statistics" })
	offerings.Addons = offered
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatalf("a retry after a catalogue change was refused: %d", status)
	}
	offerings.Addons = saved
	var oneTime, monthly int64
	var addons int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT o.one_time_minor,o.monthly_minor,(SELECT count(*) FROM portal_order_addons a WHERE a.order_id=o.id) FROM portal_orders o`).Scan(&oneTime, &monthly, &addons); err != nil || oneTime != 749500 || monthly != 17400 || addons != 3 {
		t.Fatalf("order=%d/%d add-ons=%d %v", oneTime, monthly, addons, err)
	}
	var mail string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT body FROM portal_mail_outbox`).Scan(&mail); err != nil || !strings.Contains(mail, "One-time price: 2995 DKK") || !strings.Contains(mail, "Add-on: Extra language × 2: 3000 DKK once") ||
		!strings.Contains(mail, "Add-on: Visitor statistics: 75 DKK a month") || !strings.Contains(mail, "Total: 7495 DKK once and 174 DKK a month") {
		t.Fatalf("the order confirmation does not list the add-ons: %v", err)
	}
	for _, inv := range acceptanceInvoice(t, b, fake, cfg) {
		if len(inv.Items) != 3 || inv.Items[0].UnitPrice != "2396.00" || inv.Items[1].Description != "Ekstra sprog × 2" || inv.Items[1].Quantity != 2 || inv.Items[1].UnitPrice != "1200.00" ||
			inv.Items[2].Description != "Kontaktformular" || inv.Items[2].UnitPrice != "1200.00" {
			t.Fatalf("the invoice has no line per one-time add-on: %+v", inv.Items)
		}
	}
	var order string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT id FROM portal_orders`).Scan(&order); err != nil {
		t.Fatal(err)
	}
	month := billingCharge{Order: billingOrder{ID: order, Scope: "Fictional scope"}, Period: "2026-12", GrossMinor: 17400}
	items, err := b.svc.invoiceItems(t.Context(), cfg, month)
	if err != nil || len(items) != 2 || items[0].UnitPrice != "79.20" || items[1].Description != "Besøgsstatistik (månedligt)" || items[1].UnitPrice != "60.00" {
		t.Fatalf("the monthly invoice does not add the monthly add-on: %+v %v", items, err)
	}
	if _, receipt, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(receipt, "<dt>Add-on</dt><dd>Visitor statistics: 75 DKK a month, included above</dd>") {
		t.Fatal("the order receipt does not list the add-ons")
	}
}

// TestAddonsCardPaymentAndLaunchDiscount pays add-ons by card, each on its own
// Stripe line, with the launch discount on the project's own price only.
func TestAddonsCardPaymentAndLaunchDiscount(t *testing.T) {
	b, fake := cardOrderFixture(t, 600000, 30000)
	withLaunch(t, 50)
	f, body := launchOrderForm(t, b, "en", "card")
	if f.Get("launch") != "yes" || !strings.Contains(body, `name="addon" value="seo"`) {
		t.Fatal("the receipt offers no add-ons or no launch discount")
	}
	f["addon"] = []string{"seo:1", "email:1"}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("card order=%d", status)
	}
	var oneTime, monthly int64
	if err := b.pool.QueryRow(b.f.ctx, `SELECT one_time_minor,monthly_minor FROM portal_orders`).Scan(&oneTime, &monthly); err != nil || oneTime != 550000 || monthly != 40000 {
		t.Fatalf("order=%d/%d %v: the launch discount must take 1500 DKK off 6000 DKK only", oneTime, monthly, err)
	}
	for _, s := range fake.sessions() {
		switch s.Get("mode") {
		case "payment":
			if s.Get("line_items[0][price_data][unit_amount]") != "450000" || !strings.Contains(s.Get("line_items[0][price_data][product_data][name]"), "25% launch discount") ||
				s.Get("line_items[1][price_data][unit_amount]") != "100000" || s.Get("line_items[1][price_data][product_data][name]") != "Search engine setup" || s.Has("line_items[2][quantity]") {
				t.Fatalf("one-time lines: %v", s)
			}
		case "subscription":
			if s.Get("line_items[0][price_data][unit_amount]") != "30000" || s.Get("line_items[1][price_data][unit_amount]") != "10000" || s.Get("line_items[1][price_data][product_data][name]") != "Email sending (monthly)" {
				t.Fatalf("monthly lines: %v", s)
			}
		}
	}
}

// TestAddonInvalidChoiceIsKept keeps an impossible add-on choice ticked with
// its reason and offers no order until the client corrects it.
func TestAddonInvalidChoiceIsKept(t *testing.T) {
	b := billingFixture(t)
	query := url.Values{"lang": {"en"}, "addon": {"priority", "express", "seo"}}
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?"+query.Encode(), nil, "")
	if status != 200 || !strings.Contains(body, "That combination of add-ons is not possible.") || strings.Contains(body, ">Order with obligation to pay</button>") {
		t.Fatal("an impossible add-on choice was not explained, or still offered an order")
	}
	for _, id := range []string{"priority", "express", "seo"} {
		if !strings.Contains(body, `value="`+id+`" checked`) {
			t.Errorf("the choice of %s was cleared", id)
		}
	}
}
