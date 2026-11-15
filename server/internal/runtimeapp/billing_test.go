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
