// Package stripe is the portal's small Stripe client for the card path of
// ADR 0030: it creates Checkout Sessions and verifies webhook signatures.
// Errors carry the HTTP status and Stripe's error code, never response text or
// the API key.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Stripe's API.
const DefaultBaseURL = "https://api.stripe.com"

// APIVersion pins request and response shapes. Create the webhook endpoint
// with the same version so event payloads match what Event parsing expects.
const APIVersion = "2026-10-28.endive"

// integrationIdentifier tags the portal's sessions in the Stripe Dashboard.
const integrationIdentifier = "devantler-portal-qmvhtkzr"

// Client calls Stripe with one API key.
type Client struct {
	baseURL, key string
	http         *http.Client
}

// New returns a client with bounded timeouts that never follows redirects.
func New(baseURL, key string, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || key == "" {
		return nil, errors.New("stripe: invalid client configuration")
	}
	return &Client{baseURL: u.String(), key: key, http: &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

// Error is a refused or failed call. Code is Stripe's error code, if any.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return fmt.Sprintf("stripe: status %d %s", e.Status, e.Code) }

// Line is one Checkout line in DKK minor units, VAT included.
type Line struct {
	Name        string
	AmountMinor int64
	Monthly     bool
	Addon       string // the portal add-on the line charges, kept on its product
}

// Checkout describes one Checkout Session. Mode is "payment" for one-off lines
// or "subscription" for monthly lines only. Payment methods
// are left to the Dashboard: Stripe offers MobilePay only for one-off
// payments, because MobilePay does not support recurring charges.
//
// TaxRate is the Dashboard's inclusive 25% Danish VAT rate. Every line carries
// it, so the invoice Stripe issues for the payment shows the net amount, the
// VAT rate and the VAT amount. A one-off payment asks Stripe for that paid
// invoice; a subscription gets one for every charge anyway. Checkout collects
// the client's address and, for a business, its name and VAT number for it.
type Checkout struct {
	Mode               string
	TaxRate            string
	Lines              []Line
	ClientReference    string
	CustomerEmail      string
	SuccessURL         string
	CancelURL          string
	Locale             string
	BillingCycleAnchor time.Time // subscription only: the first monthly charge
	Metadata           map[string]string
}

// Session is the subset of a Checkout Session the portal reads.
type Session struct {
	ID                string `json:"id"`
	URL               string `json:"url"`
	Mode              string `json:"mode"`
	Status            string `json:"status"`
	PaymentStatus     string `json:"payment_status"`
	AmountTotal       int64  `json:"amount_total"`
	Currency          string `json:"currency"`
	ExpiresAt         int64  `json:"expires_at"`
	ClientReferenceID string `json:"client_reference_id"`
	Subscription      string `json:"subscription"`
}

func (c Checkout) form() (url.Values, error) {
	if (c.Mode != "payment" && c.Mode != "subscription") || len(c.Lines) == 0 {
		return nil, errors.New("stripe: invalid checkout")
	}
	f := url.Values{
		"mode": {c.Mode}, "client_reference_id": {c.ClientReference},
		"success_url": {c.SuccessURL}, "cancel_url": {c.CancelURL},
		"locale": {c.Locale}, "integration_identifier": {integrationIdentifier},
		"billing_address_collection": {"required"}, "tax_id_collection[enabled]": {"true"},
	}
	if !taxRateID.MatchString(c.TaxRate) {
		return nil, errors.New("stripe: checkout needs a tax rate")
	}
	if c.Mode == "payment" {
		f.Set("invoice_creation[enabled]", "true")
		f.Set("invoice_creation[invoice_data][rendering_options][amount_tax_display]", "exclude_tax")
	}
	if c.CustomerEmail != "" {
		f.Set("customer_email", c.CustomerEmail)
	}
	monthly := false
	for i, line := range c.Lines {
		if line.AmountMinor <= 0 || line.Name == "" {
			return nil, errors.New("stripe: invalid checkout line")
		}
		p := fmt.Sprintf("line_items[%d]", i)
		f.Set(p+"[quantity]", "1")
		f.Set(p+"[price_data][currency]", "dkk")
		f.Set(p+"[price_data][unit_amount]", strconv.FormatInt(line.AmountMinor, 10))
		f.Set(p+"[price_data][product_data][name]", line.Name)
		// Every line names what it charges, so an add-on's subscription item
		// is never mistaken for another: see RemoveSubscriptionAddon.
		if line.Addon != "" {
			f.Set(p+"[price_data][product_data][metadata][portal_addon]", line.Addon)
		} else {
			f.Set(p+"[price_data][product_data][metadata][portal_line]", "order")
		}
		f.Set(p+"[tax_rates][0]", c.TaxRate)
		if line.Monthly {
			monthly = true
			f.Set(p+"[price_data][recurring][interval]", "month")
		}
	}
	if monthly != (c.Mode == "subscription") {
		return nil, errors.New("stripe: monthly lines need subscription mode")
	}
	if c.Mode == "subscription" {
		for _, line := range c.Lines {
			// Stripe refuses one-time prices without proration.
			if !line.Monthly {
				return nil, errors.New("stripe: a subscription carries only monthly lines")
			}
		}
	}
	target := "metadata"
	if c.Mode == "subscription" {
		target = "subscription_data[metadata]"
		if c.BillingCycleAnchor.IsZero() {
			return nil, errors.New("stripe: subscription needs a billing anchor")
		}
		f.Set("subscription_data[billing_cycle_anchor]", strconv.FormatInt(c.BillingCycleAnchor.Unix(), 10))
		f.Set("subscription_data[proration_behavior]", "none")
	}
	for k, v := range c.Metadata {
		f.Set(target+"["+k+"]", v)
	}
	return f, nil
}

// CreateCheckout creates a Checkout Session. The key makes a retry within
// Stripe's 24-hour idempotency window return the original session.
func (c *Client) CreateCheckout(ctx context.Context, key string, checkout Checkout) (Session, error) {
	form, err := checkout.form()
	if err != nil {
		return Session{}, err
	}
	var s Session
	if err := c.call(ctx, http.MethodPost, "/v1/checkout/sessions", form, key, &s); err != nil {
		return Session{}, err
	}
	if s.ID == "" || !strings.HasPrefix(s.URL, "https://") {
		return Session{}, errors.New("stripe: response malformed")
	}
	return s, nil
}

var (
	paymentIntentID = regexp.MustCompile(`^pi_[A-Za-z0-9_]{1,250}$`)
	subscriptionID  = regexp.MustCompile(`^sub_[A-Za-z0-9_]{1,250}$`)
	invoiceID       = regexp.MustCompile(`^in_[A-Za-z0-9_]{1,250}$`)
	taxRateID       = regexp.MustCompile(`^txr_[A-Za-z0-9_]{1,250}$`)
)

// GetInvoice reads one invoice, for example to learn whose subscription a
// refunded payment belonged to.
func (c *Client) GetInvoice(ctx context.Context, id string) (Invoice, error) {
	if !invoiceID.MatchString(id) {
		return Invoice{}, errors.New("stripe: invalid invoice")
	}
	var i Invoice
	if err := c.call(ctx, http.MethodGet, "/v1/invoices/"+id, url.Values{}, "", &i); err != nil {
		return Invoice{}, err
	}
	if i.ID != id {
		return Invoice{}, errors.New("stripe: response malformed")
	}
	return i, nil
}

// TaxRate is the subset of a tax rate the portal checks before using it.
type TaxRate struct {
	ID         string  `json:"id"`
	Active     bool    `json:"active"`
	Inclusive  bool    `json:"inclusive"`
	Percentage float64 `json:"percentage"`
}

// GetTaxRate reads one tax rate.
func (c *Client) GetTaxRate(ctx context.Context, id string) (TaxRate, error) {
	if !taxRateID.MatchString(id) {
		return TaxRate{}, errors.New("stripe: invalid tax rate")
	}
	var r TaxRate
	if err := c.call(ctx, http.MethodGet, "/v1/tax_rates/"+id, url.Values{}, "", &r); err != nil {
		return TaxRate{}, err
	}
	if r.ID != id {
		return TaxRate{}, errors.New("stripe: response malformed")
	}
	return r, nil
}

// CancelSubscriptionNow ends a subscription immediately, without proration,
// and returns when Stripe ended it.
func (c *Client) CancelSubscriptionNow(ctx context.Context, subscription string) (time.Time, error) {
	if !subscriptionID.MatchString(subscription) {
		return time.Time{}, errors.New("stripe: invalid subscription")
	}
	var s struct {
		ID         string `json:"id"`
		CanceledAt *int64 `json:"canceled_at"`
	}
	if err := c.call(ctx, http.MethodDelete, "/v1/subscriptions/"+subscription, url.Values{}, "", &s); err != nil {
		return time.Time{}, err
	}
	if s.ID != subscription || s.CanceledAt == nil {
		return time.Time{}, errors.New("stripe: response malformed")
	}
	return time.Unix(*s.CanceledAt, 0), nil
}

// PaymentReferences returns the Checkout Sessions and invoices a PaymentIntent
// paid for, so a refund or dispute can be matched to the portal's own records.
// A charge in current API versions names only its PaymentIntent.
func (c *Client) PaymentReferences(ctx context.Context, paymentIntent string) ([]string, error) {
	if !paymentIntentID.MatchString(paymentIntent) {
		return nil, errors.New("stripe: invalid payment intent")
	}
	var sessions struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/checkout/sessions", url.Values{"payment_intent": {paymentIntent}, "limit": {"10"}}, "", &sessions); err != nil {
		return nil, err
	}
	var payments struct {
		Data []struct {
			Invoice string `json:"invoice"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/invoice_payments", url.Values{"payment[type]": {"payment_intent"}, "payment[payment_intent]": {paymentIntent}, "limit": {"10"}}, "", &payments); err != nil {
		return nil, err
	}
	var refs []string
	for _, s := range sessions.Data {
		refs = append(refs, s.ID)
	}
	for _, p := range payments.Data {
		refs = append(refs, p.Invoice)
	}
	return refs, nil
}

// CancelSubscriptionAt ends a subscription at the given time without
// proration and returns the time Stripe confirmed.
func (c *Client) CancelSubscriptionAt(ctx context.Context, subscription string, at time.Time) (time.Time, error) {
	if !subscriptionID.MatchString(subscription) {
		return time.Time{}, errors.New("stripe: invalid subscription")
	}
	var s struct {
		ID       string `json:"id"`
		CancelAt *int64 `json:"cancel_at"`
	}
	form := url.Values{"cancel_at": {strconv.FormatInt(at.Unix(), 10)}, "proration_behavior": {"none"}}
	if err := c.call(ctx, http.MethodPost, "/v1/subscriptions/"+subscription, form, "", &s); err != nil {
		return time.Time{}, err
	}
	if s.ID != subscription || s.CancelAt == nil {
		return time.Time{}, errors.New("stripe: response malformed")
	}
	return time.Unix(*s.CancelAt, 0), nil
}

// call sends one request: a GET carries form as its query, a POST as its body
// with the idempotency key, if any. Errors carry only status and Stripe's code.
func (c *Client) call(ctx context.Context, method, path string, form url.Values, key string, out any) error {
	target, body := c.baseURL+path, ""
	if method == http.MethodGet || method == http.MethodDelete {
		target += "?" + form.Encode()
	} else {
		body = form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Stripe-Version", APIVersion)
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("stripe: request failed")
	}
	defer func() {
		// Draining is best-effort; the decoded result or status is the verdict.
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errors.New("stripe: response unreadable")
	}
	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Error struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		code := problem.Error.Code
		if code == "" {
			code = problem.Error.Type
		}
		return &Error{Status: resp.StatusCode, Code: code}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("stripe: response malformed")
	}
	return nil
}

// ErrSignature is any webhook that is not provably from Stripe.
var ErrSignature = errors.New("stripe: invalid webhook signature")

// VerifySignature checks a Stripe-Signature header against the endpoint's
// signing secret: an HMAC-SHA256 of "<t>.<payload>" in any v1 entry, with the
// timestamp no more than five minutes from now.
func VerifySignature(payload []byte, header, secret string, now time.Time) error {
	if secret == "" {
		return ErrSignature
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		switch {
		case !ok:
		case key == "t":
			timestamp = value
		case key == "v1":
			signatures = append(signatures, value)
		}
	}
	t, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || len(signatures) == 0 {
		return ErrSignature
	}
	if age := now.Sub(time.Unix(t, 0)); age > 5*time.Minute || age < -5*time.Minute {
		return ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, signature := range signatures {
		got, err := hex.DecodeString(signature)
		if err == nil && hmac.Equal(got, want) {
			return nil
		}
	}
	return ErrSignature
}

// Sign returns a Stripe-Signature header for payload. Tests use it to send
// genuinely signed fixture events; production code only verifies.
func Sign(payload []byte, secret string, at time.Time) string {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(payload)
	return "t=" + timestamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

// Event is a webhook event with its object kept as raw JSON.
type Event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// Invoice is the subset of a subscription invoice the portal reads. Current
// API versions name the subscription under parent; older ones at the top.
type Invoice struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	BillingReason string `json:"billing_reason"`
	AmountPaid    int64  `json:"amount_paid"`
	Currency      string `json:"currency"`
	Created       int64  `json:"created"`
	Subscription  string `json:"subscription"`
	Parent        struct {
		SubscriptionDetails struct {
			Subscription string            `json:"subscription"`
			Metadata     map[string]string `json:"metadata"`
		} `json:"subscription_details"`
	} `json:"parent"`
}

// SubscriptionID returns the invoice's subscription in either API shape.
func (i Invoice) SubscriptionID() string {
	if i.Parent.SubscriptionDetails.Subscription != "" {
		return i.Parent.SubscriptionDetails.Subscription
	}
	return i.Subscription
}

// Charge is the subset of a refunded charge the portal reads.
type Charge struct {
	ID             string `json:"id"`
	PaymentIntent  string `json:"payment_intent"`
	AmountRefunded int64  `json:"amount_refunded"`
}

// Dispute is the subset of a dispute the portal reads.
type Dispute struct {
	ID            string `json:"id"`
	PaymentIntent string `json:"payment_intent"`
}

// Subscription is the subset of an ended subscription the portal reads.
type Subscription struct {
	ID       string            `json:"id"`
	EndedAt  int64             `json:"ended_at"`
	Metadata map[string]string `json:"metadata"`
}

// RemoveSubscriptionAddon removes the subscription item that charges one portal
// add-on, without proration: the current month stays paid and the next invoice
// no longer charges it. An add-on already removed is not an error, so a retry
// converges. A subscription with an item that names neither an add-on nor the
// order (one created before lines were named), or with more items than one
// page shows, is refused: its add-on's removal cannot be confirmed.
func (c *Client) RemoveSubscriptionAddon(ctx context.Context, subscription, addon string) error {
	if !subscriptionID.MatchString(subscription) || addon == "" {
		return errors.New("stripe: invalid subscription add-on")
	}
	var s struct {
		ID    string `json:"id"`
		Items struct {
			HasMore bool `json:"has_more"`
			Data    []struct {
				ID    string `json:"id"`
				Price struct {
					Product struct {
						Metadata map[string]string `json:"metadata"`
					} `json:"product"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/subscriptions/"+subscription, url.Values{"expand[]": {"items.data.price.product"}}, "", &s); err != nil {
		return err
	}
	if s.ID != subscription {
		return errors.New("stripe: response malformed")
	}
	if s.Items.HasMore {
		return errors.New("stripe: subscription has more items than one page")
	}
	for _, item := range s.Items.Data {
		if item.Price.Product.Metadata["portal_addon"] == "" && item.Price.Product.Metadata["portal_line"] == "" {
			return errors.New("stripe: subscription item names no portal line")
		}
	}
	for _, item := range s.Items.Data {
		if item.Price.Product.Metadata["portal_addon"] != addon {
			continue
		}
		var removed struct {
			ID      string `json:"id"`
			Deleted bool   `json:"deleted"`
		}
		if err := c.call(ctx, http.MethodDelete, "/v1/subscription_items/"+item.ID, url.Values{"proration_behavior": {"none"}}, "", &removed); err != nil {
			return err
		}
		if removed.ID != item.ID || !removed.Deleted {
			return errors.New("stripe: response malformed")
		}
	}
	return nil
}
