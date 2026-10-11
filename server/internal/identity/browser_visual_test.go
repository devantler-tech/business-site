//go:build portalvisual

package identity

import (
	"fmt"
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
	inv := b.invite("client-a", "client-a", "client")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://localhost:" + fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				copy := *cookie
				copy.Name = "__Host-portal_" + strings.TrimPrefix(copy.Name, "portal_visual_")
				req.AddCookie(&copy)
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
			if key == "Set-Cookie" {
				continue
			}
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		for _, cookie := range res.Cookies() {
			copy := *cookie
			copy.Name = strings.Replace(copy.Name, "__Host-portal_", "portal_visual_", 1)
			copy.Secure = false
			http.SetCookie(w, &copy)
		}
		if location := res.Header.Get("Location"); strings.HasPrefix(location, b.broker.URL) {
			u, _ := url.Parse(location)
			w.Header().Set("Location", origin+"/fixture/broker"+u.RequestURI())
		} else if strings.HasPrefix(location, b.site.URL) {
			w.Header().Set("Location", origin+strings.TrimPrefix(location, b.site.URL))
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()
	fmt.Printf("SYNTHETIC_VISUAL_URL=%s/portal/sign-in?lang=da\nSYNTHETIC_INVITATION=%s\n", origin, inv)
	// Bounded interactive proof, never an unattended production service.
	select {
	case <-t.Context().Done():
	case <-time.After(10 * time.Minute):
	}
}
