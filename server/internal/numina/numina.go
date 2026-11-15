// Package numina is the portal's small client for the Numina public API
// (https://developers.numina.app). It covers only the invoice path of ADR 0030:
// contacts, draft invoices, booking, sending and reading payment state.
// Errors carry the HTTP status and Numina's error code, never response text or
// the API key.
package numina

import (
	"bytes"
	"context"
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

// DefaultBaseURL is Numina's production API.
const DefaultBaseURL = "https://api.numina.app"

// Client calls one Numina company with one API key.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client with bounded timeouts that never follows redirects.
func New(baseURL, token string, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || token == "" {
		return nil, errors.New("numina: invalid client configuration")
	}
	return &Client{baseURL: u.String(), token: token, http: &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

// Error is a refused or failed call. Code is Numina's `error` value, if any.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return fmt.Sprintf("numina: status %d %s", e.Status, e.Code) }

// IsStatus reports whether err is a Numina response with that HTTP status.
func IsStatus(err error, status int) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == status
}

// Contact is the subset of a Numina contact the portal reads and writes.
type Contact struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	AttnPerson  string `json:"attn_person,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
}

// Line is one invoice line. UnitPrice is excluding VAT, in DKK. VATCode is
// always sent: without it Numina would use the sales account's default code.
type Line struct {
	Description string      `json:"description"`
	Quantity    int         `json:"quantity"`
	UnitPrice   json.Number `json:"unit_price"`
	VATCode     string      `json:"vat_code"`
}

// NewInvoice is a draft invoice request.
type NewInvoice struct {
	ContactID          string `json:"contact_id"`
	IssueDate          string `json:"issue_date"`
	DueDate            string `json:"due_date"`
	Currency           string `json:"currency"`
	SalesAccountNumber int    `json:"sales_account_number"`
	VATCode            string `json:"vat_code"`
	Items              []Line `json:"items"`
	Memo               string `json:"memo"`
}

// Invoice is the subset of a Numina invoice the portal reads.
type Invoice struct {
	ID                 string       `json:"id"`
	InvoiceNumber      *int         `json:"invoice_number"`
	Type               string       `json:"type"`
	Status             string       `json:"status"`
	ContactID          string       `json:"contact_id"`
	IssueDate          string       `json:"issue_date"`
	Currency           string       `json:"currency"`
	Total              json.Number  `json:"total"`
	Memo               string       `json:"memo"`
	PaymentState       *string      `json:"payment_state"`
	OutstandingBalance *json.Number `json:"outstanding_balance"`
}

// Settled is ADR 0030's funding rule: booked, `paid`, and nothing outstanding.
// Every other state, including covered_by_unmatched_payments, waits.
func (i Invoice) Settled() bool {
	if i.Status == "draft" || i.PaymentState == nil || *i.PaymentState != "paid" || i.OutstandingBalance == nil {
		return false
	}
	f, err := strconv.ParseFloat(i.OutstandingBalance.String(), 64)
	return err == nil && f == 0
}

// Minor parses a Numina decimal DKK amount into exact minor units. It accepts
// at most two decimals and never rounds.
func Minor(amount json.Number) (int64, bool) {
	whole, fraction, decimal := strings.Cut(amount.String(), ".")
	if whole == "" || len(fraction) > 2 || (decimal && fraction == "") || strings.HasPrefix(whole, "-") || strings.HasPrefix(whole, "+") {
		return 0, false
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || units > 1_000_000_000 {
		return 0, false
	}
	cents := int64(0)
	if fraction != "" {
		parsed, err := strconv.ParseInt((fraction + "0")[:2], 10, 64)
		if err != nil {
			return 0, false
		}
		cents = parsed
	}
	return units*100 + cents, true
}

// Amount formats minor units as Numina's decimal DKK amount.
func Amount(minor int64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", minor/100, minor%100))
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, idempotencyKey string, body, out any) error {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("numina: request failed")
	}
	defer func() {
		// Draining is best-effort; the decoded result or status is the verdict.
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return errors.New("numina: response unreadable")
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var problem struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &problem)
		return &Error{Status: resp.StatusCode, Code: problem.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("numina: response malformed")
	}
	return nil
}

// CreateContact creates a contact. Numina does not protect it against retries,
// so callers look for an existing contact first.
func (c *Client) CreateContact(ctx context.Context, contact Contact) (Contact, error) {
	var created Contact
	err := c.do(ctx, http.MethodPost, "/v1/contacts", nil, "", contact, &created)
	return created, err
}

// GetContact reads one contact.
func (c *Client) GetContact(ctx context.Context, id string) (Contact, error) {
	var contact Contact
	err := c.do(ctx, http.MethodGet, "/v1/contacts/"+url.PathEscape(id), nil, "", nil, &contact)
	return contact, err
}

// UpdateContact changes name and email; Numina keeps every field left out,
// so EAN, CVR and address details set in Numina survive.
func (c *Client) UpdateContact(ctx context.Context, id, name, email string) error {
	return c.do(ctx, http.MethodPut, "/v1/contacts/"+url.PathEscape(id), nil, "", map[string]string{"name": name, "email": email}, nil)
}

// ListContacts returns every contact of the company.
func (c *Client) ListContacts(ctx context.Context) ([]Contact, error) {
	var all []Contact
	for offset := 0; offset < 100000; {
		var page struct {
			Contacts   []Contact `json:"contacts"`
			HasMore    bool      `json:"has_more"`
			NextOffset int       `json:"next_offset"`
		}
		if err := c.do(ctx, http.MethodGet, "/v1/contacts", url.Values{"limit": {"500"}, "offset": {strconv.Itoa(offset)}}, "", nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Contacts...)
		if !page.HasMore || page.NextOffset <= offset {
			return all, nil
		}
		offset = page.NextOffset
	}
	return nil, errors.New("numina: contact list too long")
}

// CreateInvoice creates a draft. The key makes a retry return the original.
func (c *Client) CreateInvoice(ctx context.Context, key string, invoice NewInvoice) (Invoice, error) {
	var created Invoice
	err := c.do(ctx, http.MethodPost, "/v1/invoices", nil, key, invoice, &created)
	return created, err
}

// BookInvoice books a draft. Numina makes this safe to repeat.
func (c *Client) BookInvoice(ctx context.Context, id string) (Invoice, error) {
	var booked Invoice
	err := c.do(ctx, http.MethodPost, "/v1/invoices/"+url.PathEscape(id)+"/book", nil, "", struct{}{}, &booked)
	return booked, err
}

// SendInvoice emails a booked invoice to the contact's address.
func (c *Client) SendInvoice(ctx context.Context, key, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/invoices/"+url.PathEscape(id)+"/send", nil, key, struct{}{}, nil)
}

// GetInvoice reads one invoice, including its payment state.
func (c *Client) GetInvoice(ctx context.Context, id string) (Invoice, error) {
	var invoice Invoice
	err := c.do(ctx, http.MethodGet, "/v1/invoices/"+url.PathEscape(id), nil, "", nil, &invoice)
	return invoice, err
}

// ListInvoices returns every invoice to a contact issued in [from, to].
func (c *Client) ListInvoices(ctx context.Context, contactID, from, to string) ([]Invoice, error) {
	var all []Invoice
	for offset := 0; offset < 10000; {
		var page struct {
			Invoices   []Invoice `json:"invoices"`
			HasMore    bool      `json:"has_more"`
			NextOffset int       `json:"next_offset"`
		}
		q := url.Values{"contact_id": {contactID}, "from_date": {from}, "to_date": {to}, "limit": {"500"}, "offset": {strconv.Itoa(offset)}}
		if err := c.do(ctx, http.MethodGet, "/v1/invoices", q, "", nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Invoices...)
		if !page.HasMore || page.NextOffset <= offset {
			return all, nil
		}
		offset = page.NextOffset
	}
	return nil, errors.New("numina: invoice list too long")
}
