package identity

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The published launch offer, before init turns it off for the package.
var publishedLaunch = offerings.LaunchDiscount

// Tests of other features expect full prices, so the package's tests run
// without the launch offer; the launch tests below turn it on.
func init() { offerings.LaunchDiscount = nil }

func withLaunch(t *testing.T, limit int) {
	t.Helper()
	offer := *publishedLaunch
	offer.Limit = int64(limit)
	offerings.LaunchDiscount = &offer
	t.Cleanup(func() { offerings.LaunchDiscount = nil })
}

// fillLaunchPlaces records places taken by earlier, already purged customers.
func fillLaunchPlaces(t *testing.T, b *browserFixture, from, to int) {
	t.Helper()
	if _, err := migrationDB(t).Exec(t.Context(), `INSERT INTO portal_launch_places(place,percent,list_minor,discount_minor) SELECT n,25,600000,150000 FROM generate_series($1::int,$2::int) n`, from, to); err != nil {
		t.Fatal(err)
	}
}

// launchOrderForm reads the receipt's order form, including whether it
// carries the launch discount the page shows.
func launchOrderForm(t *testing.T, b *browserFixture, lang, collection string) (url.Values, string) {
	t.Helper()
	f, body := orderFormFields(t, b, lang)
	f.Set("collection", collection)
	if strings.Contains(body, `name="launch" value="yes"`) {
		f.Set("launch", "yes")
	}
	return f, body
}

func TestLaunchDiscountArithmetic(t *testing.T) {
	if publishedLaunch == nil || publishedLaunch.Percent != 25 || publishedLaunch.Limit != 50 || publishedLaunch.AppliesTo != "setup" || !publishedLaunch.RequiresHosting {
		t.Fatalf("the published launch offer differs: %+v", publishedLaunch)
	}
	withLaunch(t, 50)
	for _, c := range []struct{ oneTime, monthly, want int64 }{
		{600000, 30000, 150000}, {950000, 50000, 237500}, {1500000, 80000, 375000},
		{299500, 9900, 74900}, // 748.75 DKK rounds to whole kroner
		{600000, 0, 0},        // no hosting
		{0, 30000, 0},         // no one-time price
		{100, 30000, 0},       // nothing to take off
	} {
		if got := launchDiscount(c.oneTime, c.monthly); got != c.want {
			t.Errorf("launchDiscount(%d, %d) = %d, want %d", c.oneTime, c.monthly, got, c.want)
		}
	}
	l := &launchPlace{Percent: 25, ListMinor: 600000, DiscountMinor: 150000}
	if got := launchLine("Website", "da", l); got != "Website — normalpris 6000 DKK, lanceringsrabat 25 % −1500 DKK" {
		t.Fatal(got)
	}
	if got := launchLine(strings.Repeat("x", 200), "en", l); utf8.RuneCountInString(got) != 200 || !strings.HasSuffix(got, " — list price 6000 DKK, 25% launch discount −1500 DKK") {
		t.Fatal("a long line lost its discount or grew past 200 characters:", got)
	}
	if launchLine("Website", "en", nil) != "Website" {
		t.Fatal("a line without a discount changed")
	}
	for _, bad := range []string{`"appliesTo": "monthly"`, `"requiresHosting": false`, `"percent": 100`, `"limit": 0`} {
		key := bad[:strings.Index(bad, ":")]
		raw := regexp.MustCompile(key+`: [^,\n]+`).ReplaceAllString(string(offeringsJSON), bad)
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("a catalogue with %s loaded", bad)
				}
			}()
			mustOfferings([]byte(raw))
		}()
	}
}

