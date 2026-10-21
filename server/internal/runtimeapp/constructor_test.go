package runtimeapp

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
)

// Only the fixed synthetic, loopback runtime fixture may feed either test.
func runtimeFixtureDatabase(t *testing.T) *url.URL {
	t.Helper()
	database, err := url.Parse(os.Getenv("PORTAL_TEST_DATABASE_URL"))
	if err != nil || database == nil || database.Host != "127.0.0.1:55444" && database.Host != "127.0.0.1:5432" && database.Host != "localhost:5432" || database.Path != "/portal_identity_test" || database.User == nil || database.User.Username() != "portal_runtime" {
		t.Fatal("fresh loopback runtime fixture URL is required")
	}
	password, _ := database.User.Password()
	if password != "fixture-only" {
		t.Fatal("synthetic fixture credentials are required")
	}
	return database
}

// The real constructor must assemble its private file, verified TLS database
// connection, read-only role gate, broker discovery and closed browser handler.
// This synthetic TLS gateway bridges only to the disposable PostgreSQL fixture;
// it never loosens TLS in the production constructor or contacts a real broker.
func TestConfiguredPortalConstructorWithVerifiedTLS(t *testing.T) {
	for _, projected := range []bool{false, true} {
		t.Run(fmt.Sprintf("projected=%t", projected), func(t *testing.T) { configuredPortalConstructor(t, projected) })
	}
}

// configuredPortalConstructor exercises each transport through the real dependency assembly.
func configuredPortalConstructor(t *testing.T, projected bool) {
	t.Helper()
	database := runtimeFixtureDatabase(t)
	prepareConstructorDatabase(t, database)
	var issuer string
	var discoveries atomic.Int32
	broker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		discoveries.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	}))
	defer broker.Close()
	issuer = broker.URL
	gateway := fixtureTLSDatabase(t, database.Host, broker.TLS.Certificates[0])
	database.Host = gateway
	dir := t.TempDir()
	ca := filepath.Join(dir, "fixture-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: broker.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("synthetic-constructor-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	secretKey := "PORTAL_CLIENT_SECRET_FILE"
	if projected {
		secret = projectedIdentityFixture(t, []byte("synthetic-constructor-secret"), 0440)
		secretKey = "PORTAL_CLIENT_SECRET_PROJECTION_FILE"
	}
	query := database.Query()
	query.Set("sslmode", "verify-full")
	query.Set("sslrootcert", ca)
	database.RawQuery = query.Encode()
	values := map[string]string{"PORTAL_DATABASE_URL": database.String(), "PORTAL_OIDC_ISSUER": issuer, "PORTAL_OIDC_CLIENT_ID": "portal-fixture", "PORTAL_CALLBACK_URL": "https://app.example.invalid/portal/callback", secretKey: secret}
	cfg, err := ReadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal("verified TLS fixture config rejected")
	}
	api := isolated.NewAPI()
	defer func() {
		if err := api.Shutdown(context.Background()); err != nil {
			t.Error("fixture flag provider cleanup failed")
		}
	}()
	if err := api.SetProviderAndWait(context.Background(), openfeature.NoopProvider{}); err != nil {
		t.Fatal(err)
	}
	ctx := oidc.ClientContext(context.Background(), broker.Client())
	handler, shutdown, err := Portal(ctx, cfg, api.NewClient())
	if err != nil || handler == nil {
		t.Fatal("configured constructor did not assemble the actual closed handler")
	}
	defer shutdown()
	if discoveries.Load() != 1 {
		t.Fatal("constructor did not discover the real synthetic TLS broker")
	}
	for _, route := range []string{"/portal", "/portal/sign-in", "/portal/operator", "/portal/callback"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "https://app.example.invalid"+route, nil))
		if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("complete dependencies activated the closed portal")
		}
	}
	for _, tc := range []struct {
		name, parameter, value string
	}{
		{"effective role differs from login", "role", "portal_runtime"},
		{"startup options change effective role", "options", "-c role=portal_runtime"},
		{"session identity differs from login", "session_authorization", "portal_runtime"},
		{"startup options change session identity", "options", "-c session_authorization=portal_runtime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			misconfigured := *database
			misconfigured.User = url.UserPassword("portal_fixture_owner", "fixture-only")
			query := misconfigured.Query()
			query.Set(tc.parameter, tc.value)
			misconfigured.RawQuery = query.Encode()
			values := map[string]string{"PORTAL_DATABASE_URL": misconfigured.String(), "PORTAL_OIDC_ISSUER": issuer, "PORTAL_OIDC_CLIENT_ID": "portal-fixture", "PORTAL_CALLBACK_URL": "https://app.example.invalid/portal/callback", secretKey: secret}
			unsafe, err := ReadConfig(func(key string) string { return values[key] })
			if err != nil {
				t.Fatal("constructor regression did not reach its database boundary")
			}
			before := discoveries.Load()
			handler, shutdown, err := Portal(ctx, unsafe, api.NewClient())
			defer shutdown()
			if !errors.Is(err, ErrConfig) || handler != nil {
				t.Error("constructor accepted a login outside the restricted identity")
			}
			if discoveries.Load() != before {
				t.Error("rejected database identity reached broker discovery")
			}
		})
	}
}

