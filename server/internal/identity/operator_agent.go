package identity

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/open-feature/go-sdk/openfeature"
)

// VerifyMachine verifies a non-interactive proof without creating a session.
func (b *Broker) VerifyMachine(ctx context.Context, raw string) (Principal, error) {
	if b == nil || b.verifier == nil || raw == "" || len(raw) > 16384 || ctx.Err() != nil {
		return Principal{}, ErrDenied
	}
	token, err := b.verifier.Verify(ctx, raw)
	if err != nil || token.Issuer != b.issuer || len(token.Audience) != 1 || token.Audience[0] != b.clientID || !machineSubject(token.Subject) || token.Nonce != "" {
		return Principal{}, ErrDenied
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
		NotBefore       int64  `json:"nbf"`
	}
	now := time.Now()
	if token.Claims(&claims) != nil || (claims.AuthorizedParty != "" && claims.AuthorizedParty != b.clientID) || token.IssuedAt.IsZero() || token.IssuedAt.Unix() <= 0 || token.IssuedAt.After(now.Add(30*time.Second)) || !token.Expiry.After(token.IssuedAt) || token.Expiry.Sub(token.IssuedAt) > 5*time.Minute || !now.Before(token.Expiry) || claims.NotBefore > now.Unix() || ctx.Err() != nil {
		return Principal{}, ErrDenied
	}
	return Principal{b.issuer, token.Subject, b.clientID, token.Expiry}, nil
}

// agentActions is a closed catalogue, never a prefix or HTTP-method rule. Adding
// an operation deliberately decides whether it is a read or an exact write grant.
// Money movement and deletion have no grantable entry and cannot be configured.
var agentActions = map[string]bool{
	"invitations.list": false, "workspaces.list": false, "workspaces.members.list": false,
	"audit.list": false, "openapi.read": false,
	"invitations.create": true, "invitations.revoke": true, "workspaces.create": true,
	"members.deactivate": true, "members.reactivate": true,
}

type agentSubject struct {
	Issuer  string   `json:"issuer"`
	Subject string   `json:"subject"`
	Grants  []string `json:"grants"`
}

// UnmarshalJSON refuses duplicate properties as well as unknown ones: a policy
// must have one meaning before it is installed, regardless of JSON consumer.
func (a *agentSubject) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return ErrDenied
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return ErrDenied
		}
		seen[name] = true
		switch name {
		case "issuer":
			err = decoder.Decode(&a.Issuer)
		case "subject":
			err = decoder.Decode(&a.Subject)
		case "grants":
			err = decoder.Decode(&a.Grants)
		default:
			return ErrDenied
		}
		if err != nil {
			return ErrDenied
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return ErrDenied
	}
	return nil
}

type agentAccess struct {
	broker   *Broker
	subjects map[string]map[string]bool
}

// LinkAgents installs only public policy and a verifier. Platform administration
// provisions credentials separately; this application never reads a machine secret.
// Like LinkOperators, this is startup composition and must precede serving requests.
func (s *Foundation) LinkAgents(ctx context.Context, audience, raw string) error {
	if s == nil || s.broker == nil || s.agents != nil {
		return ErrDenied
	}
	subjects, err := parseAgents(s.broker.issuer, s.broker.clientID, audience, raw)
	if err != nil || len(subjects) == 0 {
		return ErrDenied
	}
	broker, err := NewBroker(oidc.ClientContext(ctx, s.broker.client), s.broker.issuer, audience)
	if err != nil {
		return ErrDenied
	}
	s.agents = &agentAccess{broker, subjects}
	return nil
}

func (s *Foundation) agentAllowed(ctx context.Context, p Principal, action string) bool {
	if s == nil || s.agents == nil || s.flags == nil || s.flags.State() != openfeature.ReadyState || ctx.Err() != nil || p.issuer != s.agents.broker.issuer || p.audience != s.agents.broker.clientID || !time.Now().Before(p.expires) {
		return false
	}
	grants, listed := s.agents.subjects[p.subject]
	write, known := agentActions[action]
	return listed && known && (!write || grants[action])
}

type operatorAuthorityKey struct{}

