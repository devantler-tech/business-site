package identity

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/numina"
)

// fakeNumina is an in-memory Numina company with the API's retry semantics:
// invoice creation and sending honour Idempotency-Key, contacts do not.
type fakeNumina struct {
	mu              sync.Mutex
	contacts        map[string]numina.Contact
	invoices        map[string]*fakeInvoice
	keys            map[string]string
	sends           int
	contactCreates  int
	failCreateReply bool // store the invoice, then answer 500 as if the reply was lost
	forbidSend      bool // a sandbox company cannot send email
	next            int
}

type fakeInvoice struct {
	numina.Invoice
	Booked bool
	Items  []numina.Line
	VAT    string
	Due    string
}

func newFakeNumina(t *testing.T) (*fakeNumina, InvoiceBilling) {
	t.Helper()
	f := &fakeNumina{contacts: map[string]numina.Contact{}, invoices: map[string]*fakeInvoice{}, keys: map[string]string{}}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	client, err := numina.New(server.URL, "fixture-token", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return f, InvoiceBilling{Numina: client, SalesAccount: 1000, VATCode: "U25", Now: time.Now}
}

func (f *fakeNumina) id(prefix string) string {
	f.next++
	return prefix + "-" + string(rune('a'+f.next))
}

func (f *fakeNumina) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer fixture-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reply := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/contacts":
		var c numina.Contact
		_ = json.NewDecoder(r.Body).Decode(&c)
		c.ID = f.id("contact")
		f.contacts[c.ID] = c
		f.contactCreates++
		reply(201, c)
	case r.Method == "GET" && r.URL.Path == "/v1/contacts":
		var found []numina.Contact
		for _, c := range f.contacts {
			if strings.Contains(c.Name, r.URL.Query().Get("search")) {
				found = append(found, c)
			}
		}
		reply(200, map[string]any{"contacts": found, "has_more": false, "next_offset": 0})
	case len(parts) == 3 && parts[1] == "contacts":
		c, ok := f.contacts[parts[2]]
		if !ok {
			reply(404, map[string]string{"error": "not_found"})
			return
		}
		if r.Method == "PUT" {
			var update map[string]string
			_ = json.NewDecoder(r.Body).Decode(&update)
			c.Name, c.Email = update["name"], update["email"]
			f.contacts[c.ID] = c
		}
		reply(200, c)
	case r.Method == "POST" && r.URL.Path == "/v1/invoices":
		key := r.Header.Get("Idempotency-Key")
		if id, ok := f.keys[key]; ok {
			reply(201, f.invoices[id].Invoice)
			return
		}
		var in numina.NewInvoice
		_ = json.NewDecoder(r.Body).Decode(&in)
		inv := &fakeInvoice{Invoice: numina.Invoice{ID: f.id("invoice"), Status: "draft", ContactID: in.ContactID, IssueDate: in.IssueDate, Memo: in.Memo}, Items: in.Items, VAT: in.VATCode, Due: in.DueDate}
		f.invoices[inv.ID] = inv
		if f.failCreateReply {
			// The invoice exists, but the caller never learns its ID, and
			// a failed request frees its key, exactly as Numina documents.
			f.failCreateReply = false
			reply(500, map[string]string{"error": "server_error"})
			return
		}
		f.keys[key] = inv.ID
		reply(201, inv.Invoice)
	case r.Method == "GET" && r.URL.Path == "/v1/invoices":
		var list []numina.Invoice
		for _, inv := range f.invoices {
			if inv.ContactID == r.URL.Query().Get("contact_id") {
				list = append(list, inv.Invoice)
			}
		}
		reply(200, map[string]any{"invoices": list, "has_more": false, "next_offset": 0})
	case len(parts) >= 3 && parts[1] == "invoices":
		inv, ok := f.invoices[parts[2]]
		if !ok {
			reply(404, map[string]string{"error": "not_found"})
			return
		}
		switch {
		case r.Method == "POST" && len(parts) == 4 && parts[3] == "book":
			if !inv.Booked {
				inv.Booked, inv.Status = true, "sent"
				number := len(f.invoices)
				inv.InvoiceNumber = &number
				open := "open"
				inv.PaymentState = &open
				owed := numina.Amount(0)
				inv.OutstandingBalance = &owed
			}
			reply(200, inv.Invoice)
		case r.Method == "POST" && len(parts) == 4 && parts[3] == "send":
			if f.forbidSend {
				reply(403, map[string]string{"error": "forbidden"})
				return
			}
			f.sends++
			reply(200, inv.Invoice)
		case r.Method == "GET" && len(parts) == 3:
			reply(200, inv.Invoice)
		default:
			reply(404, map[string]string{"error": "not_found"})
		}
	default:
		reply(404, map[string]string{"error": "not_found"})
	}
}

