package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/open-feature/go-sdk/openfeature"
)

type browserFixture struct {
	t                   *testing.T
	f                   brokerFixture
	svc                 *Foundation
	pool                *pgxpool.Pool
	flags               *openfeature.EvaluationAPI
	site, broker        *httptest.Server
	client              *http.Client
	mu                  sync.RWMutex
	handler             http.Handler
	subject             string
	claims              map[string]any
	codes               map[string]url.Values
	exchanges           atomic.Int32
	redirectExchange    bool
	unexpectedExchanges atomic.Int32
}

func newBrowserFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := &browserFixture{t: t, pool: fixtureDB(t), flags: flagAPI(t, true), subject: "client-a", codes: map[string]url.Values{}, handler: http.NotFoundHandler()}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b.f.key = key
	b.broker = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": b.f.issuer, "authorization_endpoint": b.f.issuer + "/authorize", "token_endpoint": b.f.issuer + "/token", "jwks_uri": b.f.issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "fixture", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
		case "/authorize":
			q := r.URL.Query()
			if q.Get("client_id") != "portal-fixture" || q.Get("response_type") != "code" || q.Get("scope") != "openid" || q.Get("nonce") == "" || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 || q.Get("redirect_uri") != b.site.URL+"/portal/callback" {
				http.Error(w, "invalid synthetic authorization", 400)
				return
			}
			code := secret()
			b.mu.Lock()
			b.codes[code] = q
			b.mu.Unlock()
			http.Redirect(w, r, q.Get("redirect_uri")+"?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), 303)
		case "/token":
			b.exchanges.Add(1)
			if b.redirectExchange {
				http.Redirect(w, r, b.f.issuer+"/unexpected-token", 307)
				return
			}
			_ = r.ParseForm()
			id, pass, _ := r.BasicAuth()
			b.mu.Lock()
			q, ok := b.codes[r.Form.Get("code")]
			delete(b.codes, r.Form.Get("code"))
			sub, changes := b.subject, b.claims
			b.mu.Unlock()
			challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || id != "portal-fixture" || pass != "synthetic-client-secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != q.Get("redirect_uri") || base64.RawURLEncoding.EncodeToString(challenge[:]) != q.Get("code_challenge") {
				http.Error(w, "invalid synthetic code exchange", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic-unused-access", "token_type": "Bearer", "id_token": b.f.token(t, sub, q.Get("nonce"), changes)})
		case "/unexpected-token":
			b.unexpectedExchanges.Add(1)
			http.Error(w, "unexpected destination", 400)
		default:
			http.NotFound(w, r)
		}
	}))
	b.f.issuer = b.broker.URL
	b.f.ctx = oidc.ClientContext(context.Background(), b.broker.Client())
	b.f.broker, err = NewBroker(b.f.ctx, b.f.issuer, "portal-fixture")
	if err != nil {
		t.Fatal(err)
	}
	b.svc = New(b.pool, b.f.broker, b.flags.NewClient())
	b.site = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.RLock()
		h := b.handler
		b.mu.RUnlock()
		h.ServeHTTP(w, r)
	}))
	b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
	b.client = b.site.Client()
	b.client.Jar, _ = cookiejar.New(nil)
	b.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(b.site.Close)
	t.Cleanup(b.broker.Close)
	return b
}

func (b *browserFixture) request(method, path string, form url.Values, origin string) (int, string, http.Header) {
	b.t.Helper()
	req, err := http.NewRequest(method, b.site.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		b.t.Fatal(err)
	}
	return res.StatusCode, string(body), res.Header
}