// operatorAuthority belongs to one call, never to a session or caller-supplied
// field. The mutation callback checks it again using its own held transaction.
type operatorAuthority struct {
	principal                    Principal
	kind, action, client, target string
	current                      func(context.Context, privateResponseQuery) bool
	mutating                     bool
}

// operatorMutationAllowed re-evaluates the SDK before checking the exact original
// authority. The supplied transaction avoids another pool acquisition while a
// mutation holds identity locks. Call immediately before any effect or commit.
func (b *browser) operatorMutationAllowed(ctx context.Context, query privateResponseQuery) bool {
	bound, ok := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority)
	return ok && bound.current != nil && b.foundation.ready(ctx) && bound.current(ctx, query)
}

// setOperatorTarget records a validated server-selected identifier in this call's
// single audit row. Request fields can never set the actor, kind or action grant.
func setOperatorTarget(ctx context.Context, target string) {
	if bound, ok := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority); ok {
		bound.target = target
	}
}

// agentAuthorization refuses ambiguous headers and any browser-session input.
// A failed bearer never falls back to a signed-in human's broader authority.
func (b *browser) agentAuthorization(r *http.Request, action string) (*operatorAuthority, int) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || b.foundation.agents == nil {
		return nil, http.StatusUnauthorized
	}
	scheme, raw, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || raw == "" || strings.ContainsAny(raw, " \t\r\n,") {
		return nil, http.StatusUnauthorized
	}
	if _, err := r.Cookie("__Host-portal_session"); err == nil {
		return nil, http.StatusUnauthorized
	}
	p, err := b.foundation.agents.broker.VerifyMachine(r.Context(), raw)
	if err != nil {
		return nil, http.StatusUnauthorized
	}
	bound := &operatorAuthority{principal: p, kind: "agent", action: action}
	bound.current = func(ctx context.Context, _ privateResponseQuery) bool {
		return b.foundation.agentAllowed(ctx, p, action)
	}
	if !b.foundation.agentAllowed(r.Context(), p, action) {
		return bound, http.StatusForbidden
	}
	return bound, http.StatusOK
}

// ValidateAgents checks public machine policy before startup opens dependencies.
// An absent pair leaves machine access closed. Only this broker's machine
// subjects are accepted, with an audience distinct from its browser client.
func ValidateAgents(issuer, browserAudience, audience, raw string) error {
	_, err := parseAgents(issuer, browserAudience, audience, raw)
	return err
}

func parseAgents(issuer, browserAudience, audience, raw string) (map[string]map[string]bool, error) {
	if audience == "" && raw == "" {
		return nil, nil
	}
	if issuer == "" || audience == "" || audience == browserAudience || len(audience) > 256 || strings.ContainsAny(audience, " \t\r\n") || len(raw) == 0 || len(raw) > 8192 {
		return nil, ErrDenied
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var entries []agentSubject
	if decoder.Decode(&entries) != nil || decoder.Decode(new(any)) != io.EOF || len(entries) == 0 || len(entries) > 10 {
		return nil, ErrDenied
	}
	listed := make(map[string]map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Issuer != issuer || !machineSubject(entry.Subject) || listed[entry.Subject] != nil || len(entry.Grants) > len(agentActions) {
			return nil, ErrDenied
		}
		grants := make(map[string]bool, len(entry.Grants))
		for _, action := range entry.Grants {
			if !agentActions[action] || grants[action] {
				return nil, ErrDenied
			}
			grants[action] = true
		}
		listed[entry.Subject] = grants
	}
	return listed, nil
}

// machineSubject recognizes Dex's canonical client-credentials subject: a
// non-empty client ID and no connector. Browser identities always have a connector.
func machineSubject(subject string) bool {
	if len(subject) > 512 {
		return false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(subject)
	if err != nil || len(raw) < 3 || raw[0] != 0x0a {
		return false
	}
	length, read := binary.Uvarint(raw[1:])
	if read <= 0 || length == 0 || length != uint64(len(raw)-1-read) {
		return false
	}
	client := string(raw[1+read:])
	return customeridentity.DexSubject(client, "") == subject
}
