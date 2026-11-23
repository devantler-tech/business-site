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
	return fixturePublic(t).Handler(portalHandler(t, foundation, callback))
}

// portalHandler is the configured portal alone, before any site routing.
func portalHandler(t testing.TB, foundation *Foundation, callback string) http.Handler {
	t.Helper()
	h, err := foundation.Browser(callback, "synthetic-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// fixturePublic is the synthetic bilingual public site the portal shares a runtime with.
func fixturePublic(t testing.TB) *webapp.Artifact {
	t.Helper()
	public, err := webapp.Load(fstest.MapFS{"index.html": {Data: []byte("English public site")}, "da/index.html": {Data: []byte("Dansk public site")}})
	if err != nil {
		t.Fatal(err)
	}
	return public
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
			if strings.HasPrefix(route, "/portal") {
				// Enabled sign-in redirects straight to the broker.
				want = http.StatusSeeOther
				if tc.state != "true" {
					want = 404
				}
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

// wantPresentationAssets is the exact public asset set and each file's type,
// written out independently of presentationAssets.
var wantPresentationAssets = []struct{ name, contentType string }{
	{"icon.png", "image/png"},
	{"logo-dark.svg", "image/svg+xml"},
	{"logo-light.svg", "image/svg+xml"},
	{"portal.css", "text/css; charset=utf-8"},
	{"portal.js", "text/javascript; charset=utf-8"},
	{"trustpilot-link.css", "text/css; charset=utf-8"},
}

// TestBrowserPresentationAssets exercises the actual handlers, bytes and default-off
// boundary. Template files and unknown assets are never a public file server.
func TestBrowserPresentationAssets(t *testing.T) {
	if len(presentationAssets) != len(wantPresentationAssets) {
		t.Fatalf("served presentation assets = %d, want exactly %d", len(presentationAssets), len(wantPresentationAssets))
	}
	for _, asset := range wantPresentationAssets {
		if presentationAssets[asset.name] != asset.contentType {
			t.Fatalf("asset %s is served as %q, want %q", asset.name, presentationAssets[asset.name], asset.contentType)
		}
	}
	f := fixtureBroker(t)
	pool := fixtureDB(t)
	for _, tc := range browserGateCases() {
		h := browserHandler(t, New(pool, f.broker, flagAPI(t, tc.value).NewClient()), "https://app.example.invalid/portal/callback")
		for _, asset := range wantPresentationAssets {
			name, contentType := asset.name, asset.contentType
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

// TestBrowserSignInPage checks that sign-in redirects straight to the broker in
// both supported languages, with the portal's security headers, over real TLS.
func TestBrowserSignInPage(t *testing.T) {
	f := fixtureBroker(t)
	svc := enabled(t, fixtureDB(t), f)
	handler := http.NotFoundHandler()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	defer srv.Close()
	handler = browserHandler(t, svc, srv.URL+"/portal/callback")
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, language := range []string{"en", "da"} {
		res, err := client.Get(srv.URL + "/portal/sign-in?lang=" + language)
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
		if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), f.broker.endpoint.AuthURL+"?") {
			t.Fatalf("sign-in did not redirect straight to the broker: status=%d location=%s", res.StatusCode, res.Header.Get("Location"))
		}
		// There is no portal page before the login, so no form and no invitation code.
		if strings.Contains(string(body), "<form") || strings.Contains(string(body), `name="invitation"`) {
			t.Error("sign-in still renders a portal form")
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
		wantCSP := "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; form-action 'self' https://" + issuer.Host + "; frame-ancestors 'none'; base-uri 'none'"
		if got := res.Header.Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("security policy=%q want %q", got, wantCSP)
		}
	}
}

func TestTrustpilotLinkIsConfinedToClientHome(t *testing.T) {
	b := &browser{foundation: &Foundation{}}
	const reviewPage = "https://www.trustpilot.com/evaluate/devantler.tech"
	for _, kind := range []string{"home", "operator", "no-access"} {
		for _, lang := range []string{"en", "da"} {
			r := httptest.NewRequest("GET", "/portal/?lang="+lang, nil)
			w := httptest.NewRecorder()
			w.Header().Set("Content-Security-Policy", "default-src 'none'")
			b.render(w, r, http.StatusOK, kind, "synthetic-workspace", "")
			body := w.Body.String()
			if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
				t.Errorf("%s %s CSP = %q", kind, lang, got)
			}
			if kind != "home" {
				if strings.Contains(body, "trustpilot.com") {
					t.Fatalf("%s %s exposes a customer footer link", kind, lang)
				}
				continue
			}
			if strings.Count(body, "trustpilot.com") != 1 || !strings.Contains(body, `href="`+reviewPage+`" rel="noreferrer"`) ||
				strings.Contains(body, "<iframe") || strings.Contains(body, "data-review-consent") {
				t.Errorf("%s client home must render one direct review link", lang)
			}
		}
	}
}
