// Package identity owns the latent, invitation-only portal identity boundary.
// Its browser surface stays default-off until an injected OpenFeature provider
// enables it; production hosting and broker activation are separate gates.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"golang.org/x/oauth2"
)

// ErrDenied intentionally reveals no account, invitation or token details.
var ErrDenied = errors.New("portal access denied")

// Broker verifies one configured federated issuer, not individual providers.
type Broker struct {
	verifier         *oidc.IDTokenVerifier
	issuer, clientID string
	endpoint         oauth2.Endpoint
	client           *http.Client
}

// Principal cannot be populated by callers. Only a verified broker proof creates
// it; neither email nor token groups supplies membership or operator authority.
type Principal struct {
	issuer, subject, audience string
	expires                   time.Time
	// Browser sessions retain the exact server-owned membership that admitted
	// sign-in. Unbound broker/domain proofs have not become browser sessions.
	membershipIncarnation string
	preview               previewOperator
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
	endpoint := p.Endpoint()
	for _, address := range []string{endpoint.AuthURL, endpoint.TokenURL} {
		e, err := url.Parse(address)
		if err != nil || e.Scheme != "https" || e.Host != u.Host || e.User != nil || e.Fragment != "" {
			return nil, ErrDenied
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if supplied, ok := ctx.Value(oauth2.HTTPClient).(*http.Client); ok && supplied != nil {
		clientCopy := *supplied
		clientCopy.Timeout = 10 * time.Second
		client = &clientCopy
	}
	// Authorization codes, PKCE verifiers and client credentials may only be sent
	// to the configured token endpoint, never forwarded through a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Broker{verifier: p.Verifier(&oidc.Config{ClientID: clientID}), issuer: issuer, clientID: clientID, endpoint: endpoint, client: client}, nil
}

// Verify checks signature, issuer, audience and expiry with go-oidc, and binds the
// nonce to this login attempt. OIDC's multi-audience authorized party is checked
// explicitly; a valid token for a different relying party grants no access.
func (b *Broker) Verify(ctx context.Context, raw, nonce string) (Principal, error) {
	token, err := b.verifiedToken(ctx, raw, nonce)
	if err != nil || machineSubject(token.Subject) {
		return Principal{}, ErrDenied
	}
	return Principal{issuer: b.issuer, subject: token.Subject, audience: b.clientID, expires: token.Expiry}, nil
}

// verifiedToken retains the validated token for a purpose-specific claim consumer.
// Ordinary identity verification never depends on optional email claims.
func (b *Broker) verifiedToken(ctx context.Context, raw, nonce string) (*oidc.IDToken, error) {
	if b == nil || b.verifier == nil || nonce == "" || len(raw) > 16384 {
		return nil, ErrDenied
	}
	token, err := b.verifier.Verify(ctx, raw)
	if err != nil || token.Subject == "" || !time.Now().Before(token.Expiry) || subtle.ConstantTimeCompare([]byte(token.Nonce), []byte(nonce)) != 1 {
		return nil, ErrDenied
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if err = token.Claims(&claims); err != nil || (len(token.Audience) > 1 && claims.AuthorizedParty != b.clientID) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != b.clientID) {
		return nil, ErrDenied
	}
	return token, nil
}

// Foundation has no in-memory identity/entitlement authority. New defaults off.
type Foundation struct {
	pool   *pgxpool.Pool
	broker *Broker
	flags  *openfeature.Client
	cards  atomic.Pointer[CardBilling]
	// vatRate is the Stripe tax rate last confirmed as inclusive 25% VAT.
	vatRate atomic.Pointer[string]
	// mail is the site's mail sender for the portal's own emails.
	mail atomic.Pointer[mailer]
	// orderTerms is the immutable serving snapshot used by binding orders.
	orderTerms atomic.Pointer[agreementTerms]
	// site is the public website's origin, linked from a portal at the root of
	// its own host. A portal sharing the public host links to "/" instead.
	site string
	// endSession and endSessionClient name the upstream identity provider's
	// RP-initiated logout, which the broker cannot do itself. Both are public.
	endSession, endSessionClient string
	// operators lists the customer Dex subjects eligible for the operator role.
	operators map[string]bool
	// operatorOnly refuses external customer identities in the private preview.
	operatorOnly bool
	// customers is the optional organization-scoped provider for operator actions.
	customers CustomerDirectory
	// agents is immutable policy and a distinct verifier, installed at startup.
	agents *agentAccess
}

// LinkUpstreamLogout names the upstream provider's end_session endpoint and the
// broker's client there, so sign-out and a refused sign-in also end the upstream
// session. Without it, the portal only ends its own session.
func (s *Foundation) LinkUpstreamLogout(endpoint, client string) error {
	u, err := url.Parse(endpoint)
	if s == nil || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.String() != endpoint ||
		client == "" || len(client) > 256 || strings.ContainsAny(client, " \t\r\n") {
		return ErrDenied
	}
	s.endSession, s.endSessionClient = endpoint, client
	return nil
}

// operatorWorkspace is the workspace every configured operator's membership names.
const operatorWorkspace = "devantler-tech"

// maxOperators bounds the server-side operator list.
const maxOperators = 10

// LinkOperators pins exact customer Dex subjects. ZITADEL issues the same
// subject for every sign-in method linked to the same account. The platform
// connector is also admitted during the transition from the old operator login.
func (s *Foundation) LinkOperators(subjects []string) error {
	if s == nil || len(subjects) == 0 || len(subjects) > maxOperators {
		return ErrDenied
	}
	operators := make(map[string]bool, len(subjects))
	for _, subject := range subjects {
		connector := customeridentity.DexConnector(subject)
		if (connector != customeridentity.ZitadelConnectorID && connector != customeridentity.PlatformConnectorID) || operators[subject] {
			return ErrDenied
		}
		operators[subject] = true
	}
	s.operators = operators
	return nil
}

// LinkPreviewOperator admits one shared sign-in subject and disables customer
// invitations. Like LinkOperators, it must run before serving any requests.
func (s *Foundation) LinkPreviewOperator(subject string) error {
	if customeridentity.DexConnector(subject) != customeridentity.ZitadelConnectorID {
		return ErrDenied
	}
	if err := s.LinkOperators([]string{subject}); err != nil {
		return err
	}
	s.operatorOnly = true
	return nil
}

// OperatorsLinked reports whether any operator subject is configured.
func (s *Foundation) OperatorsLinked() bool {
	return s != nil && len(s.operators) > 0
}

// legacyOperatorSignIn preserves the existing login until the deployment has a
// verified operator account on ZITADEL. From that point the shared page is used.
func (s *Foundation) legacyOperatorSignIn() bool {
	for subject := range s.operators {
		if customeridentity.DexConnector(subject) == customeridentity.ZitadelConnectorID {
			return false
		}
	}
	return s.OperatorsLinked()
}

// platformIdentity reports whether customer Dex issued subject through the
// legacy platform connector, which only operators use.
func platformIdentity(subject string) bool {
	return customeridentity.DexConnector(subject) == customeridentity.PlatformConnectorID
}

// canInvite keeps all configured operators and the legacy platform connector
// out of customer invitations. The customer Dex cannot invite an operator.
func (s *Foundation) canInvite(subject, role string) bool {
	connector := customeridentity.DexConnector(subject)
	return !s.operatorOnly && !s.operators[subject] && connector != customeridentity.PlatformConnectorID && (connector == "" || role != "operator")
}

// enrollOperator gives a listed operator its operator membership on first sign-in.
// An existing membership, current or revoked, is left as it is, so revoking an
// operator holds even while it stays listed.
func (s *Foundation) enrollOperator(ctx context.Context, p Principal) error {
	if !s.operators[p.subject] {
		return ErrDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin operator: %w", err)
	}
	defer func() {
		// Commit decides success; a committed transaction is already closed.
		_ = tx.Rollback(ctx)
	}()
	if err = lockIdentity(ctx, tx, p.issuer, p.subject); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,$3,'operator') ON CONFLICT(issuer,subject) DO NOTHING`, p.issuer, p.subject, operatorWorkspace); err != nil {
		return fmt.Errorf("create operator: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit operator: %w", err)
	}
	return nil
}

// New accepts the application's OpenFeature client; every operation defaults off.
// It does not discover, migrate, start a server, or enable any public endpoint.
func New(pool *pgxpool.Pool, broker *Broker, flags *openfeature.Client) *Foundation {
	return &Foundation{pool: pool, broker: broker, flags: flags}
}

func (s *Foundation) ready(ctx context.Context) bool {
	if s == nil || s.pool == nil || s.broker == nil || s.broker.verifier == nil || s.flags == nil || s.flags.State() != openfeature.ReadyState {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-identity", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.flags.State() == openfeature.ReadyState
}

// Attempt secrets are for the trusted browser handler, never logs, public
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
	// Opening the portal starts an attempt, so each one also removes a bounded
	// batch of expired attempts; abandoned ones cannot accumulate.
	_, err := s.pool.Exec(ctx, `WITH expired AS (SELECT state_digest FROM portal_login_attempts WHERE expires_at<now() LIMIT 50)
 DELETE FROM portal_login_attempts a USING expired WHERE a.state_digest=expired.state_digest`)
	if err != nil {
		return Attempt{}, fmt.Errorf("remove expired login attempts: %w", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO portal_login_attempts(state_digest,browser_digest,nonce,pkce_verifier,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, digest(a.State), digest(a.BrowserBinding), a.Nonce, a.PKCEVerifier)
	if err != nil {
		return Attempt{}, fmt.Errorf("persist login attempt: %w", err)
	}
	return a, nil
}

// Invite is a CONTROL-PLANE provisioning operation, not a client-facing handler.
// Its caller must be a trusted provisioning process. The operator API uses the
// caller-bound private variant after its authentication, authorization and CSRF checks.
// The invitation is bound to one exact issuer/subject; that identity accepts it
// by signing in before it expires. Nothing secret is handed to the customer: the
// row key is a random digest that is never shown.
// Operators are never invited: they come from the server configuration.
func (s *Foundation) Invite(ctx context.Context, i Invitation) error {
	return s.invite(ctx, i, nil)
}

// invite retains the control-plane rules and permits the operator API to bind
// its final commit to the original caller after any provider or SDK wait.
func (s *Foundation) invite(ctx context.Context, i Invitation, allowed func(pgx.Tx) bool) error {
	if i.ClientID == previewWorkspace || i.Subject == previewSubject {
		return ErrDenied
	}
	if !s.ready(ctx) || i.Issuer != s.broker.issuer || strings.TrimSpace(i.Subject) == "" || !s.canInvite(i.Subject, i.Role) || strings.TrimSpace(i.ClientID) == "" || (i.Role != "client" && i.Role != "operator") || !time.Now().Before(i.Expires) || i.Expires.After(time.Now().Add(7*24*time.Hour)) {
		return ErrDenied
	}
	// One identity has at most one pending invitation and none once it has any
	// membership, current or revoked, so sign-in never has to choose between them.
	// The identity's lock serializes concurrent invitations and first sign-ins.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin invitation: %w", err)
	}
	defer func() {
		// Commit decides success; a committed transaction is already closed.
		_ = tx.Rollback(ctx)
	}()
	if err = lockIdentity(ctx, tx, i.Issuer, i.Subject); err != nil {
		return err
	}
	if err = lockWorkspace(ctx, tx, i.ClientID); err != nil {
		return err
	}
	open, err := workspaceOpen(ctx, tx, i.ClientID)
	if err != nil {
		return err
	}
	if !open {
		return ErrDenied
	}
	result, err := tx.Exec(ctx, `INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at)
 SELECT $1,$2,$3,$4,$5,$6
 WHERE NOT EXISTS (SELECT 1 FROM portal_invitations WHERE issuer=$2 AND subject=$3 AND NOT revoked AND expires_at>now())
 AND NOT EXISTS (SELECT 1 FROM portal_memberships WHERE issuer=$2 AND subject=$3)`, digest(secret()), i.Issuer, i.Subject, i.ClientID, i.Role, i.Expires)
	if err != nil {
		return fmt.Errorf("persist invitation: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrDenied
	}
	if allowed != nil && !allowed(tx) {
		return ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit invitation: %w", err)
	}
	return nil
}

// identityLock namespaces the per-identity invitation lock among the portal locks.
const identityLock = 1869820524

// lockIdentity takes this transaction's lock on one issuer/subject. Neither
// contains a space, so the joined key is unambiguous.
func lockIdentity(ctx context.Context, tx pgx.Tx, issuer, subject string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int,hashtext($2 || ' ' || $3))`, identityLock, issuer, subject); err != nil {
		return fmt.Errorf("lock identity: %w", err)
	}
	return nil
}

