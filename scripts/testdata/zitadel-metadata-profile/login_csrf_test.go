package csrfprobe

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/csrf"
)

const (
	EndpointResources                     = "/resources/"
	EndpointExternalLoginCallbackFormPost = "/external/callback"
	EndpointSAMLACS                       = "/saml/acs"
)

type instance struct{}

func (instance) SecurityPolicyAllowedOrigins() []string { return nil }

var authz = struct{ GetInstance func(any) instance }{func(any) instance { return instance{} }}
var http_utils = struct {
	DomainContext   func(any) domain
	SetCookiePrefix func(string, bool, string) string
	PrefixHost      string
}{func(any) domain { return domain{} }, func(name string, _ bool, _ string) string { return name }, ""}

type domain struct{}

func (domain) RequestedDomain() string { return "customer.example" }

// The companion source file is extracted unchanged from the real prepared
// interceptor. Doubles provide only unrelated instance/cookie-name policy.
func TestPreparedLoginCSRF(t *testing.T) {
	for _, tc := range []struct {
		name, scheme, origin, referer string
		secure, token                 bool
		want                          int
	}{
		{"https same origin", "https", "https://customer.example", "https://customer.example/login", true, true, 200},
		{"https cross origin", "https", "https://other.example", "https://customer.example/login", true, true, 403},
		{"https missing origin and referer", "https", "", "", true, true, 403},
		{"https missing token", "https", "https://customer.example", "https://customer.example/login", true, false, 403},
		{"explicit plaintext same origin", "http", "http://customer.example", "http://customer.example/login", false, true, 200},
		{"plaintext cross origin", "http", "http://other.example", "http://customer.example/login", false, true, 403},
		{"plaintext missing token", "http", "http://customer.example", "http://customer.example/login", false, false, 403},
		{"TLS policy rejects plaintext origin", "http", "http://customer.example", "http://customer.example/login", true, true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := createCSRFInterceptor("csrf", []byte("01234567890123456789012345678901"), tc.secure,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(csrf.Token(r))) }))
			get := httptest.NewRecorder()
			handler.ServeHTTP(get, httptest.NewRequest("GET", tc.scheme+"://customer.example/login", nil))
			if get.Code != 200 || get.Body.Len() == 0 || len(get.Result().Cookies()) != 1 {
				t.Fatal("token/cookie setup failed")
			}
			cookie := get.Result().Cookies()[0]
			if cookie.Secure != tc.secure {
				t.Fatal("cookie security changed")
			}
			post := httptest.NewRequest("POST", tc.scheme+"://customer.example/login", nil)
			post.Header.Set("Origin", tc.origin)
			post.Header.Set("Referer", tc.referer)
			if tc.token {
				post.Header.Set("X-CSRF-Token", get.Body.String())
			}
			post.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, post)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d", response.Code, tc.want)
			}
		})
	}
}

// Server-side URLs are origin-form, and a TLS-terminating proxy need not set
// Request.TLS. Referer validation must still follow the configured external TLS.
func TestPreparedLoginCSRFReferer(t *testing.T) {
	for _, tc := range []struct {
		name, referer string
		want          int
	}{
		{"valid HTTPS referer", "https://customer.example/login", 200},
		{"foreign referer", "https://other.example/login", 403},
		{"downgraded referer", "http://customer.example/login", 403},
		{"absent referer", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := createCSRFInterceptor("csrf", []byte("01234567890123456789012345678901"), true,
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) }))(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(csrf.Token(r))) }))
			getRequest := httptest.NewRequest("GET", "/login", nil)
			getRequest.Host = "customer.example"
			get := httptest.NewRecorder()
			handler.ServeHTTP(get, getRequest)
			if get.Code != 200 || get.Body.Len() == 0 || len(get.Result().Cookies()) != 1 {
				t.Fatal("token/cookie setup failed")
			}
			post := httptest.NewRequest("POST", "/login", nil)
			post.Host = "customer.example"
			post.Header.Set("Referer", tc.referer)
			post.Header.Set("X-CSRF-Token", get.Body.String())
			post.AddCookie(get.Result().Cookies()[0])
			if post.URL.Scheme != "" || post.TLS != nil || post.Header.Get("Origin") != "" {
				t.Fatal("probe no longer models the proxy request")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, post)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d", response.Code, tc.want)
			}
		})
	}
}
