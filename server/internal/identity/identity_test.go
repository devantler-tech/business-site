package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/isolated"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

// All keys, accounts, addresses and records in this fixture are synthetic.
type brokerFixture struct {
	broker *Broker
	key    *rsa.PrivateKey
	issuer string
	ctx    context.Context
}

func fixtureBroker(t *testing.T) brokerFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	issuer = srv.URL
	t.Cleanup(srv.Close)
	ctx := oidc.ClientContext(context.Background(), srv.Client())
	b, err := NewBroker(ctx, issuer, "portal-fixture")
	if err != nil {
		t.Fatalf("discover synthetic broker: %v", err)
	}
	return brokerFixture{b, key, issuer, ctx}
}

func (f brokerFixture) token(t *testing.T, subject, nonce string, changes map[string]any) string {
	t.Helper()
	claims := map[string]any{"iss": f.issuer, "sub": subject, "aud": "portal-fixture", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "email": "same@example.invalid", "groups": []string{"operator"}}
	for k, v := range changes {
		claims[k] = v
	}
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"fixture"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := head + "." + base64.RawURLEncoding.EncodeToString(body)
	hash := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestBrokerRejectsInvalidProofs(t *testing.T) {
	f := fixtureBroker(t)
	if _, err := f.broker.Verify(f.ctx, f.token(t, "client-a", "nonce", nil), "nonce"); err != nil {
		t.Fatalf("valid signed proof: %v", err)
	}
	cases := map[string]map[string]any{
		"issuer": {"iss": "https://wrong.example.invalid"}, "audience": {"aud": "wrong"}, "expiry": {"exp": time.Now().Add(-time.Hour).Unix()}, "subject": {"sub": ""}, "nonce": {"nonce": "wrong"}, "missing nonce": {"nonce": ""}, "authorized party": {"aud": []string{"portal-fixture", "other"}, "azp": "other"}, "missing authorized party": {"aud": []string{"portal-fixture", "other"}},
	}
	for name, changes := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.broker.Verify(f.ctx, f.token(t, "client-a", "nonce", changes), "nonce"); err == nil {
				t.Fatal("invalid proof accepted")
			}
		})
	}
	raw := f.token(t, "client-a", "nonce", nil)
	parts := strings.Split(raw, ".")
	parts[2] = base64.RawURLEncoding.EncodeToString(make([]byte, 256))
	if _, err := f.broker.Verify(f.ctx, strings.Join(parts, "."), "nonce"); err == nil {
		t.Fatal("forged signature accepted")
	}
	if _, err := f.broker.Verify(f.ctx, raw, ""); err == nil {
		t.Fatal("empty transaction nonce accepted")
	}
	if _, err := NewBroker(context.Background(), "http://broker.example.invalid", "portal-fixture"); err == nil {
		t.Fatal("insecure broker allowed")
	}
}

func migrationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PORTAL_TEST_MIGRATION_DATABASE_URL")
	if dsn == "" {
		t.Fatal("PORTAL_TEST_MIGRATION_DATABASE_URL is required")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "portal_identity_test" || cfg.ConnConfig.User != "portal_migrator" {
		t.Fatal("refusing to migrate a non-fixture database/role")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func fixtureDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PORTAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("PORTAL_TEST_DATABASE_URL is required; database tests must not skip")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "portal_identity_test" || cfg.ConnConfig.User != "portal_runtime" {
		t.Fatal("refusing to test against a non-fixture database/role")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrator := migrationDB(t)
	if err = Migrate(context.Background(), migrator); err != nil {
		t.Fatalf("migrate actual PostgreSQL: %v", err)
	}
	if _, err = migrator.Exec(context.Background(), "GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON portal_login_attempts, portal_invitations, portal_memberships, portal_sessions TO portal_runtime"); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), "TRUNCATE portal_login_attempts, portal_invitations, portal_memberships, portal_sessions"); err != nil {
		t.Fatal(err)
	}
	return pool
}

func enabled(t *testing.T, pool *pgxpool.Pool, f brokerFixture) *Foundation {
	t.Helper()
	return New(pool, f.broker, flagAPI(t, true).NewClient())
}

