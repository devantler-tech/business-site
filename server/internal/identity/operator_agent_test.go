package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// A client-credentials subject must never establish a browser session, even if
// a broker can mint it with the browser audience and the login attempt's nonce.
func TestBrokerRefusesMachineIdentityForBrowser(t *testing.T) {
	f := fixtureBroker(t)
	if _, err := f.broker.Verify(f.ctx, f.token(t, customeridentity.DexSubject("machine-fixture", ""), "nonce", nil), "nonce"); err == nil {
		t.Fatal("a machine identity was accepted as a browser proof")
	}
	if _, err := f.broker.Verify(f.ctx, f.token(t, customeridentity.DexSubject("human-fixture", customeridentity.ZitadelConnectorID), "nonce", nil), "nonce"); err != nil {
		t.Fatal("a customer browser proof was refused")
	}
}

const fixtureAgentAudience = "machine-fixture"

func linkFixtureAgent(t *testing.T, b *browserFixture, grants []string) string {
	t.Helper()
	subject := customeridentity.DexSubject(fixtureAgentAudience, "")
	raw, err := json.Marshal([]agentSubject{{Issuer: b.f.issuer, Subject: subject, Grants: grants}})
	if err != nil || b.svc.LinkAgents(b.f.ctx, fixtureAgentAudience, string(raw)) != nil {
		t.Fatal("configure synthetic machine")
	}
	return subject
}

func fixtureAgentToken(t *testing.T, b *browserFixture, subject string) string {
	t.Helper()
	return b.f.token(t, subject, "", map[string]any{"aud": fixtureAgentAudience, "exp": time.Now().Add(4 * time.Minute).Unix()})
}

func agentRequest(t *testing.T, b *browserFixture, client *http.Client, method, path, token string) (int, string, http.Header) {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), method, b.site.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	if closeErr := res.Body.Close(); closeErr != nil {
		t.Error(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body), res.Header
}

