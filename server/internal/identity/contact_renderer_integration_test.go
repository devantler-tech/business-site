//go:build rendererintegration

package identity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/contactrenderer"
	"github.com/open-feature/go-sdk/openfeature"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func contactRendererFlags(value any) map[string]memprovider.InMemoryFlag {
	flags := map[string]memprovider.InMemoryFlag{}
	for key, v := range map[string]any{"portal-identity": true, "portal-contact-verification": true, "portal-contact-renderer": value} {
		if v != nil {
			flags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "value", Variants: map[string]any{"value": v}}
		}
	}
	return flags
}

// renderedContactFixture uses the actual compiled worker and the existing real
// restricted PostgreSQL/TLS broker fixture. No HTML or auth result is substituted.
func renderedContactFixture(t *testing.T, value any) (*browserFixture, *contactrenderer.Supervisor) {
	t.Helper()
	b := newBrowserFixture(t)
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(contactRendererFlags(value))); err != nil {
		t.Fatal(err)
	}
	s, err := contactrenderer.New(os.Getenv("RENDERER_TEST_NODE"), os.Getenv("RENDERER_TEST_ARTIFACT"), b.flags.NewClient())
	if err != nil {
		t.Fatal("actual Node executable and compiled worker are required")
	}
	t.Cleanup(s.Close)
	h, err := b.svc.DeveloperContactBrowser(b.site.URL+"/portal/callback", "synthetic-client-secret", s)
	if err != nil {
		t.Fatal("developer composition failed")
	}
	b.mu.Lock()
	b.handler = h
	b.mu.Unlock()
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	return b, s
}

func TestCompiledContactHTTP(t *testing.T) {
	b, _ := renderedContactFixture(t, true)
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
		if s != 200 || !strings.Contains(body, `Client &lt;script>fictional&lt;/script>`) || strings.Contains(body, `<script>fictional`) || !strings.Contains(body, `Company &quot;quoted&quot;`) || !strings.Contains(body, `action="/portal/contact/verify"`) {
			t.Fatal("saved compiled form lost escaping or confirmation state", s)
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
		t.Fatal("confirmed compiled state is incorrect", s)
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
	other := *a
	other.Jar, _ = cookiejar.New(nil)
	b.client = &other
	b.mu.Lock()
	b.subject = "client-b"
	b.mu.Unlock()
	b.finish(b.start(b.invite("client-b", "client-b", "client")))
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
			t.Fatal("actual child crossed client scope")
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

// contactWaitProvider holds an actual post-render SDK evaluation; it does not
// replace the compiled renderer, HTTP handler, session store or database result.
type contactWaitProvider struct {
	openfeature.FeatureProvider
	key              string
	blockAt          int32
	reads            atomic.Int32
	denied           atomic.Pointer[string]
	blocked, release chan struct{}
}

func (p *contactWaitProvider) BooleanEvaluation(ctx context.Context, key string, fallback bool, values openfeature.FlattenedContext) openfeature.BoolResolutionDetail {
	if key == p.key && p.reads.Add(1) == p.blockAt {
		close(p.blocked)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	}
	if denied := p.denied.Load(); denied != nil && *denied == key {
		return openfeature.BoolResolutionDetail{Value: false}
	}
	return p.FeatureProvider.BooleanEvaluation(ctx, key, fallback, values)
}

func TestCompiledContactAuthority(t *testing.T) {
	for _, checkpoint := range []int32{2, 3} {
		for _, change := range []string{"unchanged", "durable logout", "deleted session", "expired session", "revoked membership", "operator role", "client rebind", "identity closed", "renderer closed", "verification closed", "cancelled"} {
			t.Run(fmt.Sprintf("checkpoint-%d/%s", checkpoint, change), func(t *testing.T) {
				b, _ := renderedContactFixture(t, true)
				if s, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "0", "Private Fictional Owner", "owner@example.invalid", ""), b.site.URL); s != 303 {
					t.Fatal("fixture save failed")
				}
				logoutCSRF := b.csrf("/portal/contact")
				p := &contactWaitProvider{FeatureProvider: memprovider.NewInMemoryProvider(contactRendererFlags(true)), key: "portal-contact-renderer", blockAt: checkpoint, blocked: make(chan struct{}), release: make(chan struct{})}
				if err := b.flags.SetProviderAndWait(t.Context(), p); err != nil {
					t.Fatal(err)
				}
				release := onceRelease(p.release)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				// A fixture-only request wrapper cancels the actual server context.
				if change == "cancelled" {
					b.mu.Lock()
					h := b.handler
					b.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r.WithContext(ctx)) })
					b.mu.Unlock()
				}
				type response struct {
					status int
					body   string
				}
				result := make(chan response, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					s, body, _ := b.request("GET", "/portal/contact", nil, "")
					result <- response{s, body}
				}()
				defer func() {
					release()
					cancel()
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("rendering request did not finish")
					}
				}()
				select {
				case <-p.blocked:
				case <-time.After(5 * time.Second):
					t.Fatal("actual post-render SDK checkpoint missing")
				}
				query := ""
				switch change {
				case "durable logout":
					if status, _, _ := b.request("POST", "/portal/logout", url.Values{"csrf_token": {logoutCSRF}}, b.site.URL); status != 303 {
						t.Fatal("actual durable logout failed", status)
					}
				case "deleted session":
					query = `DELETE FROM portal_sessions`
				case "expired session":
					query = `UPDATE portal_sessions SET expiry=now()-interval '1 minute'`
				case "revoked membership":
					query = `UPDATE portal_memberships SET revoked=true WHERE subject='client-a'`
				case "operator role":
					query = `UPDATE portal_memberships SET role='operator' WHERE subject='client-a'`
				case "client rebind":
					query = `UPDATE portal_memberships SET client_id='client-b' WHERE subject='client-a'`
				case "identity closed":
					key := "portal-identity"
					p.denied.Store(&key)
				case "renderer closed":
					key := "portal-contact-renderer"
					p.denied.Store(&key)
				case "verification closed":
					key := "portal-contact-verification"
					p.denied.Store(&key)
				case "cancelled":
					cancel()
				}
				if query != "" {
					if _, err := b.pool.Exec(t.Context(), query); err != nil {
						t.Fatal("fixture change failed", err)
					}
				}
				release()
				select {
				case got := <-result:
					if change == "unchanged" {
						if got.status != 200 || !strings.Contains(got.body, "Private Fictional Owner") {
							t.Fatal("unchanged actual render failed")
						}
					} else if got.status != 503 || strings.Contains(got.body, "Private Fictional Owner") || strings.Contains(got.body, "owner@example.invalid") {
						t.Fatalf("%s released private output or wrong failure: %d", change, got.status)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("rendered HTTP response missing")
				}
			})
		}
	}
}