var csrfInput = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (b *browserFixture) csrf(path string) string {
	b.t.Helper()
	status, body, _ := b.request("GET", path, nil, "")
	if status != 200 {
		b.t.Fatalf("form status=%d want 200: %s", status, body)
	}
	m := csrfInput.FindStringSubmatch(body)
	if len(m) != 2 {
		b.t.Fatal("CSRF form token missing")
	}
	return html.UnescapeString(m[1])
}
func (b *browserFixture) invite(subject, client, role string) string {
	b.t.Helper()
	inv, err := b.svc.Invite(b.f.ctx, Invitation{b.f.issuer, subject, client, role, time.Now().Add(time.Hour)})
	if err != nil {
		b.t.Fatal(err)
	}
	return inv
}
func (b *browserFixture) start(inv string) string {
	b.t.Helper()
	token := b.csrf("/portal/sign-in?lang=da")
	status, body, h := b.request("POST", "/portal/sign-in", url.Values{"csrf_token": {token}, "invitation": {inv}, "lang": {"da"}, "return_to": {"https://attacker.example.invalid"}, "role": {"operator"}}, b.site.URL)
	if status != 303 {
		b.t.Fatalf("login start status=%d want303: %s", status, body)
	}
	req, err := http.NewRequest("GET", h.Get("Location"), nil)
	if err != nil {
		b.t.Fatal(err)
	}
	client := b.broker.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 303 {
		body, _ := io.ReadAll(res.Body)
		b.t.Fatalf("broker rejected actual authorization request: %d %s", res.StatusCode, body)
	}
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		b.t.Fatal(err)
	}
	return u.RequestURI()
}
func (b *browserFixture) finish(callback string) {
	b.t.Helper()
	s, body, h := b.request("GET", callback, nil, "")
	if s != 303 || h.Get("Location") != "/portal/?lang=da" {
		b.t.Fatalf("callback=%d %s %s", s, h.Get("Location"), body)
	}
}
func (b *browserFixture) cookie() *http.Cookie {
	b.t.Helper()
	u, _ := url.Parse(b.site.URL)
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == "__Host-portal_session" {
			return c
		}
	}
	b.t.Fatal("opaque session cookie missing")
	return nil
}

