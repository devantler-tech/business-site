// Package zitadel supplies the portal's organization-scoped customer directory.
package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ErrUnavailable omits provider responses, credentials and customer details.
var ErrUnavailable = errors.New("customer directory is unavailable")

// Client is the customer organization's bounded ZITADEL client.
type Client struct {
	base, org string
	token     func() (string, error)
	http      *http.Client
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,200}$`)

// New constructs a client without reading a credential or contacting ZITADEL.
func New(base, org string, token func() (string, error), client *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || !identifier.MatchString(org) || token == nil {
		return nil, ErrUnavailable
	}
	if client == nil {
		client = http.DefaultClient
	}
	bounded := *client
	bounded.Timeout = 10 * time.Second
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{strings.TrimSuffix(base, "/"), org, token, &bounded}, nil
}

type user struct {
	ID      string `json:"userId"`
	State   string `json:"state"`
	Details struct {
		Owner string `json:"resourceOwner"`
	} `json:"details"`
	Human *struct {
		Email struct {
			Address string `json:"email"`
		} `json:"email"`
	} `json:"human"`
}

// call never follows redirects or returns a provider's diagnostic body. The
// projected credential is read for each call so rotation needs no process restart.
func (c *Client) call(ctx context.Context, method, path string, body, result any, allowed func() bool) (int, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, ErrUnavailable
	}
	token, err := c.token()
	if err != nil || token == "" || len(token) > 16384 || strings.ContainsAny(token, " \t\r\n") {
		return 0, ErrUnavailable
	}
	if ctx.Err() != nil || (allowed != nil && !allowed()) {
		return 0, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(encoded))
	if err != nil {
		return 0, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-zitadel-orgid", c.org)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer func() {
		// The response is read-only and bounded; close cannot change its admission.
		_ = res.Body.Close()
	}()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(data) > 65536 || json.Unmarshal(data, result) != nil {
		return res.StatusCode, ErrUnavailable
	}
	return res.StatusCode, nil
}

func (c *Client) find(ctx context.Context, email string) (string, error) {
	query := map[string]any{"query": map[string]string{"limit": "2"}, "queries": []any{
		map[string]any{"organizationIdQuery": map[string]string{"organizationId": c.org}},
		map[string]any{"emailQuery": map[string]string{"emailAddress": email, "method": "TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE"}},
	}}
	var found struct {
		Result []user `json:"result"`
	}
	if _, err := c.call(ctx, "POST", "/v2/users", query, &found, nil); err != nil {
		return "", err
	}
	if len(found.Result) == 0 {
		return "", nil
	}
	if len(found.Result) != 1 {
		return "", ErrUnavailable
	}
	u := found.Result[0]
	if !identifier.MatchString(u.ID) || u.Details.Owner != c.org || u.Human == nil || !strings.EqualFold(u.Human.Email.Address, email) {
		return "", ErrUnavailable
	}
	return u.ID, nil
}

// EnsureUser creates or reuses one human in the configured organization.
func (c *Client) EnsureUser(ctx context.Context, name, email string, allowed func() bool) (string, error) {
	address, err := mail.ParseAddress(email)
	parts := strings.Fields(name)
	if allowed == nil || err != nil || address.Address != email || address.Name != "" || len(email) > 254 || len(parts) < 2 || len(name) > 200 || strings.ContainsAny(name, "\r\n\x00") {
		return "", ErrUnavailable
	}
	id, err := c.find(ctx, email)
	if err != nil || id != "" {
		return id, err
	}
	body := map[string]any{"userName": email, "profile": map[string]string{"firstName": parts[0], "lastName": strings.Join(parts[1:], " "), "displayName": name}, "email": map[string]any{"email": email, "isEmailVerified": false}}
	var created struct {
		ID      string `json:"userId"`
		Details struct {
			Owner string `json:"resourceOwner"`
		} `json:"details"`
	}
	status, err := c.call(ctx, "POST", "/management/v1/users/human", body, &created, allowed)
	if status == http.StatusConflict {
		return c.findExisting(ctx, email)
	}
	if err != nil || !identifier.MatchString(created.ID) || created.Details.Owner != c.org {
		return "", ErrUnavailable
	}
	return created.ID, nil
}

func (c *Client) findExisting(ctx context.Context, email string) (string, error) {
	id, err := c.find(ctx, email)
	if err != nil || id == "" {
		return "", ErrUnavailable
	}
	return id, nil
}

func (c *Client) state(ctx context.Context, id, action string, allowed func() bool) error {
	if allowed == nil || !identifier.MatchString(id) {
		return ErrUnavailable
	}
	var existing struct {
		User user `json:"user"`
	}
	if _, err := c.call(ctx, "GET", "/v2/users/"+id, nil, &existing, nil); err != nil || existing.User.ID != id || existing.User.Details.Owner != c.org || existing.User.Human == nil {
		return ErrUnavailable
	}
	state := existing.User.State
	if state != "USER_STATE_ACTIVE" && state != "USER_STATE_INACTIVE" {
		return ErrUnavailable
	}
	if (action == "deactivate" && state == "USER_STATE_INACTIVE") || (action == "reactivate" && state == "USER_STATE_ACTIVE") {
		return nil
	}
	var changed map[string]json.RawMessage
	_, err := c.call(ctx, "POST", "/management/v1/users/"+id+"/_"+action, struct{}{}, &changed, allowed)
	return err
}

// DeactivateUser disables a customer without deleting any data.
func (c *Client) DeactivateUser(ctx context.Context, id string, allowed func() bool) error {
	return c.state(ctx, id, "deactivate", allowed)
}

// ReactivateUser re-enables a previously disabled customer.
func (c *Client) ReactivateUser(ctx context.Context, id string, allowed func() bool) error {
	return c.state(ctx, id, "reactivate", allowed)
}
