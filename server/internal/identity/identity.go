// Package identity owns the latent, invitation-only portal identity boundary.
// It registers no HTTP routes. Browser code exchange, sessions and CSRF remain
// prerequisites for wiring it into an application.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
)

// ErrDenied intentionally reveals no account, invitation or token details.
var ErrDenied = errors.New("portal access denied")

// Broker verifies one configured federated issuer, not individual providers.
type Broker struct {
	verifier         *oidc.IDTokenVerifier
	issuer, clientID string
}

// Principal cannot be populated by callers. Only a verified broker proof creates
// it; neither email nor token groups supplies membership or operator authority.
type Principal struct {
	issuer, subject, audience string
	expires                   time.Time
}

// NewBroker discovers the configured HTTPS issuer through maintained OIDC code.
// The caller supplies a bounded context and an HTTP client with timeouts.
func NewBroker(ctx context.Context, issuer, clientID string) (*Broker, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimSpace(clientID) == "" {
		return nil, ErrDenied
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover broker: %w", err)
	}
	return &Broker{p.Verifier(&oidc.Config{ClientID: clientID}), issuer, clientID}, nil
}

// Verify checks signature, issuer, audience and expiry with go-oidc, and binds the
// nonce to this login attempt. OIDC's multi-audience authorized party is checked
// explicitly; a valid token for a different relying party grants no access.
func (b *Broker) Verify(ctx context.Context, raw, nonce string) (Principal, error) {
	if b == nil || b.verifier == nil || nonce == "" || len(raw) > 16384 {
		return Principal{}, ErrDenied
	}
	token, err := b.verifier.Verify(ctx, raw)
	if err != nil || token.Subject == "" || !time.Now().Before(token.Expiry) || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 {
		return Principal{}, ErrDenied
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err = token.Claims(&claims); err != nil || (len(token.Audience) > 1 && claims.AuthorizedParty != b.clientID) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != b.clientID) {
		return Principal{}, ErrDenied
	}
	return Principal{b.issuer, token.Subject, b.clientID, token.Expiry}, nil
}

// Foundation has no in-memory identity/entitlement authority. New defaults off.
type Foundation struct {
	pool   *pgxpool.Pool
	broker *Broker
	flags  *openfeature.Client
}

// New accepts the application's OpenFeature client; every operation defaults off.
// It does not discover, migrate, start a server, or enable any public endpoint.
func New(pool *pgxpool.Pool, broker *Broker, flags *openfeature.Client) *Foundation {
	return &Foundation{pool, broker, flags}
}