// pay sets the invoice's Numina payment state and outstanding balance.
func (f *fakeNumina) pay(state, outstanding string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inv := range f.invoices {
		s, o := state, json.Number(outstanding)
		inv.PaymentState, inv.OutstandingBalance = &s, &o
	}
}

func (f *fakeNumina) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.invoices)
}

func billingFlags(t *testing.T, b *browserFixture, billing any) {
	t.Helper()
	setFixturePortalFlags(t, b, append(acceptanceFlagNames(), "portal-invoice-billing"), []any{true, true, true, true, true, true, billing})
}

// billingFixture accepts the shared fictional proposal (2995 DKK + 99 DKK/month)
// as the signed-in client, with invoice billing switched on.
func billingFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := acceptanceFixture(t)
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/proposal-acceptance", acceptanceForm(t, b), b.site.URL); status != 303 {
		t.Fatalf("acceptance=%d", status)
	}
	billingFlags(t, b, true)
	if _, err := b.pool.Exec(b.f.ctx, `INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES('private-client-key','Fictional Client','client@example.invalid','Fictional Bakery ApS',1)`); err != nil {
		t.Fatal(err)
	}
	return b
}

// orderForm reads the receipt's own order form.
func orderForm(t *testing.T, b *browserFixture, lang string) url.Values {
	t.Helper()
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang="+lang, nil, "")
	acceptance := regexp.MustCompile(`name="acceptance" value="([A-Z2-7]{26})"`).FindStringSubmatch(body)
	csrf := csrfInput.FindStringSubmatch(body)
	if status != 200 || len(acceptance) != 2 || len(csrf) != 2 || !strings.Contains(body, `value="invoice" checked`) {
		t.Fatalf("receipt has no order form: %d", status)
	}
	return url.Values{"csrf_token": {html.UnescapeString(csrf[1])}, "lang": {lang}, "acceptance": {acceptance[1]}, "collection": {"invoice"}}
}

// placeInvoiceOrder submits the receipt's own order form for the invoice path.
func placeInvoiceOrder(t *testing.T, b *browserFixture) {
	t.Helper()
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", orderForm(t, b, "en"), b.site.URL); status != 303 {
		t.Fatal("order refused")
	}
}

