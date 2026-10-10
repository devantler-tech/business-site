package identity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func browserHandler(t *testing.T, foundation *Foundation, callback string) http.Handler {
	t.Helper()
	h, err := foundation.Browser(callback, "synthetic-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

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
	}
}
