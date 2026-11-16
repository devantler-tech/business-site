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
	"github.com/jackc/pgx/v5"
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
	key, err := fixtureSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			if err := json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}}); err != nil {
				t.Error("fixture discovery response failed")
			}
		case "/keys":
			if err := json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}}); err != nil {
				t.Error("fixture key response failed")
			}
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

// token signs synthetic broker claims, allowing deliberate claim overrides for
// adversarial tests without contacting a real identity provider.
func (f brokerFixture) token(t testing.TB, subject, nonce string, changes map[string]any) string {
	t.Helper()
	claims := map[string]any{"iss": f.issuer, "sub": subject, "aud": "portal-fixture", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "email": "same@example.invalid"}
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

// migrationDB requires the explicit disposable database and migration role,
// rejecting an absent or differently scoped connection before schema changes.
func migrationDB(t testing.TB) *pgxpool.Pool {
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

// fixtureDB migrates and resets only the named disposable database, then returns
// its restricted runtime pool. Callers must not run database fixtures in parallel.
func fixtureDB(t testing.TB) *pgxpool.Pool {
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
	// Reset the same grants and rows in one durable transaction. Ordered row
	// deletion avoids recreating twenty-two table files on every serial fixture;
	// membership deletion must record its event before history is cleared.
	if err = pgx.BeginFunc(context.Background(), migrator, func(tx pgx.Tx) error {
		_, resetErr := tx.Exec(context.Background(), `
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON portal_login_attempts, portal_invitations, portal_memberships, portal_sessions, portal_contacts TO portal_runtime;
GRANT SELECT,INSERT,TRUNCATE ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions TO portal_runtime;
GRANT SELECT,INSERT,UPDATE,TRUNCATE ON portal_proposal_reviews TO portal_runtime;
REVOKE TRUNCATE ON portal_memberships FROM portal_runtime;
REVOKE ALL ON portal_membership_transitions FROM portal_runtime;
GRANT SELECT ON portal_membership_transitions TO portal_runtime;
GRANT SELECT,INSERT ON portal_work_approvals,portal_work_revocations,portal_work_current TO portal_runtime;
GRANT UPDATE(approval_id,generation) ON portal_work_current TO portal_runtime;
GRANT SELECT,INSERT ON portal_orders,portal_numina_contacts TO portal_runtime;
GRANT SELECT,INSERT,UPDATE ON portal_order_charges,portal_card_checkouts TO portal_runtime;
GRANT SELECT,INSERT ON portal_stripe_events TO portal_runtime;
GRANT SELECT,INSERT ON portal_order_ends,portal_card_cancellations,portal_charge_reversals,portal_order_withdrawals,portal_withdrawal_cancellations,portal_order_completions,portal_withdrawal_settlements,portal_mail_outbox,portal_mail_deliveries,portal_launch_places,portal_order_addons,portal_request_closures,portal_addon_ends,portal_addon_removals TO portal_runtime;
GRANT SELECT ON portal_schema_version TO portal_runtime;
DELETE FROM portal_work_current;
DELETE FROM portal_work_revocations;
DELETE FROM portal_work_approvals;
DELETE FROM portal_credit_revisions;
DELETE FROM portal_credit_transfers;
DELETE FROM portal_invoices;
DELETE FROM portal_charge_reversals;
DELETE FROM portal_card_cancellations;
DELETE FROM portal_order_ends;
DELETE FROM portal_addon_removals;
DELETE FROM portal_addon_ends;
DELETE FROM portal_order_addons;
DELETE FROM portal_request_closures;
DELETE FROM portal_launch_places;
DELETE FROM portal_mail_deliveries;
DELETE FROM portal_mail_outbox;
DELETE FROM portal_withdrawal_settlements;
DELETE FROM portal_order_completions;
DELETE FROM portal_withdrawal_cancellations;
DELETE FROM portal_order_withdrawals;
DELETE FROM portal_stripe_events;
DELETE FROM portal_card_checkouts;
DELETE FROM portal_order_charges;
DELETE FROM portal_orders;
DELETE FROM portal_numina_contacts;
DELETE FROM portal_proposal_reviews;
DELETE FROM portal_proposal_acceptances;
DELETE FROM portal_proposal_visibility;
DELETE FROM portal_proposals;
DELETE FROM portal_requests;
DELETE FROM portal_memberships;
DELETE FROM portal_membership_transitions;
DELETE FROM portal_login_attempts;
DELETE FROM portal_invitations;
DELETE FROM portal_sessions;
DELETE FROM portal_contacts;
`)
		return resetErr
	}); err != nil {
		t.Fatal(err)
	}
	return pool
}

func enabled(t *testing.T, pool *pgxpool.Pool, f brokerFixture) *Foundation {
	t.Helper()
	return New(pool, f.broker, flagAPI(t, true).NewClient())
}

// flagAPI owns an isolated provider and its cleanup so feature states cannot
// escape this fixture into another test or benchmark.
func flagAPI(t testing.TB, value any) *openfeature.EvaluationAPI {
	t.Helper()
	api := isolated.NewAPI()
	setFlag(t, api, value)
	t.Cleanup(func() { _ = api.Shutdown(context.Background()) })
	return api
}

// setFlag waits for the fixture provider to expose the requested identity gate;
// nil deliberately omits the flag instead of treating absence as enabled.
func setFlag(t testing.TB, api *openfeature.EvaluationAPI, value any) {
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
	a := invitedAttempt(t, svc, f, subject, client, role)
	p, err := svc.Complete(f.ctx, a.State, a.BrowserBinding, f.token(t, subject, a.Nonce, workspaceClaims(client, role)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// workspaceClaims names a client's workspace in groups, as the administrator's
// attribute does for every client; operators carry none.
func workspaceClaims(client, role string) map[string]any {
	if role != "client" {
		return nil
	}
	return map[string]any{"groups": []string{client}}
}

func invitedAttempt(t *testing.T, svc *Foundation, f brokerFixture, subject, client, role string) Attempt {
	t.Helper()
	if err := svc.Invite(f.ctx, Invitation{f.issuer, subject, client, role, time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	a, err := svc.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return a
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
	a := invitedAttempt(t, svc, f, "atomic", "client-atomic", "client")
	if a.State == a.BrowserBinding || a.State == a.Nonce || a.PKCEVerifier == "" {
		t.Fatal("attempt secrets are missing or reused")
	}
	if _, err := svc.Complete(f.ctx, a.State, "wrong-browser", f.token(t, "atomic", a.Nonce, workspaceClaims("client-atomic", "client"))); err == nil {
		t.Fatal("wrong browser accepted")
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	atomicRaw := f.token(t, "atomic", a.Nonce, workspaceClaims("client-atomic", "client"))
	for range 12 {
		wg.Go(func() {
			if _, err := svc.Complete(f.ctx, a.State, a.BrowserBinding, atomicRaw); err == nil {
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
	// Independent valid sign-ins of the invited identity race for its invitation:
	// it is consumed once and creates exactly one membership. Later sign-ins of
	// that member then succeed as ordinary members.
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "invite-race", "client-race", "client", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	successes.Store(0)
	for range 12 {
		attempt, err := svc.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		raw := f.token(t, "invite-race", attempt.Nonce, workspaceClaims("client-race", "client"))
		wg.Go(func() {
			if _, err := svc.Complete(f.ctx, attempt.State, attempt.BrowserBinding, raw); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrDenied) {
				t.Errorf("unexpected invitation error: %v", err)
			}
		})
	}
	wg.Wait()
	var memberships, pending int
	if err := pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM portal_memberships WHERE subject='invite-race'), (SELECT count(*) FROM portal_invitations WHERE subject='invite-race')").Scan(&memberships, &pending); err != nil {
		t.Fatal(err)
	}
	if successes.Load() < 1 || memberships != 1 || pending != 0 {
		t.Fatalf("invitation race: successes=%d memberships=%d pending=%d, want >=1, 1, 0", successes.Load(), memberships, pending)
	}
}

func TestRejectedAttemptsAndInvitationBindings(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	for _, kind := range []string{"wrong subject", "expired invitation", "revoked invitation", "expired attempt", "wrong nonce", "used attempt", "revoked member"} {
		t.Run(kind, func(t *testing.T) {
			sub := "subject-" + kind
			expires := time.Now().Add(time.Hour)
			if kind == "revoked member" {
				// A revoked identity cannot be re-invited, and even a leftover
				// invitation cannot revive its membership.
				_ = enroll(t, svc, f, sub, "client-b", "client")
				if err := svc.Revoke(f.ctx, f.issuer, sub); err != nil {
					t.Fatal(err)
				}
				if err := svc.Invite(f.ctx, Invitation{f.issuer, sub, "client-a", "client", expires}); !errors.Is(err, ErrDenied) {
					t.Fatalf("a revoked member was re-invited: %v", err)
				}
				leftoverInvitation(t, pool, f, sub, "client-a")
			} else if err := svc.Invite(f.ctx, Invitation{f.issuer, sub, "client-a", "client", expires}); err != nil {
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
			// Only the used attempt first succeeds, so only it names its workspace.
			var claims map[string]any
			if kind == "used attempt" {
				claims = workspaceClaims("client-a", "client")
			}
			raw := f.token(t, tokenSub, nonce, claims)
			if kind == "used attempt" {
				if _, err = svc.Complete(f.ctx, a.State, a.BrowserBinding, raw); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = svc.Complete(f.ctx, a.State, a.BrowserBinding, raw); !errors.Is(err, ErrDenied) {
				t.Fatalf("%s not denied: %v", kind, err)
			}
			// A refused sign-in never consumes the invitation for another identity.
			if kind == "wrong subject" || kind == "wrong nonce" || kind == "expired attempt" || kind == "revoked member" {
				var pending int
				if err = pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_invitations WHERE subject=$1", sub).Scan(&pending); err != nil || pending != 1 {
					t.Fatalf("%s consumed the invitation (pending=%d, %v)", kind, pending, err)
				}
			}
		})
	}
}

// leftoverInvitation writes a pending invitation directly, as one created before
// Invite refused members or a second invitation would be, to test sign-in alone.
func leftoverInvitation(t *testing.T, pool *pgxpool.Pool, f brokerFixture, subject, client string) {
	t.Helper()
	if _, err := pool.Exec(f.ctx, `INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at) VALUES($1,$2,$3,$4,'client',now()+interval '1 hour')`, digest(secret()), f.issuer, subject, client); err != nil {
		t.Fatal(err)
	}
}

// Inviting an existing member is refused. A current member signs straight in,
// even with a leftover pending invitation, and gains nothing from it: the
// invitation stays unused and its client is not added.
func TestExistingMemberIgnoresAnotherInvitation(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	_ = enroll(t, svc, f, "member", "client-b", "client")
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "member", "client-a", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
		t.Fatalf("an existing member was invited again: %v", err)
	}
	leftoverInvitation(t, pool, f, "member", "client-a")
	a, err := svc.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Complete(f.ctx, a.State, a.BrowserBinding, f.token(t, "member", a.Nonce, workspaceClaims("client-b", "client")))
	if err != nil {
		t.Fatalf("existing member could not sign in: %v", err)
	}
	if err = svc.Authorize(f.ctx, p, "client-b", false); err != nil {
		t.Fatal("existing member lost its own client")
	}
	if err = svc.Authorize(f.ctx, p, "client-a", false); !errors.Is(err, ErrDenied) {
		t.Fatal("existing member gained the invited client")
	}
	var pending int
	if err = pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_invitations WHERE subject='member'").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("member sign-in consumed another invitation (pending=%d)", pending)
	}
}

// One identity gets one pending invitation; sign-in never chooses between two.
func TestOnePendingInvitationPerIdentity(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	expires := time.Now().Add(time.Hour)
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "twice", "client-a", "client", expires}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "twice", "client-b", "client", expires}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a second pending invitation was created: %v", err)
	}
	// Concurrent invitations of one identity serialize: exactly one is created.
	var created atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := svc.Invite(f.ctx, Invitation{f.issuer, "concurrent", "client-a", "client", expires}); err == nil {
				created.Add(1)
			} else if !errors.Is(err, ErrDenied) {
				t.Errorf("unexpected invitation error: %v", err)
			}
		})
	}
	wg.Wait()
	var concurrent int
	if err := pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_invitations WHERE subject='concurrent'").Scan(&concurrent); err != nil || created.Load() != 1 || concurrent != 1 {
		t.Fatalf("concurrent invitations: created=%d stored=%d, want 1 and 1", created.Load(), concurrent)
	}
	// A second pending invitation that exists anyway is refused, not chosen.
	leftoverInvitation(t, pool, f, "twice", "client-b")
	if _, err := signIn(t, svc, f, "twice", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("sign-in chose between two pending invitations: %v", err)
	}
	var pending, memberships int
	if err := pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM portal_invitations WHERE subject='twice'), (SELECT count(*) FROM portal_memberships WHERE subject='twice')").Scan(&pending, &memberships); err != nil || pending != 2 || memberships != 0 {
		t.Fatalf("refused sign-in changed invitations or memberships: pending=%d memberships=%d", pending, memberships)
	}
	// After an invitation expires, the identity can be invited again.
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "expired-once", "client-a", "client", expires}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(f.ctx, "UPDATE portal_invitations SET expires_at=now()-interval '1 minute' WHERE subject='expired-once'"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Invite(f.ctx, Invitation{f.issuer, "expired-once", "client-a", "client", expires}); err != nil {
		t.Fatalf("an expired invitation blocked a new one: %v", err)
	}
}