// TestLaunchDiscountOrder orders with the 50th place: the order, its
// confirmation, the operator's page and the Numina invoice carry the
// discount, and the client cannot take a second place.
func TestLaunchDiscountOrder(t *testing.T) {
	b := billingFixture(t)
	withLaunch(t, 50)
	fillLaunchPlaces(t, b, 1, 49)
	fake, cfg := newFakeNumina(t)
	f, body := launchOrderForm(t, b, "da", "invoice")
	_, summary, _ := strings.Cut(body, `id="order-boundary"`)
	button := strings.Index(summary, ">Bestil med betalingspligt</button>")
	discount := strings.Index(summary, "Lanceringsrabat</dt><dd>−749 DKK (25 %)")
	if f.Get("launch") != "yes" || discount < 0 || button < discount || !strings.Contains(summary[:button], "2246 DKK") {
		t.Fatal("the order form does not show the launch discount before the button")
	}
	path := "/portal/requests/" + proposalRequestID + "/orders"
	for range 2 {
		if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
			t.Fatalf("order/retry=%d", status)
		}
	}
	var oneTime int64
	var place int
	var held bool
	if err := b.pool.QueryRow(b.f.ctx, `SELECT o.one_time_minor,l.place,l.client_id=o.client_id AND l.list_minor=299500 AND l.discount_minor=74900 AND l.percent=25 FROM portal_orders o JOIN portal_launch_places l ON l.order_id=o.id`).Scan(&oneTime, &place, &held); err != nil || oneTime != 224600 || place != 50 || !held {
		t.Fatalf("order=%d place=%d recorded=%v %v", oneTime, place, held, err)
	}
	var client string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT client_id FROM portal_orders`).Scan(&client); err != nil {
		t.Fatal(err)
	}
	// With places left, the customer still cannot take a second one.
	withLaunch(t, 60)
	if again, err := openLaunchPlace(b.f.ctx, b.pool, client, 299500, 9900); err != nil || again != nil {
		t.Fatal("a customer with a place could take a second one", again, err)
	}
	if other, err := openLaunchPlace(b.f.ctx, b.pool, "another-client", 299500, 9900); err != nil || other == nil || other.Place != 51 {
		t.Fatal("another customer could not take the next place", other, err)
	}
	withLaunch(t, 50)
	var mail string
	if err := b.pool.QueryRow(b.f.ctx, `SELECT body FROM portal_mail_outbox`).Scan(&mail); err != nil || !strings.Contains(mail, "Engangspris: 2246 DKK (normalpris 2995 DKK, lanceringsrabat 25 % −749 DKK)") {
		t.Fatal("the order confirmation does not name the launch discount", err)
	}
	if _, receipt, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en", nil, ""); !strings.Contains(receipt, "including the 25% launch discount: −749 DKK off 2995 DKK") {
		t.Fatal("the order receipt does not name the launch discount")
	}
	// The first run creates the contact; the operator then confirms Denmark.
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	fake.confirmDenmark()
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 1 {
		t.Fatal("acceptance invoice not created", err)
	}
	for _, inv := range fake.invoices {
		if len(inv.Items) != 1 || inv.Items[0].UnitPrice != "1796.80" || !strings.HasSuffix(inv.Items[0].Description, " — normalpris 2995 DKK, lanceringsrabat 25 % −749 DKK") {
			t.Fatalf("the invoice line does not carry the discounted price and name the discount: %+v", inv.Items)
		}
	}
}

// TestLaunchDiscountEndsAtTheLimit gives the 51st customer the full price.
func TestLaunchDiscountEndsAtTheLimit(t *testing.T) {
	b := billingFixture(t)
	withLaunch(t, 50)
	fillLaunchPlaces(t, b, 1, 50)
	f, body := launchOrderForm(t, b, "en", "invoice")
	if f.Has("launch") || strings.Contains(body, "Launch discount") {
		t.Fatal("the 51st customer was offered the launch discount")
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("order=%d", status)
	}
	var oneTime int64
	var places int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT (SELECT one_time_minor FROM portal_orders),(SELECT count(*) FROM portal_launch_places)`).Scan(&oneTime, &places); err != nil || oneTime != 299500 || places != 50 {
		t.Fatalf("order=%d places=%d %v", oneTime, places, err)
	}
}

