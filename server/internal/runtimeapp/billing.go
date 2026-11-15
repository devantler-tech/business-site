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
)

var numinaVATCode = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

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
