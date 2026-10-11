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
func browserHandler(t *testing.T, foundation *Foundation, callback string) http.Handler {
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

// TestCombinedPublicAndPrivateFlagBoundary uses the actual Browser handler and
// SDK provider, not a stub portal. Public pages remain available in either state.
func TestCombinedPublicAndPrivateFlagBoundary(t *testing.T) {
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	for _, state := range []string{"true", "false", "missing", "wrong-type"} {
		var value any
		switch state {
		case "true":
			value = true
		case "false":
			value = false
		case "wrong-type":
			value = "true"
		}
		svc := New(pool, f.broker, flagAPI(t, value).NewClient())
		h := browserHandler(t, svc, "https://app.example.invalid/portal/callback")
		for _, route := range []string{"/", "/da/", "/portal/sign-in?lang=da"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "https://app.example.invalid"+route, nil))
			want := 200
			if strings.HasPrefix(route, "/portal") && state != "true" {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("state=%s route=%s status=%d want=%d", state, route, w.Code, want)
			}
			if strings.HasPrefix(route, "/portal") && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("combined private response cacheable")
			}
		}
	}
}

// TestBrowserSignInPage checks rendered native forms and their security headers
// in both supported languages, without bypassing TLS or CSRF handling.
func TestBrowserSignInPage(t *testing.T) {
	f := fixtureBroker(t)
	svc := enabled(t, fixtureDB(t), f)
	var handler http.Handler = http.NotFoundHandler()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	handler = browserHandler(t, svc, srv.URL+"/portal/callback")
	for _, language := range []string{"en", "da"} {
		res, err := srv.Client().Get(srv.URL + "/portal/sign-in?lang=" + language)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
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
