package identity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// browserHandler requires a fully configured, fixed-callback application surface.
func browserHandler(t testing.TB, foundation *Foundation, callback string) http.Handler {
	t.Helper()
	h, err := foundation.Browser(callback, "synthetic-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	public, err := webapp.Load(fstest.MapFS{"index.html": {Data: []byte("English public site")}, "da/index.html": {Data: []byte("Dansk public site")}})
	if err != nil {
		t.Fatal(err)
	}
	return public.Handler(h)
}

type browserGateCase struct {
	state string
	value any
}

// browserGateCases retains distinct SDK values, including absent and wrong-type
// evaluations. These are fixture inputs, not production flag parsing.
func browserGateCases() []browserGateCase {
	return []browserGateCase{{"true", true}, {"false", false}, {"missing", nil}, {"wrong-type", "true"}}
}

// TestCombinedPublicAndPrivateFlagBoundary uses the actual Browser handler and
// SDK provider, not a stub portal. Public pages remain available in either state.
func TestCombinedPublicAndPrivateFlagBoundary(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	for _, tc := range browserGateCases() {
		svc := New(pool, f.broker, flagAPI(t, tc.value).NewClient())
		h := browserHandler(t, svc, "https://app.example.invalid/portal/callback")
		for _, route := range []string{"/", "/da/", "/portal/sign-in?lang=da"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://app.example.invalid"+route, nil))
			want := 200
			if strings.HasPrefix(route, "/portal") && tc.state != "true" {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("state=%s route=%s status=%d want=%d", tc.state, route, w.Code, want)
			}
			if strings.HasPrefix(route, "/portal") && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("combined private response cacheable")
			}
		}
	}
}

// TestBrowserPresentationAssets exercises the actual handlers, bytes and default-off
// boundary. Template files and unknown assets are never a public file server.
func TestBrowserPresentationAssets(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	for _, tc := range browserGateCases() {
		h := browserHandler(t, New(pool, f.broker, flagAPI(t, tc.value).NewClient()), "https://app.example.invalid/portal/callback")
		for name, contentType := range map[string]string{"portal.css": "text/css; charset=utf-8", "portal.js": "text/javascript; charset=utf-8"} {
			data, err := browserFiles.ReadFile("web/" + name)
			if err != nil || len(data) == 0 {
				t.Fatal("actual presentation asset is missing")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://app.example.invalid/portal/assets/"+name, nil))
			if tc.state == "true" {
				if w.Code != http.StatusOK || w.Header().Get("Content-Type") != contentType || w.Body.String() != string(data) {
					t.Fatalf("state=%s asset=%s is not the actual correctly typed asset", tc.state, name)
				}
				if w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("presentation asset permits content sniffing")
				}
			} else if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), string(data)) {
				t.Fatal("default-off gate exposed a presentation asset")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("portal presentation response is cacheable")
			}
		}
		for _, name := range []string{"unknown.css", "portal.html", "contact.html", "requests.html", "triage.html"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://app.example.invalid/portal/assets/"+name, nil))
			if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "{{") {
				t.Fatalf("state=%s asset=%s exposed an unknown asset or template source", tc.state, name)
			}
		}
	}
}

// TestBrowserSignInPage checks rendered native forms and their security headers
// in both supported languages, without bypassing TLS or CSRF handling.
func TestBrowserSignInPage(t *testing.T) {
	f := fixtureBroker(t)
	svc := enabled(t, fixtureDB(t), f)
	handler := http.NotFoundHandler()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	handler = browserHandler(t, svc, srv.URL+"/portal/callback")
	for _, language := range []string{"en", "da"} {
		res, err := srv.Client().Get(srv.URL + "/portal/sign-in?lang=" + language)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		if err := res.Body.Close(); err != nil {
			t.Error("fixture sign-in response cleanup failed")
		}
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("sign-in page status=%d want 200; body=%s", res.StatusCode, body)
		}
		for _, want := range []string{`lang="` + language + `"`, `name="csrf_token"`, `method="post"`, `name="invitation"`} {
			if !strings.Contains(string(body), want) {
				t.Errorf("sign-in page missing %s", want)
			}
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Error("identity page may be cached")
		}
		if got := res.Header.Get("Referrer-Policy"); got != "strict-origin" {
			t.Errorf("form policy=%q; must preserve same-origin POST Origin without leaking paths or tokens", got)
		}
		issuer, err := url.Parse(f.issuer)
		if err != nil {
			t.Fatal(err)
		}
		wantCSP := "default-src 'none'; style-src 'self'; script-src 'self'; form-action 'self' https://" + issuer.Host + "; frame-ancestors 'none'; base-uri 'none'"
		if got := res.Header.Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("security policy=%q want %q", got, wantCSP)
		}
	}
}