func chargeStates(t *testing.T, b *browserFixture) map[string]string {
	t.Helper()
	rows, err := b.pool.Query(b.f.ctx, `SELECT period,state FROM portal_order_charges`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := map[string]string{}
	for rows.Next() {
		var period, state string
		if err := rows.Scan(&period, &state); err != nil {
			t.Fatal(err)
		}
		states[period] = state
	}
	return states
}

// TestInvoiceBillingOrderToSettlement follows one order from the client's
// choice to a settled Numina invoice, including the next month's invoice.
func TestInvoiceBillingOrderToSettlement(t *testing.T) {
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	f := orderForm(t, b, "da")
	path := "/portal/requests/" + proposalRequestID + "/orders"
	for range 2 {
		if status, _, h := b.request("POST", path, f, b.site.URL); status != 303 || h.Get("Location") != "/portal/requests/"+proposalRequestID+"?lang=da" {
			t.Fatalf("order/retry=%d", status)
		}
	}
	card := url.Values{}
	for k, v := range f {
		card[k] = v
	}
	card.Set("collection", "card")
	if status, _, _ := b.request("POST", path, card, b.site.URL); status != 403 {
		t.Fatalf("second collection path admitted: %d", status)
	}
	var orders int
	if err := b.pool.QueryRow(b.f.ctx, `SELECT count(*) FROM portal_orders WHERE collection='invoice' AND one_time_minor=299500 AND monthly_minor=9900`).Scan(&orders); err != nil || orders != 1 {
		t.Fatal("order not recorded exactly once with the accepted amounts", err)
	}
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 || fake.sends != 1 || fake.contactCreates != 1 || chargeStates(t, b)["acceptance"] != "booked" {
		t.Fatalf("acceptance invoice not created, booked and sent once: %d %d %v", fake.count(), fake.sends, chargeStates(t, b))
	}
	for _, inv := range fake.invoices {
		if !inv.Booked || inv.VAT != "U25" || len(inv.Items) != 1 || inv.Items[0].UnitPrice != "2396.00" || !strings.Contains(inv.Memo, "portal-") || !strings.HasSuffix(inv.Memo, "-acceptance") {
			t.Fatalf("invoice content differs: %+v", inv)
		}
	}
	for _, c := range fake.contacts {
		if c.Name != "Fictional Bakery ApS" || c.Email != "client@example.invalid" || c.AttnPerson != "Fictional Client" || c.CountryCode != "DK" {
			t.Fatalf("contact differs: %+v", c)
		}
	}
	// Repeating the run changes nothing at Numina.
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 1 || fake.sends != 1 || fake.contactCreates != 1 {
		t.Fatal("repeated sync duplicated work", err)
	}
	// An unmatched payment is not settlement; a matched one is.
	fake.pay("covered_by_unmatched_payments", "2995.00")
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "booked" {
		t.Fatal("unmatched payment settled the order", err)
	}
	fake.pay("paid", "0")
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || chargeStates(t, b)["acceptance"] != "settled" {
		t.Fatal("paid invoice not settled", err)
	}
	// Next month, the monthly invoice is created in advance.
	cfg.Now = func() time.Time { return firstOfNextMonth(time.Now()).Add(36 * time.Hour) }
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	month := firstOfNextMonth(time.Now()).Format("2006-01")
	if states := chargeStates(t, b); len(states) != 2 || states[month] != "booked" || fake.count() != 2 {
		t.Fatalf("monthly invoice not created for %s: %v", month, states)
	}
	status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID+"?lang=da", nil, "")
	if status != 200 || !strings.Contains(body, "Accepteret tilbud: 2995 DKK — betalt") || !strings.Contains(body, month+": 99 DKK — faktureret, afventer betaling") || strings.Contains(body, `name="collection"`) {
		t.Fatal("client receipt does not show the order's invoices")
	}
	triageSignIn(t, b, "billing-operator", "operator-workspace", "operator")
	status, body, _ = b.request("GET", "/portal/operator/requests/"+proposalRequestID, nil, "")
	if status != 200 || !strings.Contains(body, `data-charge-state="settled"`) || !strings.Contains(body, "Order and billing") {
		t.Fatal("operator detail does not show the order")
	}
}

// TestInvoiceBillingRecoversALostCreateReply proves that an invoice created
// at Numina whose reply was lost is found by its memo, not created twice.
func TestInvoiceBillingRecoversALostCreateReply(t *testing.T) {
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	placeInvoiceOrder(t, b)
	fake.failCreateReply = true
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err == nil || chargeStates(t, b)["acceptance"] != "pending" {
		t.Fatal("lost reply was not reported as a failed charge")
	}
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	if fake.count() != 1 || chargeStates(t, b)["acceptance"] != "booked" {
		t.Fatalf("retry duplicated the invoice: %d", fake.count())
	}
}

