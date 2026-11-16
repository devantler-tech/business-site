package identity

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// A completed login survives leaving the portal and restarting the browser only
// until its original deadline. Navigation neither exchanges another code nor
// renews the cookie; logout still destroys the durable authority.
func TestPortalReturnPreservesBoundedLogin(t *testing.T) {
	for _, locale := range []struct{ lang, path string }{{"en", "/"}, {"da", "/da/"}} {
		t.Run(locale.lang, func(t *testing.T) {
			b := newBrowserFixture(t)
			b.atRoot()
			tokenExpiry := time.Now().Add(5 * time.Minute).Truncate(time.Second)
			b.configure(map[string]any{"exp": tokenExpiry.Unix()}, false)
			b.invite("client-a", "client-a", "client")
			status, _, headers := b.request("GET", "/sign-in?lang="+locale.lang, nil, "")
			if status != http.StatusSeeOther {
				t.Fatalf("sign-in status=%d", status)
			}
			preauth := sessionCookie(t, headers)
			if preauth.MaxAge != 0 || !preauth.Expires.IsZero() {
				t.Fatal("unfinished login persisted across browser restarts")
			}
			authorization, err := url.Parse(headers.Get("Location"))
			if err != nil || authorization.Query().Get("prompt") != "" || authorization.Query().Get("max_age") != "" {
				t.Fatal("ordinary sign-in prevents identity-provider session reuse")
			}
			status, _, headers = b.request("GET", b.authorize(authorization.String()), nil, "")
			if status != http.StatusSeeOther {
				t.Fatalf("callback status=%d", status)
			}
			cookie := sessionCookie(t, headers)
			if cookie.MaxAge <= 0 || !cookie.Expires.After(time.Now()) || cookie.Expires.After(tokenExpiry.Add(time.Second)) || !cookie.Secure || !cookie.HttpOnly || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("authenticated cookie is not secure, persistent and bounded")
			}
			hash := sha256.Sum256([]byte(cookie.Value))
			storedKey := base64.RawURLEncoding.EncodeToString(hash[:])
			var originalDeadline time.Time
			if err := b.pool.QueryRow(b.f.ctx, "SELECT expiry FROM portal_sessions WHERE token=$1", storedKey).Scan(&originalDeadline); err != nil || originalDeadline.After(tokenExpiry) {
				t.Fatal("durable session exceeds the verified token deadline", err)
			}
			// Restore only the persistent session, as after a browser restart.
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			portal, _ := url.Parse(b.site.URL)
			jar.SetCookies(portal, []*http.Cookie{cookie})
			b.client.Jar = jar
			for range 2 {
				status, body, h := b.request("GET", "/?lang="+locale.lang, nil, "")
				if status != http.StatusOK || !strings.Contains(body, `class="back" href="`+fixtureSite+locale.path+`"`) || !strings.Contains(body, `class="brand" href="`+fixtureSite+locale.path+`"`) {
					t.Fatalf("return did not open the workspace with localized website links: %d", status)
				}
				for _, c := range (&http.Response{Header: h}).Cookies() {
					if c.Name == cookie.Name {
						t.Fatal("read-only return renewed the login")
					}
				}
				public := httptest.NewRecorder()
				b.handler.ServeHTTP(public, httptest.NewRequest("GET", fixtureSite+locale.path, nil))
				if public.Code != http.StatusOK || !strings.Contains(public.Body.String(), "public site") || len(public.Result().Cookies()) != 0 {
					t.Fatal("website navigation did not stay public and leave the session untouched")
				}
			}
			if b.exchanges.Load() != 1 || b.cookie().Value != cookie.Value {
				t.Fatal("return started another login or rotated the session")
			}
			var deadline time.Time
			if err := b.pool.QueryRow(b.f.ctx, "SELECT expiry FROM portal_sessions WHERE token=$1", storedKey).Scan(&deadline); err != nil || !deadline.Equal(originalDeadline) {
				t.Fatal("return extended the durable deadline", err)
			}
			token := b.csrf("/?lang=" + locale.lang)
			if status, _, _ := b.request("POST", "/logout", url.Values{"csrf_token": {token}}, b.site.URL); status != http.StatusOK {
				t.Fatalf("logout status=%d", status)
			}
			jar.SetCookies(portal, []*http.Cookie{cookie})
			if status, _, _ := b.request("GET", "/?lang="+locale.lang, nil, ""); status == http.StatusOK {
				t.Fatal("persisted cookie restored signed-out access")
			}
		})
	}
}

func sessionCookie(t testing.TB, headers http.Header) *http.Cookie {
	t.Helper()
	for _, cookie := range (&http.Response{Header: headers}).Cookies() {
		if cookie.Name == "__Host-portal_session" {
			return cookie
		}
	}
	t.Fatal("session cookie was not issued")
	return nil
}

func TestPortalNavigationReferencesRejectWrongLanguage(t *testing.T) {
	for name := range maintenanceCases {
		pages, err := renderedMaintenancePages(name)
		if err != nil {
			t.Fatal(err)
		}
		for index := len(pages) / 2; index < len(pages); index++ {
			bad := maintenanceAlter(t, pages[index].markup, func(n *html.Node) bool {
				return n.Data == "a" && linkAttribute(n, "class") == "brand"
			}, func(n *html.Node) {
				for i := range n.Attr {
					if n.Attr[i].Key == "href" {
						n.Attr[i].Val = "/"
					}
				}
			})
			if checkMaintenanceInventoryPage(name, index, bad, false) == nil {
				t.Fatal("Danish page accepted an English website link")
			}
		}
	}
}
