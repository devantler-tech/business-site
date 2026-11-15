package identity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// prefixedLink finds a rendered address that still names the internal "/portal" mount.
var prefixedLink = regexp.MustCompile(`(?i)\b(?:href|action|src|formaction)\s*=\s*"/portal(?:[/?"])`)

// freshKey is a per-render random key (a new one on every page view), not an address.
var freshKey = regexp.MustCompile(`(name="[a-z_]*key" value=")[^"]*"`)

// TestRootPortalPagesMatchTheirSharedHostPages renders every page inventory at the
// root of its own host and on a shared host. With the "/portal" prefix removed the
// two must be identical, so every root address names exactly the internal route
// the shared-host link contract already validates, and none keeps the prefix.
func TestRootPortalPagesMatchTheirSharedHostPages(t *testing.T) {
	unprefix := strings.NewReplacer(`="/portal/`, `="/`, `="/portal?`, `="/?`, `="/portal"`, `="/"`)
	for _, name := range []string{"portal.html", "contact.html", "requests.html", "triage.html", "work-review.html"} {
		t.Run(name, func(t *testing.T) {
			shared, err := renderedLinkPagesFor(name, pages(internalRoot), &browser{})
			if err != nil {
				t.Fatal(err)
			}
			root, err := renderedLinkPagesFor(name, pages(""), &browser{atRoot: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(root) == 0 || len(root) != len(shared) {
				t.Fatalf("rendered %d root pages and %d shared pages", len(root), len(shared))
			}
			for i := range root {
				if m := prefixedLink.FindString(root[i].markup); m != "" {
					t.Fatalf("%s keeps the internal prefix: %s", root[i].uri, m)
				}
				got := freshKey.ReplaceAllString(root[i].markup, `${1}"`)
				if want := freshKey.ReplaceAllString(unprefix.Replace(shared[i].markup), `${1}"`); got != want {
					at := 0
					for at < len(want) && at < len(got) && want[at] == got[at] {
						at++
					}
					from := max(at-80, 0)
					t.Fatalf("%s differs from its shared-host page beyond the prefix near:\n root: %q\n want: %q", root[i].uri, got[from:min(at+80, len(got))], want[from:min(at+80, len(want))])
				}
			}
		})
	}
}

// TestPortalSignsInAtTheRootOfItsHost runs a real sign-in through the two-host
// router: the portal's callback is "/callback", every redirect and link it sends is
// root-based, an old "/portal…" address moves permanently, and the internal mount
// itself is never reachable a second time.
func TestPortalSignsInAtTheRootOfItsHost(t *testing.T) {
	b := newBrowserFixture(t)
	site, err := url.Parse(b.site.URL)
	if err != nil {
		t.Fatal(err)
	}
	b.callbackPath = "/callback"
	h, err := b.svc.Browser(b.callbackURL(), "synthetic-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	public, err := webapp.Load(fstest.MapFS{"index.html": {Data: []byte("English public site")}, "da/index.html": {Data: []byte("Dansk public site")}})
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.handler = public.HostHandler(h, webapp.Hosts{Public: "public.example.invalid", Portal: site.Hostname()})
	b.mu.Unlock()

	status, body, _ := b.request("GET", "/sign-in?lang=da", nil, "")
	if status != 200 || !strings.Contains(body, `action="/sign-in"`) || prefixedLink.MatchString(body) {
		t.Fatalf("root sign-in page: status=%d prefixed=%v", status, prefixedLink.MatchString(body))
	}
	token := b.csrf("/sign-in?lang=da")
	status, body, _ = b.request("POST", "/sign-in", url.Values{"csrf_token": {token}, "invitation": {b.invite("client-a", "client-a", "client")}, "lang": {"da"}}, b.site.URL)
	if status != 200 {
		t.Fatalf("root sign-in start status=%d", status)
	}
	handoff, err := url.Parse(brokerHandoffAddress(t, body))
	if err != nil || handoff.Query().Get("redirect_uri") != b.site.URL+"/callback" {
		t.Fatalf("broker handoff does not name the root callback: %v", err)
	}
	callback := b.authorize(handoff.String())
	if !strings.HasPrefix(callback, "/callback?") {
		t.Fatalf("broker returned to %q, not the root callback", callback)
	}
	status, _, header := b.request("GET", callback, nil, "")
	if status != http.StatusSeeOther || header.Get("Location") != "/?lang=da" {
		t.Fatalf("callback status=%d location=%q; want 303 /?lang=da", status, header.Get("Location"))
	}
	status, body, _ = b.request("GET", "/?lang=da", nil, "")
	if status != 200 || !strings.Contains(body, `action="/logout?lang=da"`) || prefixedLink.MatchString(body) {
		t.Fatalf("root home: status=%d prefixed=%v", status, prefixedLink.MatchString(body))
	}
	for path, want := range map[string]string{"/portal/": "/", "/portal/sign-in?lang=da": "/sign-in?lang=da", "/portal/requests": "/requests"} {
		if status, _, header := b.request("GET", path, nil, ""); status != http.StatusMovedPermanently || header.Get("Location") != want {
			t.Fatalf("old address %s: status=%d location=%q; want 301 %q", path, status, header.Get("Location"), want)
		}
	}
	// The router never hands an old address to the portal, and the portal itself
	// maps any address it is given onto its root, so "/portal/…" is never served twice.
	r, _ := http.NewRequest("GET", b.site.URL+"/portal/sign-in", nil)
	r.Host = site.Host
	if w := recordedStatus(h, r); w != http.StatusNotFound {
		t.Fatalf("portal served its internal mount directly: %d", w)
	}
	token = b.csrf("/?lang=da")
	status, _, header = b.request("POST", "/logout", url.Values{"csrf_token": {token}, "lang": {"da"}}, b.site.URL)
	if status != http.StatusSeeOther || header.Get("Location") != "/sign-in?lang=da" {
		t.Fatalf("logout status=%d location=%q; want 303 /sign-in?lang=da", status, header.Get("Location"))
	}
}

// authorize follows the synthetic broker's authorization redirect, as start does,
// and returns the portal address it sends the browser back to.
func (b *browserFixture) authorize(address string) string {
	b.t.Helper()
	req, err := http.NewRequestWithContext(b.t.Context(), "GET", address, nil)
	if err != nil {
		b.t.Fatal(err)
	}
	client := b.broker.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(res.Body)
		b.t.Fatalf("broker rejected the authorization request: %d %s", res.StatusCode, body)
	}
	u, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		b.t.Fatal(err)
	}
	return u.RequestURI()
}

func recordedStatus(h http.Handler, r *http.Request) int {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// TestStripeWebhookAtTheRoot verifies events at "/stripe/webhook", the address
// registered with Stripe, through the two-host router. The old "/portal/stripe/webhook"
// is not served: a POST there is refused, never redirected (Stripe would not follow it).
func TestStripeWebhookAtTheRoot(t *testing.T) {
	b, _ := cardOrderFixture(t, 299500, 0)
	cardFlags(t, b, true)
	site, err := url.Parse(b.site.URL)
	if err != nil {
		t.Fatal(err)
	}
	b.callbackPath = "/callback"
	h, err := b.svc.Browser(b.callbackURL(), "synthetic-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	public, err := webapp.Load(fstest.MapFS{"index.html": {Data: []byte("English public site")}, "da/index.html": {Data: []byte("Dansk public site")}})
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.handler = public.HostHandler(h, webapp.Hosts{Public: "public.example.invalid", Portal: site.Hostname()})
	b.mu.Unlock()
	event := map[string]any{"id": "cs_unknown", "mode": "payment", "payment_status": "paid", "amount_total": 299500, "currency": "dkk"}
	for _, path := range []string{"/stripe/webhook"} {
		if status := sendEventTo(t, b, path, "evt_forged", "checkout.session.completed", event, "whsec_other"); status != http.StatusBadRequest {
			t.Fatalf("%s: forged event status=%d; want 400 from the verifying handler", path, status)
		}
		if status := sendEventTo(t, b, path, "evt_root", "checkout.session.completed", event, webhookSecret); status == http.StatusNotFound || status/100 == 3 || status == http.StatusMethodNotAllowed {
			t.Fatalf("%s: signed event was not handled: %d", path, status)
		}
	}
	if status := sendEventTo(t, b, "/portal/stripe/webhook", "evt_old", "checkout.session.completed", event, webhookSecret); status != http.StatusMethodNotAllowed {
		t.Fatalf("old webhook address status=%d; want 405", status)
	}
}
