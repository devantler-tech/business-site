package runtimeapp

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the compiled command rather than replacing its OpenFeature client.
// All federation/store traffic stays inside disposable, synthetic TLS fixtures.
func TestActualExecutableStagingIdentity(t *testing.T) {
	dir := t.TempDir()
	binary, public := buildActualCommand(t)
	var issuer string
	var discoveryDelay atomic.Int64
	discoveryDelay.Store(int64(5 * time.Second))
	broker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		// Slow but valid broker startup must leave time for the enabled assertion.
		if delay := time.Duration(discoveryDelay.Swap(0)); delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}); err != nil {
			t.Error("synthetic discovery response failed")
		}
	}))
	t.Cleanup(broker.Close)
	issuer = broker.URL
	database := runtimeFixtureDatabase(t)
	prepareConstructorDatabase(t, database)
	database.Host = fixtureTLSDatabase(t, database.Host, broker.TLS.Certificates[0])
	ca, secret := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "secret")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: broker.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal("synthetic certificate write failed")
	}
	if err := os.WriteFile(secret, []byte("synthetic-constructor-secret"), 0600); err != nil {
		t.Fatal("synthetic client configuration write failed")
	}
	projection := projectedIdentityFixture(t, []byte("synthetic-constructor-secret"), 0440)
	query := database.Query()
	query.Set("sslmode", "verify-full")
	query.Set("sslrootcert", ca)
	database.RawQuery = query.Encode()
	environment := []string{"PORTAL_DATABASE_URL=" + database.String(), "PORTAL_OIDC_ISSUER=" + issuer,
		"PORTAL_OIDC_CLIENT_ID=portal-fixture", "PORTAL_CALLBACK_URL=https://app.example.invalid/portal/callback",
		"PORTAL_CLIENT_SECRET_FILE=" + secret, "SSL_CERT_FILE=" + ca}
	on := `{"defaultVariant":"on","variants":[{"name":"on","value":true}]}`
	for _, tc := range []struct {
		name, flag string
		want       int
		projected  bool
	}{
		{"explicit identity after slow startup", on, 200, false},
		{"missing flag", "", 404, false},
		{"false", `{"defaultVariant":"off","variants":[{"name":"off","value":false}]}`, 404, false},
		{"malformed", "{invalid", 404, false},
		{"wrong type", `{"defaultVariant":"on","variants":[{"name":"on","value":"true"}]}`, 404, false},
		{"explicit projected identity", on, 200, true},
		{"projected identity remains off", "", 404, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			until := time.Now().Add(30 * time.Second)
			env := append([]string{}, environment...)
			if tc.projected {
				if tc.want == 200 {
					// Projection startup must also tolerate a slow but valid discovery.
					discoveryDelay.Store(int64(7 * time.Second))
				}
				// Leave margin beyond the executable's 45-second startup deadline.
				until = time.Now().Add(time.Minute)
				env = nil
				for _, value := range environment {
					if !strings.HasPrefix(value, "PORTAL_CLIENT_SECRET_FILE=") {
						env = append(env, value)
					}
				}
				env = append(env, "PORTAL_CLIENT_SECRET_PROJECTION_FILE="+projection)
			}
			env = append(env, "PORTAL_IDENTITY_FLAG="+tc.flag, "PORTAL_IDENTITY_LEASE_UNTIL="+until.UTC().Format(time.RFC3339Nano),
				"portal-requests="+on, "portal-triage="+on, "portal-proposals="+on,
				"portal-proposal-sharing="+on, "portal-proposal-acceptance="+on)
			address, stop := startActualCommand(t, binary, public, env)
			defer stop()
			commandStatus(t, address, "/portal/sign-in", tc.want)
			for _, route := range []string{"/", "/da/", "/projects/", "/blog/"} {
				commandStatus(t, address, route, 200)
			}
			commandStatus(t, address, "/portal/requests", 404)
			commandStatus(t, address, "/portal/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA/proposal-review", 404)
			commandStatus(t, address, "/portal/requests/AAAAAAAAAAAAAAAAAAAAAAAAAA/proposal-acceptance", 404)
			commandStatus(t, address, "/portal/operator", map[bool]int{true: 403, false: 404}[tc.want == 200])
			// The ordinary transport retains the actual shared-provider expiry proof.
			if tc.want == 200 && !tc.projected {
				<-time.After(time.Until(until) + time.Millisecond)
				commandStatus(t, address, "/portal/sign-in", 404)
				commandStatus(t, address, "/", 200)
			}
		})
	}
}

// commandStatus observes the real route status and private-response cache policy.
func commandStatus(t *testing.T, address, route string, want int) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+route, nil)
	if err != nil {
		t.Fatal("fixture request construction failed")
	}
	req.Host = "app.example.invalid"
	response, err := (&http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal("actual command HTTP request failed")
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode != want {
		t.Fatalf("actual command route %s status=%d, want=%d", route, response.StatusCode, want)
	}
	if strings.HasPrefix(route, "/portal") && response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("actual private response became cacheable")
	}
}

// startActualCommand owns the executable through readiness, lease expiry and clean shutdown.
func startActualCommand(t *testing.T, binary, public string, environment []string) (string, func()) {
	t.Helper()
	process := launchActualCommand(t, binary, public, "127.0.0.1:0", environment)
	address, err := process.awaitReady()
	if err != nil {
		t.Fatal(err)
	}
	stop := func() {
		if err := process.stop(); err != nil {
			t.Error(err)
		}
	}
	return address, stop
}