// Separate database avoids concurrent table migration/grant mutations from the
// identity package. Only the exact fixture administrator may create it, and
// cleanup drops only the uniquely named database created by this test.
func prepareConstructorDatabase(t *testing.T, database *url.URL) {
	t.Helper()
	ctx := context.Background()
	adminURL := *database
	adminURL.Path = "/postgres"
	adminURL.User = url.UserPassword("portal_fixture_owner", "fixture-only")
	admin, err := pgx.Connect(ctx, adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("portal_constructor_%d_%d", os.Getpid(), time.Now().UnixNano())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted+" OWNER portal_migrator"); err != nil {
		if err := admin.Close(ctx); err != nil {
			t.Error("fixture administrator cleanup failed")
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Error("owned constructor fixture cleanup failed")
		}
		if err := admin.Close(ctx); err != nil {
			t.Error("fixture administrator cleanup failed")
		}
	})
	database.Path = "/" + name
	migrationURL := *database
	migrationURL.User = url.UserPassword("portal_migrator", "fixture-only")
	migrator, err := pgxpool.New(ctx, migrationURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err := identity.Migrate(ctx, migrator); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(ctx, "GRANT SELECT ON portal_membership_transitions TO portal_runtime"); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public TO portal_runtime; GRANT SELECT,INSERT,UPDATE,DELETE ON portal_login_attempts,portal_invitations,portal_memberships,portal_sessions,portal_contacts TO portal_runtime; GRANT SELECT,INSERT ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions TO portal_runtime; GRANT SELECT,INSERT,UPDATE ON portal_proposal_reviews TO portal_runtime"); err != nil {
		t.Fatal(err)
	}
}

// fixtureTLSDatabase requires PostgreSQL TLS negotiation before bridging the validated fixture.
func fixtureTLSDatabase(t *testing.T, upstream string, certificate tls.Certificate) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	var workers sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				// Teardown may also close this owned connection; check both attempts.
				defer closeFixtureConnection(t, conn)
				_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
				var request [8]byte
				if _, err := io.ReadFull(conn, request[:]); err != nil || binary.BigEndian.Uint32(request[:4]) != 8 || binary.BigEndian.Uint32(request[4:]) != 80877103 {
					return
				}
				if _, err := conn.Write([]byte("S")); err != nil {
					return
				}
				secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				if err := secure.Handshake(); err != nil {
					return
				}
				backend, err := net.DialTimeout("tcp", upstream, time.Second)
				if err != nil {
					return
				}
				_ = backend.SetDeadline(time.Now().Add(30 * time.Second))
				bridgeFixtureStreams(t, backend, secure)
			}()
		}
	}()
	t.Cleanup(func() {
		closeFixtureConnection(t, listener)
		<-done
		mu.Lock()
		for _, conn := range connections {
			closeFixtureConnection(t, conn)
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener.Addr().String()
}

type fixtureFailureReporter interface {
	Helper()
	Error(...any)
}

// closeFixtureConnection reports unexpected cleanup failures, including first closes.
func closeFixtureConnection(t fixtureFailureReporter, connection io.Closer) {
	t.Helper()
	if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Error("fixture connection cleanup failed")
	}
}

// bridgeFixtureStreams is the actual constructor fixture's duplex bridge.
func bridgeFixtureStreams(t fixtureFailureReporter, backend, secure io.ReadWriteCloser) {
	defer closeFixtureConnection(t, backend)
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(backend, secure)
		closeFixtureConnection(t, backend)
		close(copied)
	}()
	_, _ = io.Copy(secure, backend)
	closeFixtureConnection(t, secure)
	<-copied
}

type fixtureCloseStream struct {
	closes        atomic.Int32
	first, second error
}

func (*fixtureCloseStream) Read([]byte) (int, error)       { return 0, io.EOF }
func (*fixtureCloseStream) Write(data []byte) (int, error) { return len(data), nil }
func (stream *fixtureCloseStream) Close() error {
	if stream.closes.Add(1) == 1 {
		return stream.first
	}
	return stream.second
}

type fixtureFailureCounter struct{ failures atomic.Int32 }

func (*fixtureFailureCounter) Helper()              {}
func (counter *fixtureFailureCounter) Error(...any) { counter.failures.Add(1) }

// The actual bridge must report the first failed cleanup, not just its duplicate.
func TestFixtureBridgeReportsCleanupFailures(t *testing.T) {
	failure := errors.New("synthetic cleanup failure")
	for _, tc := range []struct {
		name                  string
		first, second, secure error
		failures              int32
	}{
		{name: "successful first close", second: net.ErrClosed},
		{name: "already closed", first: net.ErrClosed, second: net.ErrClosed},
		{name: "first backend failure", first: failure, second: net.ErrClosed, failures: 1},
		{name: "unexpected duplicate failure", second: failure, failures: 1},
		{name: "secure close failure", second: net.ErrClosed, secure: failure, failures: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fixtureCloseStream{first: tc.first, second: tc.second}
			secure := &fixtureCloseStream{first: tc.secure}
			counter := &fixtureFailureCounter{}
			bridgeFixtureStreams(counter, backend, secure)
			if got := counter.failures.Load(); got != tc.failures {
				t.Fatalf("reported cleanup failures=%d, want %d", got, tc.failures)
			}
			if backend.closes.Load() != 2 || secure.closes.Load() != 1 {
				t.Fatal("the actual bridge cleanup paths were not exercised")
			}
		})
	}
}