func TestBrowserSessionDurabilityAndLogout(t *testing.T) {
	b := newBrowserFixture(t)
	callback := b.start(b.invite("client-a", "client-a", "client"))
	preauth := b.cookie().Value
	b.finish(callback)
	authenticated := b.cookie().Value
	if authenticated == preauth {
		t.Fatal("authentication did not rotate the session")
	}
	s, body, _ := b.request("GET", "/portal/?lang=da", nil, "")
	if s != 200 || !strings.Contains(body, "Du er logget ind") || !strings.Contains(body, "client-a") || strings.Contains(body, "Operatør") {
		t.Fatalf("signed-in client page wrong: %d %s", s, body)
	}
	if strings.Contains(authenticated, "client-a") || strings.Contains(authenticated, b.f.issuer) {
		t.Fatal("identity leaked into cookie")
	}
	// A separately constructed application and pool reads the durable session.
	pool, err := pgxpool.New(b.f.ctx, b.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	b.mu.Lock()
	b.handler = browserHandler(t, New(pool, b.f.broker, b.flags.NewClient()), b.site.URL+"/portal/callback")
	b.mu.Unlock()
	s, _, _ = b.request("GET", "/portal/", nil, "")
	if s != 200 {
		t.Fatal("session lost after application restart")
	}
	token := b.csrf("/portal/")
	s, _, _ = b.request("POST", "/portal/logout", url.Values{"csrf_token": {token}}, b.site.URL)
	if s != 303 {
		t.Fatalf("logout=%d", s)
	}
	u, _ := url.Parse(b.site.URL)
	b.client.Jar.SetCookies(u, []*http.Cookie{{Name: "__Host-portal_session", Value: authenticated, Path: "/", Secure: true}})
	s, _, _ = b.request("GET", "/portal/", nil, "")
	if s == 200 {
		t.Fatal("destroyed cookie authenticates after durable logout")
	}
	b.client.Jar.SetCookies(u, []*http.Cookie{{Name: "__Host-portal_session", Value: preauth, Path: "/", Secure: true}})
	s, _, _ = b.request("GET", "/portal/", nil, "")
	if s == 200 {
		t.Fatal("old pre-authentication cookie authenticates")
	}
}

func TestBrowserCallbackIsConsumedBeforeExchange(t *testing.T) {
	b := newBrowserFixture(t)
	callback := b.start(b.invite("client-a", "client-a", "client"))
	u, _ := url.Parse(b.site.URL)
	cookies := b.client.Jar.Cookies(u)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			c := *b.client
			c.Jar = nil
			req, _ := http.NewRequest("GET", b.site.URL+callback, nil)
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
			res, err := c.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			res.Body.Close()
			if res.StatusCode == 303 {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 || b.exchanges.Load() != 1 {
		t.Fatalf("successful callbacks=%d exchanges=%d; both must be exactly1", successes.Load(), b.exchanges.Load())
	}
	s, _, _ := b.request("GET", callback, nil, "")
	if s == 303 || b.exchanges.Load() != 1 {
		t.Fatal("replayed callback exchanged code")
	}
}

func TestBrowserInvalidCallbacksNeverExchange(t *testing.T) {
	for _, kind := range []string{"state", "browser", "expired", "broker error", "duplicate code", "duplicate state"} {
		t.Run(kind, func(t *testing.T) {
			b := newBrowserFixture(t)
			callback := b.start(b.invite("client-a", "client-a", "client"))
			u, _ := url.Parse(callback)
			q := u.Query()
			switch kind {
			case "state":
				q.Set("state", "wrong")
			case "browser":
				b.client.Jar, _ = cookiejar.New(nil)
			case "expired":
				_, err := b.pool.Exec(b.f.ctx, "UPDATE portal_login_attempts SET expires_at=now()-interval '1 minute'")
				if err != nil {
					t.Fatal(err)
				}
			case "broker error":
				q.Set("error", "access_denied")
			case "duplicate code":
				q.Add("code", "other")
			case "duplicate state":
				q.Add("state", "other")
			}
			s, body, _ := b.request("GET", u.Path+"?"+q.Encode(), nil, "")
			if s != 400 || b.exchanges.Load() != 0 || strings.Contains(body, q.Get("code")) {
				t.Fatalf("invalid %s: status=%d exchanges=%d", kind, s, b.exchanges.Load())
			}
		})
	}
}

func TestBrowserSignedClaimsAndMembership(t *testing.T) {
	for _, kind := range []string{"nonce", "issuer", "audience", "expired token", "uninvited"} {
		t.Run(kind, func(t *testing.T) {
			b := newBrowserFixture(t)
			inv := b.invite("client-a", "client-a", "client")
			if kind == "uninvited" {
				inv = ""
			}
			switch kind {
			case "nonce":
				b.claims = map[string]any{"nonce": "wrong"}
			case "issuer":
				b.claims = map[string]any{"iss": "https://other.example.invalid"}
			case "audience":
				b.claims = map[string]any{"aud": "other"}
			case "expired token":
				b.claims = map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}
			}
			callback := b.start(inv)
			s, _, _ := b.request("GET", callback, nil, "")
			if s != 400 {
				t.Fatalf("invalid identity accepted: %d", s)
			}
			s, _, _ = b.request("GET", "/portal/", nil, "")
			if s == 200 {
				t.Fatal("invalid identity got durable authority")
			}
		})
	}
}

func TestBrowserCurrentRevocationExpiryAndIsolation(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	for _, path := range []string{"/portal/?client_id=client-b&role=operator", "/portal/operator"} {
		s, body, _ := b.request("GET", path, nil, "")
		if path == "/portal/operator" && s != 403 {
			t.Fatal("client got operator route")
		}
		if strings.Contains(body, "client-b") || strings.Contains(body, "Operatør") {
			t.Fatal("browser selected identity/authority")
		}
	}
	if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "client-a"); err != nil {
		t.Fatal(err)
	}
	s, _, _ := b.request("GET", "/portal/", nil, "")
	if s == 200 {
		t.Fatal("current revocation ignored")
	}
	// An explicitly provisioned operator is distinct from token groups.
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	b.finish(b.start(b.invite("operator", "operator-client", "operator")))
	s, body, _ := b.request("GET", "/portal/operator?lang=da", nil, "")
	if s != 200 || !strings.Contains(body, "Operatør") {
		t.Fatal("explicit operator denied")
	}
	_, err := b.pool.Exec(b.f.ctx, "UPDATE portal_sessions SET expiry=now()-interval '1 minute'")
	if err != nil {
		t.Fatal(err)
	}
	s, _, _ = b.request("GET", "/portal/", nil, "")
	if s == 200 {
		t.Fatal("expired durable session accepted")
	}
}