// Machine reads use the real HTTP/JWKS/database/audit path without enrolling any
// operator membership or creating a browser session. Refusals are audited too.
func TestMachineAPIReadsAndAuditWithoutHumanAuthority(t *testing.T) {
	b := newBrowserFixture(t)
	subject := linkFixtureAgent(t, b, nil)
	raw := fixtureAgentToken(t, b, subject)
	for _, path := range []string{"/invitations", "/workspaces", "/audit", "/openapi.json"} {
		status, _, headers := agentRequest(t, b, b.site.Client(), "GET", operatorAPIRoot+path, raw)
		if status != http.StatusOK || headers.Get("Cache-Control") != "no-store" || headers.Get("Set-Cookie") != "" {
			t.Fatalf("machine read %s did not retain its own authority: %d", path, status)
		}
	}
	if got := b.auditRows(); len(got) != 4 || got[0] != "agent invitations.list allowed" || got[3] != "agent openapi.read allowed" {
		t.Fatalf("machine calls lost audit attribution: %q", got)
	}
	var memberships int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_memberships`).Scan(&memberships); err != nil || memberships != 0 {
		t.Fatal("machine access enrolled a browser membership")
	}
	stranger := fixtureAgentToken(t, b, customeridentity.DexSubject("other-machine", ""))
	if status, _, _ := agentRequest(t, b, b.site.Client(), "GET", operatorAPIRoot+"/invitations", stranger); status != http.StatusForbidden {
		t.Fatal("unlisted machine was admitted", status)
	}
	if got := b.auditRows(); got[len(got)-1] != "agent invitations.list refused" {
		t.Fatal("unlisted verified machine refusal was not recorded")
	}
	if status, _, _ := agentRequest(t, b, b.site.Client(), "GET", "/portal/operator/audit", raw); status != http.StatusForbidden {
		t.Fatal("machine reached a human page", status)
	}
}

// Each grantable write is refused before its callback without its exact grant.
// Synthetic dispatch routes exercise the shared authority boundary while the
// action-specific provider/database behavior is covered by its own slice.
func TestMachineAPIWriteGrantsAreExactAndCannotMoveMoneyOrDelete(t *testing.T) {
	for _, granted := range []string{"", "invitations.create", "members.deactivate"} {
		t.Run(granted, func(t *testing.T) {
			b := newBrowserFixture(t)
			var grants []string
			if granted != "" {
				grants = []string{granted}
			}
			subject := linkFixtureAgent(t, b, grants)
			// Even an in-memory policy defect cannot turn these names into powers.
			// The startup loader separately refuses them in real configuration.
			b.svc.agents.subjects[subject]["payments.refund"] = true
			b.svc.agents.subjects[subject]["customers.delete"] = true
			original := operatorRoutes
			operatorRoutes = slices.Clone(original)
			t.Cleanup(func() { operatorRoutes = original })
			calls := 0
			actions := []string{"invitations.create", "invitations.revoke", "workspaces.create", "members.deactivate", "members.reactivate", "payments.refund", "customers.delete", "unknown.write"}
			for _, action := range actions {
				operatorRoutes = append(operatorRoutes, operatorRoute{"POST", "/test-" + action, action, func(_ *browser, _ *http.Request, ctx context.Context) (any, error) {
					calls++
					setOperatorTarget(ctx, "synthetic-target")
					return map[string]bool{"accepted": true}, nil
				}})
			}
			b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
			for _, action := range actions {
				before := calls
				status, _, _ := agentRequest(t, b, b.site.Client(), "POST", operatorAPIRoot+"/test-"+action, fixtureAgentToken(t, b, subject))
				want := http.StatusForbidden
				if action == granted {
					want = http.StatusOK
				}
				if status != want || (calls != before) != (status == http.StatusOK) {
					t.Fatalf("grant=%s action=%s status=%d callbacks=%d", granted, action, status, calls-before)
				}
			}
			rows := b.auditRows()
			if len(rows) != len(actions) || rows[len(rows)-1] != "agent unknown.write refused" {
				t.Fatal("write refusals were not audited", rows)
			}
		})
	}
}

// A bearer cannot borrow a human cookie after validation fails, and accepting a
// machine token never silently switches a browser request to a different actor.
func TestMachineAPIRefusesBearerAndSessionAmbiguity(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	subject := linkFixtureAgent(t, b, nil)
	for _, raw := range []string{"invalid", fixtureAgentToken(t, b, subject)} {
		if status, _, _ := agentRequest(t, b, b.client, "GET", operatorAPIRoot+"/invitations", raw); status != http.StatusUnauthorized {
			t.Fatal("bearer request borrowed a human session", status)
		}
	}
	if status, _, _ := b.request("GET", operatorAPIRoot+"/invitations", nil, ""); status != http.StatusOK {
		t.Fatal("ordinary operator session stopped working", status)
	}
}

// No machine access exists without both server policy and the independent
// identity gate. A public runtime never opens an API because a JWT is presented.
func TestMachineAPIIsClosedWithoutPolicyOrActivation(t *testing.T) {
	for _, policy := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-policy", true: "identity-off"}[policy], func(t *testing.T) {
			b := newBrowserFixture(t)
			subject := customeridentity.DexSubject(fixtureAgentAudience, "")
			want := http.StatusUnauthorized
			if policy {
				subject = linkFixtureAgent(t, b, nil)
				b.svc.flags = flagAPI(t, false).NewClient()
				want = http.StatusNotFound
			}
			if status, _, _ := agentRequest(t, b, b.site.Client(), "GET", operatorAPIRoot+"/invitations", fixtureAgentToken(t, b, subject)); status != want {
				t.Fatal("closed machine boundary admitted a request", status)
			}
			if len(b.auditRows()) != 0 {
				t.Fatal("closed boundary treated an unauthenticated token as an actor")
			}
		})
	}
}

// VerifyMachine validates the actual signed JWT against the synthetic broker's
// TLS JWKS. No token claims can select a human role or extend a five-minute lease.
func TestMachineProofRequiresDistinctBoundedClaims(t *testing.T) {
	f := fixtureBroker(t)
	subject := customeridentity.DexSubject("machine-fixture", "")
	changes := func(extra map[string]any) map[string]any {
		values := map[string]any{"nonce": "", "exp": time.Now().Add(4 * time.Minute).Unix()}
		for key, value := range extra {
			values[key] = value
		}
		return values
	}
	if p, err := f.broker.VerifyMachine(f.ctx, f.token(t, subject, "", changes(nil))); err != nil || p.subject != subject || p.audience != "portal-fixture" {
		t.Fatal("valid machine proof refused")
	}
	for name, extra := range map[string]map[string]any{
		"issuer":                 {"iss": "https://other.example.invalid"},
		"browser audience":       {"aud": "business-portal"},
		"multiple audiences":     {"aud": []string{"portal-fixture", "business-portal"}, "azp": "portal-fixture"},
		"wrong authorized party": {"azp": "other"},
		"expired":                {"exp": time.Now().Add(-time.Minute).Unix()},
		"too long":               {"exp": time.Now().Add(6 * time.Minute).Unix()},
		"missing issued at":      {"iat": 0},
		"future issued at":       {"iat": time.Now().Add(time.Minute).Unix()},
		"not yet valid":          {"nbf": time.Now().Add(time.Minute).Unix()},
		"browser nonce":          {"nonce": "login-nonce"},
		"human subject":          {"sub": customeridentity.DexSubject("human", customeridentity.PlatformConnectorID)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.broker.VerifyMachine(f.ctx, f.token(t, subject, "", changes(extra))); err == nil {
				t.Fatal("invalid machine proof accepted")
			}
		})
	}
	raw := f.token(t, subject, "", changes(nil))
	parts := strings.Split(raw, ".")
	parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
	if _, err := f.broker.VerifyMachine(f.ctx, strings.Join(parts, ".")); err == nil {
		t.Fatal("forged machine proof accepted")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err := f.broker.VerifyMachine(ctx, raw); err == nil {
		t.Fatal("cancelled machine proof accepted")
	}
}