// An identity with neither membership nor invitation is refused after a valid
// authentication: the portal is invitation-only.
func TestUninvitedIdentityIsRefused(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	if _, err := signIn(t, svc, f, "stranger", nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("uninvited identity was not refused: %v", err)
	}
	var memberships int
	if err := pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_memberships WHERE subject='stranger'").Scan(&memberships); err != nil || memberships != 0 {
		t.Fatal("uninvited identity gained a membership")
	}
}

func TestDisabledAndIncompleteConfiguration(t *testing.T) {
	for _, svc := range []*Foundation{nil, New(nil, nil, nil), New(nil, nil, flagAPI(t, true).NewClient())} {
		if _, err := svc.Begin(context.Background()); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled begin accepted")
		}
		if err := svc.Invite(context.Background(), Invitation{}); !errors.Is(err, ErrDenied) {
			t.Fatal("disabled invite accepted")
		}
		if _, err := svc.Complete(context.Background(), "", "", ""); !errors.Is(err, ErrDenied) {
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
			if err := svc.Invite(f.ctx, Invitation{f.issuer, "account-b", "client-b", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
				t.Fatal("closed flag allowed invitation")
			}
			if _, err := svc.Complete(f.ctx, "state", "browser", "token"); !errors.Is(err, ErrDenied) {
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

// signIn runs one complete sign-in attempt for subject through the real store,
// with any extra token claims.
func signIn(t *testing.T, svc *Foundation, f brokerFixture, subject string, claims map[string]any) (Principal, error) {
	t.Helper()
	a, err := svc.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return svc.Complete(f.ctx, a.State, a.BrowserBinding, f.token(t, subject, a.Nonce, claims))
}

// Opening the portal starts an attempt, so Begin also removes a bounded batch
// of expired attempts; abandoned ones cannot pile up.
func TestBeginRemovesExpiredAttempts(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	for range 60 {
		if _, err := svc.Begin(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(f.ctx, "UPDATE portal_login_attempts SET expires_at=now()-interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Begin(f.ctx); err != nil {
		t.Fatal(err)
	}
	var expired, current int
	if err := pool.QueryRow(f.ctx, "SELECT count(*) FILTER (WHERE expires_at<now()), count(*) FILTER (WHERE expires_at>now()) FROM portal_login_attempts").Scan(&expired, &current); err != nil {
		t.Fatal(err)
	}
	if expired != 10 || current != 1 {
		t.Fatalf("expired=%d current=%d, want 10 expired left after one bounded batch and 1 current", expired, current)
	}
}

// An administrator-set workspace, delivered as exactly one groups value in the
// verified token, admits a first sign-in as that workspace's client, and every
// later client sign-in needs it to still name that workspace. It never grants
// operator, never moves or revives a membership, and a pending invitation wins.
func TestGroupsAdmitFirstSignInAsClient(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	svc := enabled(t, pool, f)
	withGroups := func(subject string, groups any) (Principal, error) {
		t.Helper()
		return signIn(t, svc, f, subject, map[string]any{"groups": groups})
	}
	role := func(subject string) string {
		var r string
		if err := pool.QueryRow(f.ctx, "SELECT role FROM portal_memberships WHERE subject=$1", subject).Scan(&r); err != nil {
			return ""
		}
		return r
	}
	p, err := withGroups("admitted", []string{"acme"})
	if err != nil || svc.Authorize(f.ctx, p, "acme", false) != nil || role("admitted") != "client" {
		t.Fatalf("a named workspace did not admit a first sign-in as its client: %v", err)
	}
	if svc.Authorize(f.ctx, p, "acme", true) == nil {
		t.Fatal("a groups value granted operator")
	}
	// Every sign-in of a client needs its current workspace: a changed, removed
	// or malformed one refuses it, and neither moves nor removes the membership.
	for name, groups := range map[string]any{"changed": []string{"other"}, "removed": nil, "malformed": []string{"Acme"}} {
		if _, err = withGroups("admitted", groups); !errors.Is(err, ErrDenied) {
			t.Fatalf("a %s workspace still signed an existing client in: %v", name, err)
		}
	}
	if p, err = withGroups("admitted", []string{"acme"}); err != nil || svc.Authorize(f.ctx, p, "acme", false) != nil || role("admitted") != "client" {
		t.Fatalf("the restored workspace did not sign its client in: %v", err)
	}
	// A client invitation is consumed only by a sign-in naming its workspace.
	if err = svc.Invite(f.ctx, Invitation{f.issuer, "invited-client", "beta", "client", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err = withGroups("invited-client", nil); !errors.Is(err, ErrDenied) || role("invited-client") != "" {
		t.Fatalf("a client invitation admitted a sign-in without its workspace: %v", err)
	}
	var pending int
	if err = pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_invitations WHERE subject='invited-client'").Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("a refused sign-in consumed the client invitation: pending=%d", pending)
	}
	if _, err = withGroups("invited-client", []string{"beta"}); err != nil || role("invited-client") != "client" {
		t.Fatalf("the client invitation did not admit its workspace: %v", err)
	}
	for name, groups := range map[string]any{"none": nil, "two values": []string{"acme", "beta"}, "empty value": []string{""}, "uppercase": []string{"Acme"}, "not a list": "acme", "too long": []string{strings.Repeat("a", 65)}, "path-like": []string{"../acme"}} {
		subject := "shape-" + strings.ReplaceAll(name, " ", "-")
		if _, err = withGroups(subject, groups); !errors.Is(err, ErrDenied) || role(subject) != "" {
			t.Fatalf("groups %s admitted a sign-in: %v", name, err)
		}
	}
	// A revoked member stays refused whatever the workspace says.
	if err = svc.Revoke(f.ctx, f.issuer, "admitted"); err != nil {
		t.Fatal(err)
	}
	if _, err = withGroups("admitted", []string{"acme"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("a revoked member was readmitted: %v", err)
	}
	// A pending invitation wins over the workspace, including its role.
	if err = svc.Invite(f.ctx, Invitation{f.issuer, "both", "operations", "operator", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var bothClient string
	if p, err = withGroups("both", []string{"acme"}); err != nil || svc.Authorize(f.ctx, p, "operations", true) != nil || role("both") != "operator" ||
		pool.QueryRow(f.ctx, "SELECT client_id FROM portal_memberships WHERE subject='both'").Scan(&bothClient) != nil || bothClient != "operations" {
		t.Fatalf("the workspace overrode a pending invitation: %v", err)
	}
	// Concurrent first sign-ins of one identity create one membership, and each
	// succeeds once it exists.
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Go(func() {
			if _, err := withGroups("racer", []string{"acme"}); err != nil {
				failed.Add(1)
			}
		})
	}
	wg.Wait()
	var memberships int
	if err = pool.QueryRow(f.ctx, "SELECT count(*) FROM portal_memberships WHERE subject='racer'").Scan(&memberships); err != nil || memberships != 1 || failed.Load() != 0 {
		t.Fatalf("concurrent first sign-ins: memberships=%d failed=%d, want 1 and 0", memberships, failed.Load())
	}
}
