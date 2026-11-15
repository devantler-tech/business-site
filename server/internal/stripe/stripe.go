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
}

// Checkout describes one Checkout Session. Mode is "payment" for one-off lines
// or "subscription" for monthly lines only. Payment methods
// are left to the Dashboard: Stripe offers MobilePay only for one-off
// payments, because MobilePay does not support recurring charges.
type Checkout struct {
	Mode               string
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

// CreateCheckout creates a Checkout Session. Its Idempotency-Key is the given
// key bound to a digest of the exact request. A retry with the same parameters
// returns the original session within Stripe's 24-hour window. A retry whose
// parameters changed (another language, email or anchor) gets a fresh key
// instead of Stripe's refusal to reuse one. A session whose reply was lost was
// never shown to anyone, so it can only expire.
func (c *Client) CreateCheckout(ctx context.Context, key string, checkout Checkout) (Session, error) {
	form, err := checkout.form()
	if err != nil {
		return Session{}, err
	}
	body := form.Encode()
	digest := sha256.Sum256([]byte(body))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/checkout/sessions", strings.NewReader(body))
	if err != nil {
		return Session{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Stripe-Version", APIVersion)
	req.Header.Set("Idempotency-Key", key+"-"+hex.EncodeToString(digest[:8]))
	resp, err := c.http.Do(req)
	if err != nil {
		return Session{}, errors.New("stripe: request failed")
	}
	defer func() {
		// Draining is best-effort; the decoded result or status is the verdict.
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Session{}, errors.New("stripe: response unreadable")
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
		return Session{}, &Error{Status: resp.StatusCode, Code: code}
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil || s.ID == "" || !strings.HasPrefix(s.URL, "https://") {
		return Session{}, errors.New("stripe: response malformed")
	}
	return s, nil
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
