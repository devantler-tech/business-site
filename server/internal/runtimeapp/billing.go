package runtimeapp

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/devantler-tech/business-site/server/internal/numina"
	"github.com/devantler-tech/business-site/server/internal/stripe"
)

var numinaVATCode = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

var stripeTaxRate = regexp.MustCompile(`^txr_[A-Za-z0-9_]{1,250}$`)

// readBillingConfig reads the optional Numina invoice path. Without
// PORTAL_NUMINA_TOKEN_FILE every other billing setting must be absent.
func readBillingConfig(getenv func(string) string) (billingConfig, error) {
	b := billingConfig{tokenFile: getenv("PORTAL_NUMINA_TOKEN_FILE"), baseURL: getenv("PORTAL_NUMINA_API_URL"), vatCode: getenv("PORTAL_NUMINA_VAT_CODE")}
	account := getenv("PORTAL_NUMINA_SALES_ACCOUNT")
	if b.tokenFile == "" {
		if b.baseURL != "" || b.vatCode != "" || account != "" {
			return billingConfig{}, ErrConfig
		}
		return billingConfig{}, nil
	}
	if !filepath.IsAbs(b.tokenFile) {
		return billingConfig{}, ErrConfig
	}
	if b.baseURL == "" {
		b.baseURL = numina.DefaultBaseURL
	}
	if u, err := url.Parse(b.baseURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return billingConfig{}, ErrConfig
	}
	if b.vatCode == "" {
		b.vatCode = "U25"
	}
	if !numinaVATCode.MatchString(b.vatCode) {
		return billingConfig{}, ErrConfig
	}
	b.salesAccount = 1000
	if account != "" {
		n, err := strconv.Atoi(account)
		if err != nil || n < 1 || n > 99999 || strconv.Itoa(n) != account {
			return billingConfig{}, ErrConfig
		}
		b.salesAccount = n
	}
	return b, nil
}

// startInvoiceBilling reads the Numina key from its projected file and starts
// the sync loop. The returned function stops it. Nothing is invoiced while the
// portal-invoice-billing gate is off.
func startInvoiceBilling(f *identity.Foundation, b billingConfig) (func(), error) {
	if b == (billingConfig{}) {
		return func() {}, nil
	}
	if os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return nil, ErrConfig
	}
	data, err := projectionBytes(b.tokenFile)
	if err != nil {
		return nil, ErrConfig
	}
	token := trimInputLineEnding(string(data))
	client, err := numina.New(b.baseURL, token, nil)
	if err != nil {
		return nil, ErrConfig
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.RunInvoiceBilling(ctx, identity.InvoiceBilling{Numina: client, SalesAccount: b.salesAccount, VATCode: b.vatCode, Now: time.Now}, 15*time.Minute)
	}()
	return func() { cancel(); <-done }, nil
}

// cardConfig is the optional Stripe card path (ADR 0030, #553).
type cardConfig struct {
	keyFile, webhookSecretFile, baseURL, taxRate string
}

// readCardConfig reads the optional Stripe card path: both secret files and the
// inclusive 25% VAT tax rate, or none of them. The API URL is optional and
// HTTPS only.
func readCardConfig(getenv func(string) string) (cardConfig, error) {
	c := cardConfig{keyFile: getenv("PORTAL_STRIPE_KEY_FILE"), webhookSecretFile: getenv("PORTAL_STRIPE_WEBHOOK_SECRET_FILE"), baseURL: getenv("PORTAL_STRIPE_API_URL"), taxRate: getenv("PORTAL_STRIPE_TAX_RATE")}
	if c == (cardConfig{}) {
		return c, nil
	}
	if !filepath.IsAbs(c.keyFile) || !filepath.IsAbs(c.webhookSecretFile) || !stripeTaxRate.MatchString(c.taxRate) {
		return cardConfig{}, ErrConfig
	}
	if c.baseURL == "" {
		c.baseURL = stripe.DefaultBaseURL
	}
	if u, err := url.Parse(c.baseURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return cardConfig{}, ErrConfig
	}
	return c, nil
}

// enableCardBilling reads the Stripe key and webhook secret from their
// projected files. Nothing is charged while portal-card-billing is off.
func enableCardBilling(f *identity.Foundation, c cardConfig) error {
	if c == (cardConfig{}) {
		return nil
	}
	if os.Geteuid() <= 0 || os.Getegid() <= 0 {
		return ErrConfig
	}
	var values [2]string
	for i, file := range []string{c.keyFile, c.webhookSecretFile} {
		data, err := projectionBytes(file)
		if err != nil {
			return ErrConfig
		}
		values[i] = trimInputLineEnding(string(data))
	}
	client, err := stripe.New(c.baseURL, values[0], nil)
	if err != nil {
		return ErrConfig
	}
	return f.EnableCardBilling(identity.CardBilling{Stripe: client, WebhookSecret: values[1], TaxRate: c.taxRate, Now: time.Now})
}