// Complete is a server-side identity boundary, NOT a browser callback endpoint.
// raw must come from the future handler's verified code exchange, never directly
// from an untrusted request. That handler still owes PKCE, redirect and CSRF proof.
// Consumption happens in PostgreSQL, so another process cannot replay the attempt.
func (s *Foundation) Complete(ctx context.Context, state, browser, raw string) (Principal, error) {
	nonce, _, err := s.consumeAttempt(ctx, state, browser)
	if err != nil {
		return Principal{}, err
	}
	return s.completeProof(ctx, nonce, raw)
}

// consumeAttempt is the sole atomic consumer, before any browser code exchange.
// Request-local deletion alone cannot stop another process replaying a callback.
func (s *Foundation) consumeAttempt(ctx context.Context, state, browser string) (string, string, error) {
	if !s.ready(ctx) || state == "" || browser == "" {
		return "", "", ErrDenied
	}
	var nonce, verifier string
	err := s.pool.QueryRow(ctx, `DELETE FROM portal_login_attempts WHERE state_digest=$1 AND browser_digest=$2 AND expires_at>now() AND purpose='sign-in' RETURNING nonce,pkce_verifier`, digest(state), digest(browser)).Scan(&nonce, &verifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrDenied
	}
	if err != nil {
		return "", "", fmt.Errorf("consume login attempt: %w", err)
	}
	return nonce, verifier, nil
}

