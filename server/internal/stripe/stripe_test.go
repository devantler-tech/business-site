package stripe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignatureVerification(t *testing.T) {
	payload := []byte(`{"id":"evt_1","type":"checkout.session.completed"}`)
	now := time.Unix(1_800_000_000, 0)
	good := Sign(payload, "whsec_fixture", now)
	if err := VerifySignature(payload, good, "whsec_fixture", now.Add(time.Minute)); err != nil {
		t.Fatal("genuine signature refused", err)
	}
	if err := VerifySignature(payload, "v1=00,"+good, "whsec_fixture", now); err != nil {
		t.Fatal("a second, valid v1 entry was not accepted", err)
	}
	for name, tc := range map[string]struct {
		payload        []byte
		header, secret string
		at             time.Time
	}{
		"changed payload": {[]byte(`{"id":"evt_2"}`), good, "whsec_fixture", now},
		"wrong secret":    {payload, good, "whsec_other", now},
		"empty secret":    {payload, good, "", now},
		"too old":         {payload, good, "whsec_fixture", now.Add(6 * time.Minute)},
		"from the future": {payload, good, "whsec_fixture", now.Add(-6 * time.Minute)},
		"no v1":           {payload, strings.Split(good, ",")[0], "whsec_fixture", now},
		"no timestamp":    {payload, strings.Split(good, ",")[1], "whsec_fixture", now},
		"v0 only":         {payload, strings.Replace(good, "v1=", "v0=", 1), "whsec_fixture", now},
		"not hex":         {payload, strings.Split(good, ",")[0] + ",v1=zz", "whsec_fixture", now},
	} {
		if err := VerifySignature(tc.payload, tc.header, tc.secret, tc.at); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestCheckoutRequestShape(t *testing.T) {
	var form url.Values
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		_ = r.ParseForm()
		form = r.PostForm
		if r.PostForm.Get("mode") == "payment" && r.PostForm.Get("line_items[0][price_data][unit_amount]") == "1" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"amount_too_small","message":"private"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "cs_test_1", "url": "https://checkout.stripe.com/c/pay/cs_test_1", "mode": r.PostForm.Get("mode"), "status": "open", "expires_at": 1})
	}))
	defer server.Close()
	c, err := New(server.URL, "rk_test_fixture", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	s, err := c.CreateCheckout(t.Context(), "portal-X-checkout-1", Checkout{Mode: "subscription", ClientReference: "ORDER", SuccessURL: "https://portal.invalid/ok", CancelURL: "https://portal.invalid/back", Locale: "da", BillingCycleAnchor: anchor, Metadata: map[string]string{"portal_order": "ORDER"},
		Lines: []Line{{Name: "Hosting", AmountMinor: 12500, Monthly: true}}})
	if err != nil || s.ID != "cs_test_1" {
		t.Fatal("checkout failed", err)
	}
	if headers.Get("Authorization") != "Bearer rk_test_fixture" || headers.Get("Idempotency-Key") != "portal-X-checkout-1" || headers.Get("Stripe-Version") != APIVersion {
		t.Fatal("key, idempotency key or API version not sent")
	}
	for field, want := range map[string]string{
		"mode": "subscription", "line_items[0][price_data][unit_amount]": "12500", "line_items[0][price_data][recurring][interval]": "month",
		"line_items[0][price_data][currency]": "dkk", "subscription_data[billing_cycle_anchor]": "1796083200",
		"subscription_data[proration_behavior]": "none", "subscription_data[metadata][portal_order]": "ORDER", "client_reference_id": "ORDER",
	} {
		if form.Get(field) != want {
			t.Errorf("%s=%q, want %q", field, form.Get(field), want)
		}
	}
	for field := range form {
		if strings.HasPrefix(field, "payment_method_types") || strings.HasPrefix(field, "line_items[1]") {
			t.Errorf("unexpected field %s", field)
		}
	}
	if _, err := c.CreateCheckout(t.Context(), "k", Checkout{Mode: "payment", Lines: []Line{{Name: "x", AmountMinor: 1}}}); err == nil || strings.Contains(err.Error(), "private") || !strings.Contains(err.Error(), "amount_too_small") {
		t.Fatalf("refusal lost its code or leaked text: %v", err)
	}
	for name, bad := range map[string]Checkout{
		"monthly in payment mode":       {Mode: "payment", Lines: []Line{{Name: "x", AmountMinor: 100, Monthly: true}}},
		"no monthly in subscription":    {Mode: "subscription", BillingCycleAnchor: anchor, Lines: []Line{{Name: "x", AmountMinor: 100}}},
		"one-time line in subscription": {Mode: "subscription", BillingCycleAnchor: anchor, Lines: []Line{{Name: "x", AmountMinor: 100}, {Name: "y", AmountMinor: 100, Monthly: true}}},
		"subscription without anchor":   {Mode: "subscription", Lines: []Line{{Name: "x", AmountMinor: 100, Monthly: true}}},
		"zero amount":                   {Mode: "payment", Lines: []Line{{Name: "x"}}},
		"no lines":                      {Mode: "payment"},
	} {
		if _, err := c.CreateCheckout(t.Context(), "k", bad); err == nil {
			t.Errorf("%s was sent", name)
		}
	}
}

func TestInvoiceSubscriptionInBothShapes(t *testing.T) {
	var current, legacy Invoice
	if err := json.Unmarshal([]byte(`{"id":"in_1","parent":{"subscription_details":{"subscription":"sub_new"}}}`), &current); err != nil || current.SubscriptionID() != "sub_new" {
		t.Fatal("current shape not read", err)
	}
	if err := json.Unmarshal([]byte(`{"id":"in_2","subscription":"sub_old"}`), &legacy); err != nil || legacy.SubscriptionID() != "sub_old" {
		t.Fatal("legacy shape not read", err)
	}
}