func flagAPI(t *testing.T, value any) *openfeature.EvaluationAPI {
	t.Helper()
	api := isolated.NewAPI()
	setFlag(t, api, value)
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })
	return api
}

func setFlag(t *testing.T, api *openfeature.EvaluationAPI, value any) {
	t.Helper()
	flags := map[string]memprovider.InMemoryFlag{}
	if value != nil {
		flags["portal-identity"] = memprovider.InMemoryFlag{Key: "portal-identity", State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": value}}
	}
	if err := api.SetProviderAndWait(context.Background(), memprovider.NewInMemoryProvider(flags)); err != nil {
		t.Fatal(err)
	}
}

func enroll(t *testing.T, svc *Foundation, f brokerFixture, subject, client, role string) Principal {
	t.Helper()
	inv, err := svc.Invite(f.ctx, Invitation{f.issuer, subject, client, role, time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Complete(f.ctx, a.State, a.BrowserBinding, f.token(t, subject, a.Nonce, nil), inv)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDurableInvitationAndAuthorization(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	a := enroll(t, svc, f, "account-a", "client-a", "client")
	b := enroll(t, svc, f, "account-b", "client-b", "client")
	op := enroll(t, svc, f, "account-operator", "operations", "operator")
	for _, tc := range []struct {
		name           string
		p              Principal
		client         string
		operator, want bool
	}{
		{"missing record scope", a, "", false, false},
		{"own", a, "client-a", false, true}, {"other client", a, "client-b", false, false}, {"email collision", b, "client-a", false, false}, {"token groups not operator", a, "client-a", true, false}, {"assigned operator", op, "client-b", true, true}, {"zero proof", Principal{}, "client-a", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.Authorize(f.ctx, tc.p, tc.client, tc.operator)
			if (err == nil) != tc.want {
				t.Fatalf("allowed=%v want=%v error=%v", err == nil, tc.want, err)
			}
		})
	}
	expired := a
	expired.expires = time.Now().Add(-time.Minute)
	if err := svc.Authorize(f.ctx, expired, "client-a", false); !errors.Is(err, ErrDenied) {
		t.Fatal("expired principal accepted")
	}
	wrongAudience := a
	wrongAudience.audience = "other-relying-party"
	if err := svc.Authorize(f.ctx, wrongAudience, "client-a", false); !errors.Is(err, ErrDenied) {
		t.Fatal("another relying party accepted")
	}
	if err := svc.Revoke(f.ctx, f.issuer, "account-a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(f.ctx, a, "client-a", false); err == nil {
		t.Fatal("revoked identity retained access")
	}
	if err := svc.Authorize(f.ctx, b, "client-b", false); err != nil {
		t.Fatal("unrelated member revoked")
	}
	// A new pool and service prove records are not process/session-memory authority.
	newPool, err := pgxpool.New(f.ctx, os.Getenv("PORTAL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer newPool.Close()
	restarted := enabled(t, newPool, f)
	if err = restarted.Authorize(f.ctx, b, "client-b", false); err != nil {
		t.Fatalf("membership lost after restart: %v", err)
	}
	if err = Migrate(f.ctx, migrationDB(t)); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err = restarted.Authorize(f.ctx, a, "client-a", false); err == nil {
		t.Fatal("restart forgot revocation")
	}
}

func TestAttemptsAndInvitationsAreAtomic(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	inv, err := svc.Invite(f.ctx, Invitation{f.issuer, "atomic", "client-atomic", "client", time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.State == a.BrowserBinding || a.State == a.Nonce || a.PKCEVerifier == "" {
		t.Fatal("attempt secrets are missing or reused")
	}
	if _, err = svc.Complete(f.ctx, a.State, "wrong-browser", f.token(t, "atomic", a.Nonce, nil), inv); err == nil {
		t.Fatal("wrong browser accepted")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	atomicRaw := f.token(t, "atomic", a.Nonce, nil)
	for range 12 {
		wg.Go(func() {
			if _, err := svc.Complete(f.ctx, a.State, a.BrowserBinding, atomicRaw, inv); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrDenied) {
				t.Errorf("unexpected concurrent error: %v", err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("callback successful consumers=%d want 1", successes.Load())
	}
	// Independent valid login attempts race for the same invitation.
	inv, err = svc.Invite(f.ctx, Invitation{f.issuer, "invite-race", "client-race", "client", time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	for range 12 {
		attempt, err := svc.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		raw := f.token(t, "invite-race", attempt.Nonce, nil)
		wg.Go(func() {
			if _, err := svc.Complete(f.ctx, attempt.State, attempt.BrowserBinding, raw, inv); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrDenied) {
				t.Errorf("unexpected invitation error: %v", err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("invitation successful consumers=%d want 1", successes.Load())
	}
}

func TestRejectedAttemptsAndInvitationBindings(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	for _, kind := range []string{"wrong subject", "expired invitation", "revoked invitation", "expired attempt", "wrong nonce", "used attempt", "existing identity"} {
		t.Run(kind, func(t *testing.T) {
			sub := "subject-" + kind
			expires := time.Now().Add(time.Hour)
			inv, err := svc.Invite(f.ctx, Invitation{f.issuer, sub, "client-a", "client", expires})
			if err != nil {
				t.Fatal(err)
			}
			a, err := svc.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "revoked invitation" {
				_, err = pool.Exec(f.ctx, "UPDATE portal_invitations SET revoked=true WHERE subject=$1", sub)
			}
			if kind == "expired invitation" {
				_, err = pool.Exec(f.ctx, "UPDATE portal_invitations SET expires_at=now()-interval '1 minute' WHERE subject=$1", sub)
			}
			if kind == "expired attempt" {
				_, err = pool.Exec(f.ctx, "UPDATE portal_login_attempts SET expires_at=now()-interval '1 minute'")
			}
			if err != nil {
				t.Fatal(err)
			}
			nonce := a.Nonce
			if kind == "wrong nonce" {
				nonce = "wrong"
			}
			tokenSub := sub
			if kind == "wrong subject" {
				tokenSub = "attacker"
			}
			if kind == "existing identity" {
				_ = enroll(t, svc, f, sub, "client-b", "client")
			}
			raw := f.token(t, tokenSub, nonce, nil)
			if kind == "used attempt" {
				if _, err = svc.Complete(f.ctx, a.State, a.BrowserBinding, raw, inv); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = svc.Complete(f.ctx, a.State, a.BrowserBinding, raw, inv); !errors.Is(err, ErrDenied) {
				t.Fatalf("%s not denied: %v", kind, err)
			}
		})
	}
}

func TestDisabledAndIncompleteConfiguration(t *testing.T) {
	for _, svc := range []*Foundation{nil, New(nil, nil, nil), New(nil, nil, flagAPI(t, true).NewClient())} {
		if _, err := svc.Begin(context.Background()); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled begin accepted")
		}
		if _, err := svc.Invite(context.Background(), Invitation{}); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled invite accepted")
		}
		if _, err := svc.Complete(context.Background(), "", "", "", ""); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled completion accepted")
		}
		if err := svc.Authorize(context.Background(), Principal{}, "client", false); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled authorization accepted")
		}
	}
}

func TestOpenFeatureGateDefaultsOffAndIsReevaluated(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	api := flagAPI(t, true)
	svc := New(pool, f.broker, api.NewClient())
	p := enroll(t, svc, f, "account-a", "client-a", "client")
	for name, value := range map[string]any{"off": false, "missing flag": nil, "wrong type": "true"} {
		t.Run(name, func(t *testing.T) {
			setFlag(t, api, value)
			if _, err := svc.Begin(f.ctx); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag allowed begin")
			}
			if _, err := svc.Invite(f.ctx, Invitation{f.issuer, "account-b", "client-b", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag allowed invitation")
			}
			if _, err := svc.Complete(f.ctx, "state", "browser", "token", ""); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag allowed completion")
			}
			if err := svc.Authorize(f.ctx, p, "client-a", false); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag retained authorization")
			}
			if err := svc.Revoke(f.ctx, f.issuer, "account-a"); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag allowed revocation")
			}
			setFlag(t, api, true)
			if err := svc.Authorize(f.ctx, p, "client-a", false); err != nil {
				t.Fatalf("reopened valid flag: %v", err)
			}
		})
	}
	svc.flags = nil
	if _, err := svc.Begin(f.ctx); !errors.Is(err, ErrDenied) {
		t.Fatal("missing client allowed begin")
	}
}
