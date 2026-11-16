package identity

import (
	"context"
	"encoding/base64"
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