func (s *Foundation) ready(ctx context.Context) bool {
	if s == nil || s.pool == nil || s.broker == nil || s.broker.verifier == nil || s.flags == nil {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-identity", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

// Attempt secrets are for the trusted future browser handler, never logs, public
// assets or a browser-selected role. State/binding are stored only as digests.
type Attempt struct{ State, BrowserBinding, Nonce, PKCEVerifier string }

// Invitation binds one broker subject to one client and an explicit server role.
type Invitation struct {
	Issuer, Subject, ClientID, Role string
	Expires                         time.Time
}

func secret() string         { return rand.Text() }
func digest(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

// Begin durably creates a ten-minute login attempt with independent secrets.
func (s *Foundation) Begin(ctx context.Context) (Attempt, error) {
	if !s.ready(ctx) {
		return Attempt{}, ErrDenied
	}
	a := Attempt{secret(), secret(), secret(), secret() + secret()}
	_, err := s.pool.Exec(ctx, `INSERT INTO portal_login_attempts(state_digest,browser_digest,nonce,pkce_verifier,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, digest(a.State), digest(a.BrowserBinding), a.Nonce, a.PKCEVerifier)
	if err != nil {
		return Attempt{}, fmt.Errorf("persist login attempt: %w", err)
	}
	return a, nil
}

// Invite is a CONTROL-PLANE provisioning operation, not a client-facing handler.
// Its caller must be a trusted provisioning process. A future network invitation
// API requires independent operator authentication, authorization and CSRF checks.
func (s *Foundation) Invite(ctx context.Context, i Invitation) (string, error) {
	if !s.ready(ctx) || i.Issuer != s.broker.issuer || strings.TrimSpace(i.Subject) == "" || strings.TrimSpace(i.ClientID) == "" || (i.Role != "client" && i.Role != "operator") || !time.Now().Before(i.Expires) || i.Expires.After(time.Now().Add(7*24*time.Hour)) {
		return "", ErrDenied
	}
	token := secret()
	_, err := s.pool.Exec(ctx, `INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, digest(token), i.Issuer, i.Subject, i.ClientID, i.Role, i.Expires)
	if err != nil {
		return "", fmt.Errorf("persist invitation: %w", err)
	}
	return token, nil
}

// Complete is a server-side identity boundary, NOT a browser callback endpoint.
// raw must come from the future handler's verified code exchange, never directly
// from an untrusted request. That handler still owes PKCE, redirect and CSRF proof.
// Consumption happens in PostgreSQL, so another process cannot replay the attempt.
func (s *Foundation) Complete(ctx context.Context, state, browser, raw, invitation string) (Principal, error) {
	if !s.ready(ctx) || state == "" || browser == "" {
		return Principal{}, ErrDenied
	}
	var nonce string
	err := s.pool.QueryRow(ctx, `DELETE FROM portal_login_attempts WHERE state_digest=$1 AND browser_digest=$2 AND expires_at>now() RETURNING nonce`, digest(state), digest(browser)).Scan(&nonce)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrDenied
	}
	if err != nil {
		return Principal{}, fmt.Errorf("consume login attempt: %w", err)
	}
	p, err := s.broker.Verify(ctx, raw, nonce)
	if err != nil {
		return Principal{}, ErrDenied
	}
	if invitation != "" {
		if err = s.redeem(ctx, p, invitation); err != nil {
			return Principal{}, err
		}
	}
	if _, _, err = s.membership(ctx, p); err != nil {
		return Principal{}, err
	}
	return p, nil
}

func (s *Foundation) redeem(ctx context.Context, p Principal, token string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin invitation: %w", err)
	}
	defer tx.Rollback(ctx)
	var client, role string
	err = tx.QueryRow(ctx, `DELETE FROM portal_invitations WHERE token_digest=$1 AND issuer=$2 AND subject=$3 AND NOT revoked AND expires_at>now() RETURNING client_id,role`, digest(token), p.issuer, p.subject).Scan(&client, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}
	// An existing identity cannot gain a new client or revive revoked membership.
	// No email matching, implicit account linking or entitlement transfer occurs.
	result, err := tx.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,$3,$4) ON CONFLICT(issuer,subject) DO NOTHING`, p.issuer, p.subject, client, role)
	if err != nil {
		return fmt.Errorf("create membership: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit invitation: %w", err)
	}
	return nil
}

// Authorize re-reads CURRENT durable membership on every call. Handlers must pass
// their server-owned record's nonempty client ID. operator=true demands an
// explicitly provisioned operator; groups or browser role fields grant nothing.
func (s *Foundation) Authorize(ctx context.Context, p Principal, client string, operator bool) error {
	if client == "" {
		return ErrDenied
	}
	own, role, err := s.membership(ctx, p)
	if err != nil {
		return err
	}
	if operator && role != "operator" {
		return ErrDenied
	}
	if own != client && role != "operator" {
		return ErrDenied
	}
	return nil
}

func (s *Foundation) membership(ctx context.Context, p Principal) (string, string, error) {
	if !s.ready(ctx) || p.subject == "" || p.issuer != s.broker.issuer || p.audience != s.broker.clientID || !time.Now().Before(p.expires) {
		return "", "", ErrDenied
	}
	var own, role string
	err := s.pool.QueryRow(ctx, `SELECT client_id,role FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND NOT revoked`, p.issuer, p.subject).Scan(&own, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrDenied
	}
	if err != nil {
		return "", "", fmt.Errorf("read membership: %w", err)
	}
	return own, role, nil
}

// Revoke is a trusted control-plane operation, subject to the same caller
// restriction as Invite. It does not expose an HTTP revocation API.
func (s *Foundation) Revoke(ctx context.Context, issuer, subject string) error {
	if !s.ready(ctx) {
		return ErrDenied
	}
	_, err := s.pool.Exec(ctx, `UPDATE portal_memberships SET revoked=true WHERE issuer=$1 AND subject=$2`, issuer, subject)
	if err != nil {
		return fmt.Errorf("revoke membership: %w", err)
	}
	return nil
}