// Initial opt-out includes a real provider wait, so it needs the same durable
// recheck before the retained Go response. The positive control stays usable.
func TestCompiledContactOptOutAuthority(t *testing.T) {
	for _, change := range []bool{false, true} {
		b, _ := renderedContactFixture(t, true)
		p := &contactWaitProvider{FeatureProvider: memprovider.NewInMemoryProvider(contactRendererFlags(false)), key: "portal-contact-renderer", blockAt: 1, blocked: make(chan struct{}), release: make(chan struct{})}
		if err := b.flags.SetProviderAndWait(t.Context(), p); err != nil {
			t.Fatal(err)
		}
		release := onceRelease(p.release)
		type response struct {
			status int
			body   string
		}
		result := make(chan response, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s, body, _ := b.request("GET", "/portal/contact", nil, "")
			result <- response{s, body}
		}()
		defer func() {
			release()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("opt-out request did not finish")
			}
		}()
		select {
		case <-p.blocked:
		case <-time.After(5 * time.Second):
			t.Fatal("initial opt-out did not wait on real SDK")
		}
		if change {
			if _, err := b.pool.Exec(t.Context(), "DELETE FROM portal_sessions"); err != nil {
				t.Fatal(err)
			}
		}
		release()
		select {
		case got := <-result:
			if change {
				if got.status != 503 || strings.Contains(got.body, `class="contact-form"`) {
					t.Fatal("opt-out bypassed durable session loss")
				}
			} else if got.status != 200 || !strings.Contains(got.body, `class="contact-form"`) {
				t.Fatal("unchanged opt-out lost Go response")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("opt-out result missing")
		}
	}
}

func TestCompiledContactFallbackAndFailure(t *testing.T) {
	for _, value := range []any{nil, false, "true", true} {
		b, supervisor := renderedContactFixture(t, value)
		s, body, _ := b.request("GET", "/portal/contact", nil, "")
		if s != 200 || !strings.Contains(body, `name="revision" value="0"`) {
			t.Fatal("closed/true composition lost native form", s)
		}
		if value != true && strings.Contains(body, "<!--[") {
			t.Fatal("closed gate did not retain Go presentation")
		}
		supervisor.Close()
	}
	b, _ := renderedContactFixture(t, true)
	// A real failing Node child must not silently fall back to successful Go HTML.
	broken := filepath.Join(t.TempDir(), "broken.mjs")
	if err := os.WriteFile(broken, []byte(`throw new Error("fictional private diagnostic");`), 0600); err != nil {
		t.Fatal(err)
	}
	supervisor, err := contactrenderer.New(os.Getenv("RENDERER_TEST_NODE"), broken, b.flags.NewClient())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(supervisor.Close)
	h, err := b.svc.DeveloperContactBrowser(b.site.URL+"/portal/callback", "synthetic-client-secret", supervisor)
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.handler = h
	b.mu.Unlock()
	if s, body, _ := b.request("GET", "/portal/contact", nil, ""); s != 503 || strings.Contains(body, "fictional private diagnostic") || strings.Contains(body, `class="contact-form"`) {
		t.Fatal("enabled child failure exposed diagnostics or success fallback", s)
	}
}