func (s *Foundation) completeProof(ctx context.Context, nonce, raw string) (Principal, error) {
	if !s.ready(ctx) {
		return Principal{}, ErrDenied
	}
	p, err := s.broker.Verify(ctx, raw, nonce)
	if err != nil {
		return Principal{}, ErrDenied
	}
	// Listed operators enroll directly. Private preview and legacy platform
	// sign-ins use this branch exclusively, with no invitation fallback.
	if s.operatorOnly || s.operators[p.subject] || platformIdentity(p.subject) {
		if err = s.enrollOperator(ctx, p); err != nil {
			return Principal{}, err
		}
		if _, _, p.membershipIncarnation, err = s.membershipState(ctx, p); err != nil {
			return Principal{}, err
		}
		return p, nil
	}
	// A current member signs straight in. An identity without membership accepts
	// its own pending invitation, which was bound to this exact issuer/subject
	// when it was created; anyone else has nothing to accept and is refused.
	if _, _, p.membershipIncarnation, err = s.membershipState(ctx, p); errors.Is(err, ErrDenied) {
		if err = s.redeem(ctx, p); err != nil && !errors.Is(err, ErrDenied) {
			return Principal{}, err
		}
		// Current membership decides, including one a concurrent first sign-in of
		// the same identity has just created; a revoked membership still refuses.
		_, _, p.membershipIncarnation, err = s.membershipState(ctx, p)
	}
	if err != nil {
		return Principal{}, err
	}
	return p, nil
}

