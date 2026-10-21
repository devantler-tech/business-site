//go:build contactintegration

package identity

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGoContactHTTP exercises the retained native form, exact integer revisions
// and concurrent independent actors through real TLS and restricted storage.
func TestGoContactHTTP(t *testing.T) {
	b := newBrowserFixture(t)
	contactVerificationFlags(t, b, true)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	for _, lang := range []string{"en", "da"} {
		s, body, headers := b.request("GET", "/portal/contact?lang="+lang, nil, "")
		for _, want := range []string{`<html lang="` + lang + `">`, `name="revision" value="0"`, `method="post" action="/portal/contact"`, `noindex,nofollow`, `<footer>Devantler Tech</footer>`} {
			if s != 200 || !strings.Contains(body, want) || headers.Get("Cache-Control") != "no-store" {
				t.Fatalf("actual empty %s response missing %s (status=%d)", lang, want, s)
			}
		}
	}
	token := b.csrf("/portal/contact")
	if s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", `Client <script>fictional</script>`, "a@example.invalid", `Company "quoted"`), b.site.URL); s != 303 {
		t.Fatal("native save failed")
	}
	for _, lang := range []string{"en", "da"} {
		s, body, _ := b.request("GET", "/portal/contact?lang="+lang, nil, "")
		if s != 200 || !strings.Contains(body, `Client &lt;script&gt;fictional&lt;/script&gt;`) || strings.Contains(body, `<script>fictional`) || !strings.Contains(body, `Company &#34;quoted&#34;`) || !strings.Contains(body, `action="/portal/contact/verify"`) {
			t.Fatal("saved Go form lost escaping or confirmation state", s)
		}
		form := contactForm(b.csrf("/portal/contact"), "1", "Recoverable Fictional Draft", "bad", "")
		form.Set("lang", lang)
		s, body, _ = b.request("POST", "/portal/contact", form, b.site.URL)
		if s != 400 || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `value="Recoverable Fictional Draft"`) || !strings.Contains(body, `name="revision" value="1"`) || strings.Contains(body, `action="/portal/contact/verify"`) {
			t.Fatal("native invalid recovery changed", s)
		}
	}
	for _, lang := range []string{"en", "da"} {
		b.configure(map[string]any{"email": "a@example.invalid", "email_verified": false}, false)
		callback := verificationStart(t, b, "1")
		s, body, _ := b.request("GET", callback+"&lang="+lang, nil, "")
		if s != 400 || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, `name="revision" value="1"`) || !strings.Contains(body, `<html lang="`+lang+`">`) {
			t.Fatal("actual failed confirmation lost localized recovery", s)
		}
	}
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	if s, _, _ := b.request("GET", verificationStart(t, b, "1"), nil, ""); s != 303 {
		t.Fatal("native exact saved-contact confirmation failed", s)
	}
	if s, body, _ := b.request("GET", "/portal/contact", nil, ""); s != 200 || strings.Contains(body, `action="/portal/contact/verify"`) || !strings.Contains(body, "Confirmed by your sign-in provider") {
		t.Fatal("confirmed Go state is incorrect", s)
	}
	b.mu.Lock()
	b.expectedScope = ""
	b.mu.Unlock()
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_contacts SET revision=9007199254740993 WHERE client_id='client-a'`); err != nil {
		t.Fatal(err)
	}
	s, body, _ := b.request("GET", "/portal/contact", nil, "")
	if s != 200 || !strings.Contains(body, `name="revision" value="9007199254740993"`) {
		t.Fatal("revision lost decimal precision")
	}
	s, body, _ = b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "1", "Stale Fictional Draft", "stale@example.invalid", ""), b.site.URL)
	if s != 409 || !strings.Contains(body, `role="alert"`) {
		t.Fatal("native stale edit no longer conflicts")
	}
	// Current revision is still saved by Go; no JavaScript form execution is used.
	if s, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "9007199254740993", "Alice Fictional", "a@example.invalid", ""), b.site.URL); s != 303 {
		t.Fatal("precise revision could not save")
	}
	a := b.client
	other := independentContactClient(t, b, "client-b")
	if s, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "0", "Bob Fictional", "b@example.invalid", ""), b.site.URL); s != 303 {
		t.Fatal("second client save failed")
	}
	for _, actor := range []struct {
		client       *http.Client
		own, foreign string
		revision     string
	}{{a, "Alice Fictional", "Bob Fictional", "9007199254740994"}, {&other, "Bob Fictional", "Alice Fictional", "1"}} {
		b.client = actor.client
		s, body, _ = b.request("GET", "/portal/contact?client_id=client-b&subject=client-b&role=operator", nil, "")
		if s != 200 || !strings.Contains(body, actor.own) || strings.Contains(body, actor.foreign) {
			t.Fatal("actual Go handler crossed client scope")
		}
		form := contactForm(b.csrf("/portal/contact"), actor.revision, "Forged", "forged@example.invalid", "")
		form.Set("client_id", "client-b")
		if s, _, _ := b.request("POST", "/portal/contact", form, b.site.URL); s != 400 {
			t.Fatal("browser-selected client accepted")
		}
		form.Del("client_id")
		form.Set("csrf_token", "foreign")
		if s, _, _ := b.request("POST", "/portal/contact", form, b.site.URL); s != 403 {
			t.Fatal("forged CSRF accepted")
		}
		form.Set("csrf_token", b.csrf("/portal/contact"))
		if s, _, _ := b.request("POST", "/portal/contact", form, "https://foreign.example.invalid"); s != 403 {
			t.Fatal("foreign origin accepted")
		}
	}
	operator := *a
	operator.Jar, _ = cookiejar.New(nil)
	b.client = &operator
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	b.finish(b.start(b.invite("operator", "operator-space", "operator")))
	if s, body, _ := b.request("GET", "/portal/contact", nil, ""); s != 403 || strings.Contains(body, "Alice Fictional") || strings.Contains(body, "Bob Fictional") {
		t.Fatal("operator gained contact override")
	}
	// The actual three actors read concurrently with independent cookie jars.
	type result struct {
		status       int
		body         string
		err          error
		own, foreign string
	}
	results := make(chan result, 3)
	start := make(chan struct{})
	var workers sync.WaitGroup
	defer workers.Wait()
	for _, actor := range []struct {
		client       *http.Client
		path         string
		own, foreign string
	}{{a, "/portal/contact", "Alice Fictional", "Bob Fictional"}, {&other, "/portal/contact?lang=da", "Bob Fictional", "Alice Fictional"}, {&operator, "/portal/operator", "Operator", "Fictional"}} {
		workers.Go(func() {
			<-start
			res, err := actor.client.Get(b.site.URL + actor.path)
			if err != nil {
				results <- result{err: err}
				return
			}
			body, readErr := io.ReadAll(res.Body)
			closeErr := res.Body.Close()
			if readErr == nil {
				readErr = closeErr
			}
			results <- result{res.StatusCode, string(body), readErr, actor.own, actor.foreign}
		})
	}
	close(start)
	for range 3 {
		select {
		case got := <-results:
			if got.err != nil || got.status != 200 || !strings.Contains(got.body, got.own) || strings.Contains(got.body, got.foreign) {
				t.Fatal("concurrent actors lost isolation", got.status, got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent actor did not finish")
		}
	}
}
