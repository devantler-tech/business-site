package runtimeapp

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// Exercise the compiled command rather than replacing its OpenFeature client.
// All federation/store traffic stays inside disposable, synthetic TLS fixtures.
func TestActualExecutableStagingIdentity(t *testing.T) {
	dir := t.TempDir()
	public := filepath.Join(dir, "public")
	for _, route := range []string{"", "da", "projects", "blog"} {
		target := filepath.Join(public, route)
		if err := os.MkdirAll(target, 0700); err != nil {
			t.Fatal("public fixture creation failed")
		}
		if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("<html>public fixture</html>"), 0600); err != nil {
			t.Fatal("public fixture write failed")
		}
	}
	artifact, err := webapp.Load(os.DirFS(public))
	if err != nil {
		t.Fatal("public fixture validation failed")
	}
	binary := filepath.Join(dir, "business-site")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "-ldflags",
		"-X main.revision="+strings.Repeat("a", 40)+" -X main.publicDigest="+artifact.Digest()+" -X main.buildMode=development", "./cmd/business-site")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("actual command compilation failed: %s", output)
	}
	var issuer string
	broker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}); err != nil {
			t.Error("synthetic discovery response failed")
		}
	}))
	t.Cleanup(broker.Close)
	issuer = broker.URL
	database, err := url.Parse(os.Getenv("PORTAL_TEST_DATABASE_URL"))
	if err != nil || database == nil || database.Host != "127.0.0.1:55444" && database.Host != "127.0.0.1:5432" && database.Host != "localhost:5432" || database.Path != "/portal_identity_test" || database.User == nil || database.User.Username() != "portal_runtime" {
		t.Fatal("fresh loopback runtime fixture URL is required")
	}
	password, _ := database.User.Password()
	if password != "fixture-only" {
		t.Fatal("synthetic fixture credentials are required")
	}
	prepareConstructorDatabase(t, database)
	database.Host = fixtureTLSDatabase(t, database.Host, broker.TLS.Certificates[0])
	ca, secret := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "secret")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: broker.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal("synthetic certificate write failed")
	}
	if err := os.WriteFile(secret, []byte("synthetic-constructor-secret"), 0600); err != nil {
		t.Fatal("synthetic client configuration write failed")
	}
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
	}{
		{"explicit identity", on, 200},
		{"missing flag", "", 404},
		{"false", `{"defaultVariant":"off","variants":[{"name":"off","value":false}]}`, 404},
		{"malformed", "{invalid", 404},
		{"wrong type", `{"defaultVariant":"on","variants":[{"name":"on","value":"true"}]}`, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			until := time.Now().Add(4 * time.Second)
			env := append([]string{}, environment...)
			env = append(env, "PORTAL_IDENTITY_FLAG="+tc.flag, "PORTAL_IDENTITY_LEASE_UNTIL="+until.UTC().Format(time.RFC3339Nano),
				"portal-requests="+on, "portal-triage="+on, "portal-proposals="+on)
			address, stop := startActualCommand(t, binary, public, env)
			defer stop()
			commandStatus(t, address, "/portal/sign-in", tc.want)
			for _, route := range []string{"/", "/da/", "/projects/", "/blog/"} {
				commandStatus(t, address, route, 200)
			}
			commandStatus(t, address, "/portal/requests", 404)
			commandStatus(t, address, "/portal/operator", map[bool]int{true: 403, false: 404}[tc.want == 200])
			if tc.want == 200 {
				<-time.After(time.Until(until) + time.Millisecond)
				commandStatus(t, address, "/portal/sign-in", 404)
				commandStatus(t, address, "/", 200)
			}
		})
	}
}

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

func startActualCommand(t *testing.T, binary, public string, environment []string) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("fixture port allocation failed")
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal("fixture port release failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	command := exec.CommandContext(ctx, binary, "--public", public, "--listen", address)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "PORTAL_") && !strings.HasPrefix(value, "portal-") && !strings.HasPrefix(value, "SSL_CERT_FILE=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, environment...)
	output, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal("fixture output channel failed")
	}
	command.Stderr = io.Discard // Private startup errors are asserted separately.
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal("actual command did not start")
	}
	done := make(chan error, 1)
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		found := scanner.Scan() && strings.HasPrefix(scanner.Text(), "runtime ready;")
		ready <- found
		_, _ = io.Copy(io.Discard, output)
		done <- command.Wait()
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if err := command.Process.Signal(os.Interrupt); err != nil {
				t.Error("actual command shutdown signal failed")
			}
			select {
			case err := <-done:
				if err != nil {
					t.Error("actual command did not shut down cleanly")
				}
			case <-ctx.Done():
				t.Error("actual command shutdown exceeded its deadline")
			}
			cancel()
		})
	}
	t.Cleanup(stop)
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("actual command exited before readiness")
		}
	case <-ctx.Done():
		t.Fatal("actual command readiness exceeded its deadline")
	}
	return address, stop
}