func TestBrowserCSRFAndClosedFlags(t *testing.T) {
	b := newBrowserFixture(t)
	token := b.csrf("/portal/sign-in")
	for _, path := range []string{"/portal/sign-in", "/portal/logout"} {
		for _, kind := range []string{"missing token", "bad token", "missing metadata", "unsafe origin"} {
			t.Run(path+kind, func(t *testing.T) {
				form := url.Values{"csrf_token": {token}}
				origin := b.site.URL
				switch kind {
				case "missing token":
					form.Del("csrf_token")
				case "bad token":
					form.Set("csrf_token", "bad")
				case "missing metadata":
					origin = ""
					form.Del("csrf_token")
				case "unsafe origin":
					origin = "https://other.example.invalid"
				}
				s, _, _ := b.request("POST", path, form, origin)
				if s != 403 {
					t.Fatalf("CSRF %s %s accepted: %d", path, kind, s)
				}
			})
		}
	}
	for _, value := range []any{false, nil, "true"} {
		setFlag(t, b.flags, value)
		for _, path := range []string{"/portal/", "/portal/sign-in", "/portal/callback?code=x&state=y", "/portal/operator", "/portal/logout"} {
			s, _, h := b.request("GET", path, nil, "")
			if s != 404 || len(h.Values("Set-Cookie")) != 0 {
				t.Fatalf("closed flag exposed %s: %d", path, s)
			}
		}
		s, _, _ := b.request("POST", "/portal/sign-in", url.Values{"csrf_token": {token}}, b.site.URL)
		if s != 404 {
			t.Fatal("closed flag exposed mutation")
		}
	}
}

func TestBrowserCodeExchangeDoesNotFollowRedirects(t *testing.T) {
	b := newBrowserFixture(t)
	b.redirectExchange = true
	s, _, _ := b.request("GET", b.start(b.invite("client-a", "client-a", "client")), nil, "")
	if s != 400 || b.unexpectedExchanges.Load() != 0 {
		t.Fatalf("code exchange followed a redirect: status=%d unexpected_requests=%d", s, b.unexpectedExchanges.Load())
	}
}

func TestBrowserMissingOrInvalidConfigurationHasNoHandler(t *testing.T) {
	b := newBrowserFixture(t)
	for _, callback := range []string{"http://example.invalid/portal/callback", "https://example.invalid/other", "https://user@example.invalid/portal/callback", "https://example.invalid/portal/callback?return_to=x", "https://example.invalid/portal/callback#x"} {
		if h, err := b.svc.Browser(callback, "synthetic-secret"); err == nil || h != nil {
			t.Errorf("invalid fixed callback admitted: %s", callback)
		}
	}
	for _, svc := range []*Foundation{nil, New(nil, b.f.broker, b.flags.NewClient()), New(b.pool, nil, b.flags.NewClient())} {
		if h, err := svc.Browser(b.site.URL+"/portal/callback", "synthetic-secret"); err == nil || h != nil {
			t.Fatal("incomplete configuration registered routes")
		}
	}
	if h, err := b.svc.Browser(b.site.URL+"/portal/callback", ""); err == nil || h != nil {
		t.Fatal("missing secret registered routes")
	}
	pool, err := pgxpool.New(b.f.ctx, b.pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if h, err := New(pool, b.f.broker, b.flags.NewClient()).Browser(b.site.URL+"/portal/callback", "synthetic-secret"); err == nil || h != nil {
		t.Fatal("invalid durable store registered routes")
	}
}

func TestBrowserTwoClientsKeepTheirServerOwnedWorkspace(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	clientA := b.client
	clientB := *clientA
	clientB.Jar, _ = cookiejar.New(nil)
	b.client = &clientB
	b.mu.Lock()
	b.subject = "client-b"
	b.mu.Unlock()
	b.finish(b.start(b.invite("client-b", "client-b", "client")))
	for _, tc := range []struct {
		client     *http.Client
		own, other string
	}{{clientA, "client-a", "client-b"}, {&clientB, "client-b", "client-a"}} {
		b.client = tc.client
		s, body, _ := b.request("GET", "/portal/?client_id="+tc.other+"&subject="+tc.other+"&role=operator", nil, "")
		if s != 200 || !strings.Contains(body, tc.own) || strings.Contains(body, tc.other) {
			t.Fatal("client selected another identity or workspace")
		}
		s, _, _ = b.request("GET", "/portal/operator", nil, "")
		if s != 403 {
			t.Fatal("broker operator groups or browser role became authority")
		}
	}
}

func TestBrowserErrorsPreserveChosenLanguage(t *testing.T) {
	b := newBrowserFixture(t)
	callback := b.start(b.invite("client-a", "client-a", "client"))
	u, _ := url.Parse(callback)
	q := u.Query()
	q.Set("state", "incorrect")
	u.RawQuery = q.Encode()
	s, body, _ := b.request("GET", u.RequestURI(), nil, "")
	if s != 400 || !strings.Contains(body, `lang="da"`) || !strings.Contains(body, "Login kunne ikke gennemføres") {
		t.Fatal("Danish sign-in failure switched language")
	}
}