// TestInvoiceBillingHoldsWhatItMustNotDecide covers a customer outside
// Denmark, a sandbox company that cannot email, and a missing contact.
func TestInvoiceBillingHoldsWhatItMustNotDecide(t *testing.T) {
	b := billingFixture(t)
	fake, cfg := newFakeNumina(t)
	placeInvoiceOrder(t, b)
	fake.contacts["contact-x"] = numina.Contact{ID: "contact-x", Name: "Fictional Bakery ApS", Email: "client@example.invalid", CountryCode: "SE"}
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 0 || chargeStates(t, b)["acceptance"] != "held" {
		t.Fatalf("foreign customer was invoiced with Danish VAT: %v", err)
	}
	if fake.contactCreates != 0 {
		t.Fatal("existing Numina contact with the same email was duplicated")
	}
	fake.contacts["contact-x"] = numina.Contact{ID: "contact-x", Name: "Fictional Bakery ApS", Email: "client@example.invalid", CountryCode: "DK"}
	fake.forbidSend = true
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 1 || fake.sends != 0 || chargeStates(t, b)["acceptance"] != "booked" {
		t.Fatalf("held charge was not resumed, or a sandbox send failure blocked booking: %v", err)
	}
	if _, err := b.pool.Exec(b.f.ctx, `DELETE FROM portal_numina_contacts`); err == nil {
		t.Fatal("runtime can delete the contact mapping")
	}
}

// TestInvoiceBillingStaysClosedWhenTheGateIsOff covers the default state.
func TestInvoiceBillingStaysClosedWhenTheGateIsOff(t *testing.T) {
	b := billingFixture(t)
	f := orderForm(t, b, "en")
	for _, off := range []any{false, "true", nil} {
		billingFlags(t, b, off)
		status, body, _ := b.request("GET", "/portal/requests/"+proposalRequestID, nil, "")
		if status != 200 || strings.Contains(body, "portal-order") {
			t.Fatalf("gate %v showed the order form", off)
		}
		if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 404 {
			t.Fatalf("gate %v admitted an order: %d", off, status)
		}
	}
	billingFlags(t, b, true)
	if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", f, b.site.URL); status != 303 {
		t.Fatal("order refused with the gate on")
	}
	billingFlags(t, b, false)
	fake, cfg := newFakeNumina(t)
	if err := b.svc.SyncInvoiceBilling(t.Context(), cfg); err != nil || fake.count() != 0 || len(chargeStates(t, b)) != 0 {
		t.Fatal("sync ran with the gate off", err)
	}
	for _, field := range []string{"extra", "collection"} {
		bad := url.Values{}
		for k, v := range f {
			bad[k] = v
		}
		if field == "extra" {
			bad.Set("amount", "1")
		} else {
			bad.Add("collection", "card")
		}
		billingFlags(t, b, true)
		if status, _, _ := b.request("POST", "/portal/requests/"+proposalRequestID+"/orders", bad, b.site.URL); status != 400 {
			t.Fatalf("forged %s admitted: %d", field, status)
		}
	}
}

// TestInvoiceBillingMigrationKeepsHistoryAndPrivileges proves v16 replays after
// unchanged v1–v15 and that the runtime cannot rewrite orders or contacts.
func TestInvoiceBillingMigrationKeepsHistoryAndPrivileges(t *testing.T) {
	b := billingFixture(t)
	placeInvoiceOrder(t, b)
	for _, statement := range []string{`UPDATE portal_orders SET collection='card'`, `DELETE FROM portal_orders`, `UPDATE portal_numina_contacts SET contact_id='x'`, `DELETE FROM portal_order_charges`} {
		if _, err := b.pool.Exec(b.f.ctx, statement); err == nil {
			t.Fatalf("runtime ran %q", statement)
		}
	}
	migrator := migrationDB(t)
	var before string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=15`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	rewindOrdersFixture(t, migrator)
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal(err)
		}
	}
	var after string
	var tables int
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=15`).Scan(&after); err != nil || after != before {
		t.Fatal("v16 changed earlier checksums", err)
	}
	if err := migrator.QueryRow(t.Context(), `SELECT count(*) FROM pg_tables WHERE tablename IN ('portal_orders','portal_order_charges','portal_numina_contacts')`).Scan(&tables); err != nil || tables != 3 {
		t.Fatal("v16 tables were not recreated", err)
	}
}
