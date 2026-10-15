package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"sync"
	"testing"
)

// All fixtures in this test binary use immutable, test-only signing material.
// Each broker still owns its TLS server, issuer, verifier and transaction state.
var fixtureSigningKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

// TestFixtureSigningKeyReuse retains real signed-proof verification without
// repeatedly paying for random prime generation in each synthetic fixture.
func TestFixtureSigningKeyReuse(t *testing.T) {
	first, second := fixtureBroker(t), fixtureBroker(t)
	browser := newBrowserFixture(t)
	if first.key != second.key || first.key != browser.f.key {
		t.Fatal("synthetic fixtures regenerate the same-purpose signing material")
	}
	if first.key.N.BitLen() != 2048 {
		t.Fatal("synthetic signing key strength changed")
	}
	if first.issuer == second.issuer || first.issuer == browser.f.issuer || second.issuer == browser.f.issuer {
		t.Fatal("synthetic broker issuer isolation was lost")
	}
	for _, f := range []brokerFixture{first, second, browser.f} {
		raw := f.token(t, "fixture-key-control", "independent-nonce", nil)
		if _, err := f.broker.Verify(f.ctx, raw, "independent-nonce"); err != nil {
			t.Fatal("synthetic broker rejected its own valid signed proof", err)
		}
		for _, other := range []brokerFixture{first, second, browser.f} {
			if f.issuer != other.issuer {
				if _, err := other.broker.Verify(other.ctx, raw, "independent-nonce"); err == nil {
					t.Fatal("shared synthetic signing material collapsed issuer isolation")
				}
			}
		}
	}
}
