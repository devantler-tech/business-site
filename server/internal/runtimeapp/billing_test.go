package runtimeapp

import (
	"path/filepath"
	"testing"
)

func TestBillingConfigIsOptionalAndStrict(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	none, err := readBillingConfig(env(nil))
	if err != nil || none != (billingConfig{}) {
		t.Fatal("absent billing configuration was not empty", err)
	}
	defaults, err := readBillingConfig(env(map[string]string{"PORTAL_NUMINA_TOKEN_FILE": "/run/numina/token"}))
	if err != nil || defaults.baseURL != "https://api.numina.app" || defaults.vatCode != "U25" || defaults.salesAccount != 1000 {
		t.Fatalf("defaults differ: %+v %v", defaults, err)
	}
	set, err := readBillingConfig(env(map[string]string{"PORTAL_NUMINA_TOKEN_FILE": "/run/numina/token", "PORTAL_NUMINA_API_URL": "https://sandbox.numina.invalid", "PORTAL_NUMINA_VAT_CODE": "U25", "PORTAL_NUMINA_SALES_ACCOUNT": "1010"}))
	if err != nil || set.baseURL != "https://sandbox.numina.invalid" || set.salesAccount != 1010 {
		t.Fatalf("explicit values differ: %+v %v", set, err)
	}
	for name, values := range map[string]map[string]string{
		"settings without a key": {"PORTAL_NUMINA_VAT_CODE": "U25"},
		"relative key file":      {"PORTAL_NUMINA_TOKEN_FILE": "token"},
		"plain HTTP":             {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_API_URL": "http://api.numina.app"},
		"URL with credentials":   {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_API_URL": "https://u:p@api.numina.app"},
		"URL with a path":        {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_API_URL": "https://api.numina.app/v1"},
		"lowercase VAT code":     {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_VAT_CODE": "u25"},
		"non-numeric account":    {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_SALES_ACCOUNT": "10a"},
		"zero-padded account":    {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_SALES_ACCOUNT": "01000"},
		"account out of range":   {"PORTAL_NUMINA_TOKEN_FILE": "/k", "PORTAL_NUMINA_SALES_ACCOUNT": "100000"},
	} {
		if _, err := readBillingConfig(env(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// Billing without the portal's own dependencies is refused as a whole.
	if _, err := ReadConfig(env(map[string]string{"PORTAL_NUMINA_TOKEN_FILE": "/run/numina/token"})); err == nil {
		t.Fatal("billing configured without a portal")
	}
}

func TestBillingStartsOnlyWithAReadableKey(t *testing.T) {
	stop, err := startInvoiceBilling(nil, billingConfig{})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	missing := billingConfig{tokenFile: filepath.Join(t.TempDir(), "absent"), baseURL: "https://api.numina.app", vatCode: "U25", salesAccount: 1000}
	if _, err := startInvoiceBilling(nil, missing); err == nil {
		t.Fatal("missing key file started billing")
	}
}

func TestCardConfigIsOptionalAndStrict(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	if c, err := readCardConfig(env(nil)); err != nil || c != (cardConfig{}) {
		t.Fatal("absent card configuration was not empty", err)
	}
	c, err := readCardConfig(env(map[string]string{"PORTAL_STRIPE_KEY_FILE": "/run/stripe/key", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/run/stripe/webhook", "PORTAL_STRIPE_TAX_RATE": "txr_moms25"}))
	if err != nil || c.baseURL != "https://api.stripe.com" || c.taxRate != "txr_moms25" {
		t.Fatalf("defaults differ: %+v %v", c, err)
	}
	for name, values := range map[string]map[string]string{
		"key without webhook secret": {"PORTAL_STRIPE_KEY_FILE": "/k"},
		"webhook secret without key": {"PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w"},
		"relative files":             {"PORTAL_STRIPE_KEY_FILE": "k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "w"},
		"URL without files":          {"PORTAL_STRIPE_API_URL": "https://api.stripe.com"},
		"plain HTTP":                 {"PORTAL_STRIPE_KEY_FILE": "/k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w", "PORTAL_STRIPE_TAX_RATE": "txr_moms25", "PORTAL_STRIPE_API_URL": "http://api.stripe.com"},
		"URL with a path":            {"PORTAL_STRIPE_KEY_FILE": "/k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w", "PORTAL_STRIPE_TAX_RATE": "txr_moms25", "PORTAL_STRIPE_API_URL": "https://api.stripe.com/v1"},
		"files without a tax rate":   {"PORTAL_STRIPE_KEY_FILE": "/k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w"},
		"malformed tax rate":         {"PORTAL_STRIPE_KEY_FILE": "/k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w", "PORTAL_STRIPE_TAX_RATE": "25"},
		"tax rate without files":     {"PORTAL_STRIPE_TAX_RATE": "txr_moms25"},
	} {
		if _, err := readCardConfig(env(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := ReadConfig(env(map[string]string{"PORTAL_STRIPE_KEY_FILE": "/k", "PORTAL_STRIPE_WEBHOOK_SECRET_FILE": "/w", "PORTAL_STRIPE_TAX_RATE": "txr_moms25"})); err == nil {
		t.Fatal("card billing configured without a portal")
	}
	if err := enableCardBilling(nil, cardConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := enableCardBilling(nil, cardConfig{keyFile: filepath.Join(t.TempDir(), "absent"), webhookSecretFile: "/absent", baseURL: "https://api.stripe.com"}); err == nil {
		t.Fatal("missing key file enabled card billing")
	}
}
