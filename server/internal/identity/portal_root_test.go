package identity

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/webapp"
)

// fixtureSite is the public website a root-mounted fixture portal links back to.
const fixtureSite = "https://public.example.invalid"

// prefixedLink finds a rendered address that still names the internal "/portal" mount.
var prefixedLink = regexp.MustCompile(`(?i)\b(?:href|action|src|formaction)\s*=\s*"/portal(?:[/?"])`)

// freshKey is a per-render random key (a new one on every page view), not an address.
var freshKey = regexp.MustCompile(`(name="[a-z_]*key" value=")[^"]*"`)

// TestRootPortalPagesMatchTheirSharedHostPages renders every page inventory at the
// root of its own host and on a shared host. With the "/portal" prefix removed the
// two must be identical, so every root address names exactly the internal route
// the shared-host link contract already validates, and none keeps the prefix.
func TestRootPortalPagesMatchTheirSharedHostPages(t *testing.T) {
	// A shared host links the public site as "/"; a root portal names its origin.
	stripPrefix := strings.NewReplacer(`href="/"`, `href="`+fixtureSite+`/"`, `="/portal/`, `="/`, `="/portal?`, `="/?`, `="/portal"`, `="/"`)
	for _, name := range []string{"portal.html", "contact.html", "requests.html", "triage.html", "work-review.html"} {
		t.Run(name, func(t *testing.T) {
			shared, err := renderedLinkPagesFor(name, pages(internalRoot, ""), &browser{})
			if err != nil {
				t.Fatal(err)
			}
			root, err := renderedLinkPagesFor(name, pages("", fixtureSite), &browser{atRoot: true})
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
				if want := freshKey.ReplaceAllString(stripPrefix.Replace(shared[i].markup), `${1}"`); got != want {
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
	h := b.atRoot()

	b.invite("client-a", "client-a", "client")
	status, _, header := b.request("GET", "/sign-in?lang=da", nil, "")
	handoff, err := url.Parse(header.Get("Location"))
	if status != http.StatusSeeOther || err != nil || handoff.Query().Get("redirect_uri") != b.site.URL+"/callback" {
		t.Fatalf("root sign-in did not go straight to the broker with the root callback: status=%d %v", status, err)
	}
	callback := b.authorize(handoff.String())
	if !strings.HasPrefix(callback, "/callback?") {
		t.Fatalf("broker returned to %q, not the root callback", callback)
	}
	status, _, header = b.request("GET", callback, nil, "")
	if status != http.StatusSeeOther || header.Get("Location") != "/?lang=da" {
		t.Fatalf("callback status=%d location=%q; want 303 /?lang=da", status, header.Get("Location"))
	}
	status, body, _ := b.request("GET", "/?lang=da", nil, "")
	if status != 200 || !strings.Contains(body, `action="/logout?lang=da"`) || !strings.Contains(body, `class="back" href="`+fixtureSite+`/"`) || prefixedLink.MatchString(body) {
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
	if w := recordedStatus(h, r); w != http.StatusNotFound {
		t.Fatalf("portal served its internal mount directly: %d", w)
	}
	token := b.csrf("/?lang=da")
	// Without an upstream logout, sign-out lands on the public site.
	status, body, _ = b.request("POST", "/logout", url.Values{"csrf_token": {token}, "lang": {"da"}}, b.site.URL)
	if status != http.StatusOK || brokerHandoffRefreshAddress(t, body) != fixtureSite+"/" {
		t.Fatalf("logout status=%d did not land on the public site", status)
	}
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
	b.atRoot()
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

// atRoot moves the fixture's portal to the root of its own host, behind the real
// two-host router, and returns the portal handler itself.
func (b *browserFixture) atRoot() http.Handler {
	b.t.Helper()
	site, err := url.Parse(b.site.URL)
	if err != nil {
		b.t.Fatal(err)
	}
	b.callbackPath = "/callback"
	if err := b.svc.LinkPublicSite(fixtureSite); err != nil {
		b.t.Fatal(err)
	}
	h := portalHandler(b.t, b.svc, b.callbackURL())
	b.mu.Lock()
	b.handler = fixturePublic(b.t).HostHandler(h, webapp.Hosts{Public: "public.example.invalid", Portal: site.Hostname()})
	b.mu.Unlock()
	return h
}

// TestRootPortalNeedsItsPublicSite refuses a root-mounted portal that cannot link
// back to the public website, and accepts only a bare HTTPS origin for it.
func TestRootPortalNeedsItsPublicSite(t *testing.T) {
	b := newBrowserFixture(t)
	svc := New(b.pool, b.f.broker, b.flags.NewClient())
	if h, err := svc.Browser(b.site.URL+"/callback", "synthetic-client-secret"); err == nil || h != nil {
		t.Fatal("root portal accepted without a public site")
	}
	for _, origin := range []string{"", "http://public.example.invalid", "https://public.example.invalid/", "https://public.example.invalid/da/", "https://user@public.example.invalid", "https://public.example.invalid?x=1"} {
		if svc.LinkPublicSite(origin) == nil {
			t.Fatalf("public site %q accepted", origin)
		}
	}
	if err := svc.LinkPublicSite(fixtureSite); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Browser(b.site.URL+"/callback", "synthetic-client-secret"); err != nil {
		t.Fatalf("root portal with its public site refused: %v", err)
	}
}