// redeem consumes the one pending invitation bound to this exact identity and
// creates its membership in the same transaction. None, or more than one (which
// Invite prevents), is refused rather than chosen between; a refusal rolls back.
// Concurrent first sign-ins serialize on the deleted rows, so only one redeems.
func (s *Foundation) redeem(ctx context.Context, p Principal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin invitation: %w", err)
	}
	defer func() {
		// Commit decides success. A committed transaction is already closed;
		// pgx discards a failed rollback's connection and returns it to the pool.
		_ = tx.Rollback(ctx)
	}()
	if err = lockIdentity(ctx, tx, p.issuer, p.subject); err != nil {
		return err
	}
	var workspace string
	var invitations int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(min(client_id),''),count(*) FROM portal_invitations
 WHERE issuer=$1 AND subject=$2 AND NOT revoked AND expires_at>now()`, p.issuer, p.subject).Scan(&workspace, &invitations); err != nil {
		return fmt.Errorf("read pending invitation: %w", err)
	}
	if invitations != 1 {
		return ErrDenied
	}
	if err = lockWorkspace(ctx, tx, workspace); err != nil {
		return err
	}
	open, err := workspaceOpen(ctx, tx, workspace)
	if err != nil {
		return err
	}
	if !open {
		return ErrDenied
	}
	rows, err := tx.Query(ctx, `DELETE FROM portal_invitations WHERE issuer=$1 AND subject=$2 AND NOT revoked AND expires_at>now() AND client_id=$3 RETURNING client_id,role`, p.issuer, p.subject, workspace)
	if err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}
	var client, role string
	pending := 0
	for rows.Next() {
		if err = rows.Scan(&client, &role); err != nil {
			rows.Close()
			return fmt.Errorf("consume invitation: %w", err)
		}
		pending++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return fmt.Errorf("consume invitation: %w", err)
	}
	if pending != 1 || !s.canInvite(p.subject, role) {
		return ErrDenied
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
	client, role, _, err := s.membershipState(ctx, p)
	return client, role, err
}

// membershipState is the single membership policy for trusted proofs and bound
// browser principals. A bound principal never adopts a replacement membership.
func (s *Foundation) membershipState(ctx context.Context, p Principal) (string, string, string, error) {
	if !s.ready(ctx) || p.subject == "" || p.issuer != s.broker.issuer || p.audience != s.broker.clientID || !time.Now().Before(p.expires) {
		return "", "", "", ErrDenied
	}
	if p.isClientPreview() {
		return s.previewClient(ctx, p)
	}
	// Legacy platform identities can only be operators.
	connector := customeridentity.DexConnector(p.subject)
	listed := s.operators[p.subject]
	if (s.operatorOnly || connector == customeridentity.PlatformConnectorID) && !listed {
		return "", "", "", ErrDenied
	}
	var own, role, incarnation string
	err := s.pool.QueryRow(ctx, `SELECT client_id,role,incarnation::text FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND NOT revoked AND ($3='' OR incarnation::text=$3)`, p.issuer, p.subject, p.membershipIncarnation).Scan(&own, &role, &incarnation)
	// SQL completion is not authority to release expired or cancelled reads.
	if ctx.Err() != nil || !time.Now().Before(p.expires) {
		return "", "", "", ErrDenied
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", ErrDenied
	}
	if err != nil {
		return "", "", "", fmt.Errorf("read membership: %w", err)
	}
	// A customer Dex operator must hold both the configured grant and the
	// current operator membership. A grant does not promote an existing client.
	if connector != "" && listed != (role == "operator") {
		return "", "", "", ErrDenied
	}
	return own, role, incarnation, nil
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