// TestLaunchDiscountGoneBeforeTheOrder refuses an order whose last place went
// after the client saw the discount, shows the new price, and then orders at it.
func TestLaunchDiscountGoneBeforeTheOrder(t *testing.T) {
	b := billingFixture(t)
	withLaunch(t, 50)
	fillLaunchPlaces(t, b, 1, 49)
	f, _ := launchOrderForm(t, b, "en", "invoice")
	fillLaunchPlaces(t, b, 50, 50)
	path := "/portal/requests/" + proposalRequestID + "/orders"
	if status, _, h := b.request("POST", path, f, b.site.URL); status != 303 || h.Get("Location") != "/portal/requests/"+proposalRequestID+"?lang=en&launch=ended" {
		t.Fatalf("an order with a discount that ended was not sent back: %d %s", status, h.Get("Location"))
	}
	var orders int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_orders`).Scan(&orders); err != nil || orders != 0 {
		t.Fatal("an order was placed at a price the client did not see", err)
	}
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=en&launch=ended", nil, "")
	if status != 200 || !strings.Contains(body, "The last launch discount place was taken before you ordered.") || strings.Contains(body, `name="launch"`) {
		t.Fatal("the receipt does not show the new price with its reason")
	}
	f.Del("launch")
	if status, _, _ := b.request("POST", path, f, b.site.URL); status != 303 {
		t.Fatalf("order at the new price=%d", status)
	}
	var oneTime int64
	if err := b.pool.QueryRow(b.f.ctx, `SELECT one_time_minor FROM portal_orders`).Scan(&oneTime); err != nil || oneTime != 299500 {
		t.Fatal("the order is not at the full price", oneTime, err)
	}
}

// TestLaunchPlacesAreTakenOneAtATime makes a second order wait for the first
// to take the last place, then find none left.
func TestLaunchPlacesAreTakenOneAtATime(t *testing.T) {
	b := billingFixture(t)
	withLaunch(t, 50)
	fillLaunchPlaces(t, b, 1, 49)
	ctx := b.f.ctx
	first, err := b.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	if p, err := lockLaunchPlace(ctx, first, "launch-first", 600000, 30000); err != nil || p == nil || p.Place != 50 {
		t.Fatal("the first order found no place", p, err)
	}
	type result struct {
		place *launchPlace
		err   error
	}
	done := make(chan result, 1)
	go func() {
		second, err := b.pool.Begin(ctx)
		if err != nil {
			done <- result{nil, err}
			return
		}
		defer func() { _ = second.Rollback(ctx) }()
		p, err := lockLaunchPlace(ctx, second, "launch-second", 600000, 30000)
		done <- result{p, err}
	}()
	for waiting := 0; waiting == 0; {
		select {
		case r := <-done:
			t.Fatalf("the second order did not wait for the first: %+v", r)
		case <-time.After(20 * time.Millisecond):
		}
		if err = b.pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = first.Exec(ctx, `INSERT INTO portal_launch_places(place,percent,list_minor,discount_minor) VALUES(50,25,600000,150000)`); err != nil {
		t.Fatal(err)
	}
	if err = first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != nil || r.place != nil {
		t.Fatalf("the second order took a place after the last: %+v", r)
	}
}

// TestLaunchDiscountCardPayment names the discount on the Stripe line and
// charges the discounted one-time price; hosting keeps its full price.
func TestLaunchDiscountCardPayment(t *testing.T) {
	b, fake := cardOrderFixture(t, 600000, 30000)
	withLaunch(t, 50)
	f, _ := launchOrderForm(t, b, "en", "card")
	if f.Get("launch") != "yes" {
		t.Fatal("the card order form does not carry the launch discount")
	}
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatalf("card order=%d", status)
	}
	sessions := fake.sessions()
	if len(sessions) != 2 {
		t.Fatalf("checkout sessions=%d, want one-off and subscription", len(sessions))
	}
	for _, s := range sessions {
		switch s.Get("mode") {
		case "payment":
			if s.Get("line_items[0][price_data][unit_amount]") != "450000" || !strings.HasSuffix(s.Get("line_items[0][price_data][product_data][name]"), " — list price 6000 DKK, 25% launch discount −1500 DKK") {
				t.Fatalf("one-time line: %v", s)
			}
		case "subscription":
			if s.Get("line_items[0][price_data][unit_amount]") != "30000" || strings.Contains(s.Get("line_items[0][price_data][product_data][name]"), "discount") {
				t.Fatalf("hosting was discounted: %v", s)
			}
		}
	}
}
