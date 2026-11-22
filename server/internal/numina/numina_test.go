package numina

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettledFollowsTheFundingRule(t *testing.T) {
	state := func(s string) *string { return &s }
	amount := func(s string) *json.Number { n := json.Number(s); return &n }
	for _, tc := range []struct {
		name string
		in   Invoice
		want bool
	}{
		{"paid and nothing owed", Invoice{Status: "paid", PaymentState: state("paid"), OutstandingBalance: amount("0")}, true},
		{"paid with decimals", Invoice{Status: "paid", PaymentState: state("paid"), OutstandingBalance: amount("0.00")}, true},
		{"still owed", Invoice{Status: "sent", PaymentState: state("open"), OutstandingBalance: amount("2995.00")}, false},
		{"unmatched payment", Invoice{Status: "sent", PaymentState: state("covered_by_unmatched_payments"), OutstandingBalance: amount("2995.00")}, false},
		{"exchange difference", Invoice{Status: "sent", PaymentState: state("exchange_difference"), OutstandingBalance: amount("0.01")}, false},
		{"paid but a remainder", Invoice{Status: "paid", PaymentState: state("paid"), OutstandingBalance: amount("10")}, false},
		{"draft", Invoice{Status: "draft", PaymentState: state("paid"), OutstandingBalance: amount("0")}, false},
		{"no state", Invoice{Status: "sent", OutstandingBalance: amount("0")}, false},
		{"no balance", Invoice{Status: "paid", PaymentState: state("paid")}, false},
	} {
		if got := tc.in.Settled(); got != tc.want {
			t.Errorf("%s: Settled()=%v", tc.name, got)
		}
	}
}

func TestAmountIsExactDecimalKroner(t *testing.T) {
	for minor, want := range map[int64]string{300000: "3000.00", 239600: "2396.00", 7920: "79.20", 5: "0.05"} {
		if got := Amount(minor); string(got) != want {
			t.Errorf("Amount(%d)=%s, want %s", minor, got, want)
		}
	}
}

func TestMinorParsesExactAmounts(t *testing.T) {
	for in, want := range map[string]int64{"3750": 375000, "3750.0": 375000, "3750.00": 375000, "2396.5": 239650, "0.05": 5, "0": 0} {
		if got, ok := Minor(json.Number(in)); !ok || got != want {
			t.Errorf("Minor(%s)=%d,%v want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "1.234", "-1", "+1", "1.", ".5", "1e3", "abc", "99999999999"} {
		if _, ok := Minor(json.Number(in)); ok {
			t.Errorf("Minor(%q) accepted", in)
		}
	}
}

func TestClientSendsKeyAndTokenAndHidesResponseText(t *testing.T) {
	var seen *http.Request
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(r.Context())
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch r.URL.Path {
		case "/v1/invoices":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"inv-1","status":"draft","memo":"Portal reference: x"}`))
		case "/v1/invoices/inv-1/send":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"forbidden","error_description":"private detail"}`))
		default:
			http.Redirect(w, r, "https://elsewhere.invalid/", http.StatusFound)
		}
	}))
	defer server.Close()
	c, err := New(server.URL, "fictional-token", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateInvoice(t.Context(), "portal-KEY-acceptance", NewInvoice{ContactID: "c1", Items: []Line{{Description: "x", Quantity: 1, UnitPrice: Amount(239600)}}})
	if err != nil || created.ID != "inv-1" {
		t.Fatal("create failed", err)
	}
	if seen.Header.Get("Authorization") != "Bearer fictional-token" || seen.Header.Get("Idempotency-Key") != "portal-KEY-acceptance" {
		t.Fatal("token or idempotency key not sent")
	}
	items, _ := body["items"].([]any)
	var line map[string]any
	if len(items) == 1 {
		line, _ = items[0].(map[string]any)
	}
	if line["unit_price"] != 2396.0 || line["vat_code"] != "" {
		t.Fatalf("unit price not sent as a number: %v", body["items"])
	}
	err = c.SendInvoice(t.Context(), "k", "inv-1")
	if !IsStatus(err, http.StatusForbidden) || strings.Contains(err.Error(), "private detail") || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("error lost its status or leaked text: %v", err)
	}
	if _, err = c.GetInvoice(t.Context(), "elsewhere"); !IsStatus(err, http.StatusFound) {
		t.Fatalf("redirect was followed: %v", err)
	}
	if _, err := New("ftp://numina.invalid", "x", nil); err == nil {
		t.Fatal("non-HTTP base URL accepted")
	}
	if _, err := New(DefaultBaseURL, "", nil); err == nil {
		t.Fatal("empty token accepted")
	}
}
