//go:build portalvisual

package identity

import (
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestBrowserVisualFixture is an explicitly synthetic, loopback-only UI runner.
// Its gateway translates HTTP localhost to the TLS application/broker fixture
// so visual testing needs no certificate-warning bypass or trust-store edits.
// This gateway is not production ingress/origin-security evidence: ordinary
// untagged tests exercise the strict HTTPS and origin checks directly.
func TestBrowserVisualFixture(t *testing.T) {
	b := newBrowserFixture(t)
	sharingFlags(t, b, true, true, true, true, true)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-contact-verification", "portal-proposal-acceptance"}, []any{true, true, true, true, true, true, true})
	b.mu.Lock()
	b.claims = map[string]any{"email": "fixture@example.invalid", "email_verified": true}
	b.mu.Unlock()
	for _, subject := range []string{"client-a", "client-b"} {
		triageSignIn(t, b, subject, subject, "client")
		form := requestForm(t, b)
		form.Set("title", "Fictional "+subject+" project")
		form.Set("brief", "A fictional brief for reviewing the private inbox.\nNo work is approved by reading this request.")
		if status, _, _ := b.request("POST", "/portal/requests", form, b.site.URL); status != 303 {
			t.Fatal("synthetic client request failed")
		}
	}
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	inv := b.invite("operator", "fictional-operator", "operator")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://localhost:" + fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && (r.URL.Path == "/portal/sign-in" || r.URL.Path == "/portal/contact/verify") {
			b.mu.Lock()
			b.expectedScope = "openid"
			if r.URL.Path == "/portal/contact/verify" {
				b.expectedScope = "openid email"
			}
			b.mu.Unlock()
		}
		// Fixed fictional broker actors only, confined to this opt-in test gateway.
		// Switching the simulated actor is not real authentication or federation.
		if r.Method == http.MethodGet && (r.URL.Path == "/fixture/client" || r.URL.Path == "/fixture/operator") {
			subject := "operator"
			if r.URL.Path == "/fixture/client" {
				subject = "client-a"
			}
			b.mu.Lock()
			b.subject = subject
			// Both fixed actors are already provisioned once the visual gateway
			// starts. Subsequent sign-in uses current membership, not re-invitation.
			inv = ""
			b.mu.Unlock()
			http.Redirect(w, r, "/portal/sign-in", http.StatusSeeOther)
			return
		}
		base, client, path := b.site.URL, b.site.Client(), r.URL.RequestURI()
		if strings.HasPrefix(r.URL.Path, "/fixture/broker/") {
			base, client, path = b.broker.URL, b.broker.Client(), strings.TrimPrefix(r.URL.RequestURI(), "/fixture/broker")
		}
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		req, err := http.NewRequestWithContext(r.Context(), r.Method, base+path, r.Body)
		if err != nil {
			http.Error(w, "Fixture unavailable", 503)
			return
		}
		req.Header = r.Header.Clone()
		// The HTTP-only visual gateway uses separate fixture cookie names. The
		// actual application's Secure/__Host- cookies remain unchanged and are
		// independently exercised over TLS by the ordinary HTTP regression suite.
		req.Header.Del("Cookie")
		for _, cookie := range r.Cookies() {
			if strings.HasPrefix(cookie.Name, "portal_visual_") {
				cookieCopy := *cookie
				cookieCopy.Name = "__Host-portal_" + strings.TrimPrefix(cookieCopy.Name, "portal_visual_")
				req.AddCookie(&cookieCopy)
			}
		}
		if req.Header.Get("Origin") == origin {
			req.Header.Set("Origin", b.site.URL)
		}
		if req.Header.Get("Referer") != "" {
			req.Header.Set("Referer", b.site.URL+"/portal/sign-in")
		}
		res, err := client.Do(req)
		if err != nil {
			http.Error(w, "Fixture unavailable", 503)
			return
		}
		defer res.Body.Close()
		for key, values := range res.Header {
			if key == "Set-Cookie" || key == "Content-Length" {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		for _, cookie := range res.Cookies() {
			cookieCopy := *cookie
			cookieCopy.Name = strings.Replace(cookieCopy.Name, "__Host-portal_", "portal_visual_", 1)
			cookieCopy.Secure = false
			http.SetCookie(w, &cookieCopy)
		}
		if location := res.Header.Get("Location"); strings.HasPrefix(location, b.broker.URL) {
			u, _ := url.Parse(location)
			w.Header().Set("Location", origin+"/fixture/broker"+u.RequestURI())
		} else if strings.HasPrefix(location, b.site.URL) {
			w.Header().Set("Location", origin+strings.TrimPrefix(location, b.site.URL))
		}
		w.WriteHeader(res.StatusCode)
		if r.Method == http.MethodGet && r.URL.Path == "/portal/sign-in" && res.StatusCode == http.StatusOK {
			// Fictional, disposable gateway only: preload its synthetic invitation
			// so UI evaluation does not expose any token in terminal diagnostics.
			body, readErr := io.ReadAll(io.LimitReader(res.Body, 64*1024+1))
			if readErr != nil || len(body) > 64*1024 {
				return
			}
			b.mu.RLock()
			invitation := inv
			b.mu.RUnlock()
			_, _ = io.WriteString(w, strings.Replace(string(body), `id="invitation"`, `id="invitation" value="`+html.EscapeString(invitation)+`"`, 1))
			return
		}
		_, _ = io.Copy(w, res.Body)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	fmt.Printf("SYNTHETIC_VISUAL_URL=%s/portal/sign-in?lang=da\n", origin)
	// Bounded interactive proof, never an unattended production service.
	select {
	case <-t.Context().Done():
	case <-time.After(10 * time.Minute):
	}
}
