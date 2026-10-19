package identity

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// contactForm builds a synthetic native-form submission for real TLS controls
// of record scoping, expected revisions and CSRF-protected private writes.
func contactForm(token, revision, name, email, company string) url.Values {
	return url.Values{"csrf_token": {token}, "revision": {revision}, "name": {name}, "email": {email}, "company": {company}, "lang": {"en"}}
}

// TestContactProfilePersistsWithoutGrantingAuthority reconnects the real store
// and verifies that a saved, unverified address never grants operator access.
func TestContactProfilePersistsWithoutGrantingAuthority(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token := b.csrf("/portal/contact?lang=en")
	s, _, h := b.request("POST", "/portal/contact", contactForm(token, "0", "A Client", "a@example.invalid", ""), b.site.URL)
	if s != 303 || h.Get("Location") != "/portal/contact?lang=en" {
		t.Fatalf("contact save=%d location=%s", s, h.Get("Location"))
	}
	b.reconnect(context.Background())
	s, body, h := b.request("GET", "/portal/contact?lang=en", nil, "")
	if s != 200 || !strings.Contains(body, `value="A Client"`) || !strings.Contains(body, `value="a@example.invalid"`) || !strings.Contains(body, `name="revision" value="1"`) || !strings.Contains(body, "not verified") || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("durable contact missing: %d %s", s, body)
	}
	s, _, _ = b.request("GET", "/portal/operator", nil, "")
	if s != 403 {
		t.Fatal("profile granted operator access")
	}
	// An email change is still self-declared, never a verified identity link.
	token = b.csrf("/portal/contact?lang=en")
	s, _, _ = b.request("POST", "/portal/contact", contactForm(token, "1", "A Client", "changed@example.invalid", "Example Company"), b.site.URL)
	if s != 303 {
		t.Fatalf("email update=%d", s)
	}
	_, body, _ = b.request("GET", "/portal/contact?lang=en", nil, "")
	if !strings.Contains(body, "changed@example.invalid") || !strings.Contains(body, "not verified") {
		t.Fatal("email change lost explicit pending state")
	}
}

// independentContactClient creates another maintained session with a separate jar.
func independentContactClient(t *testing.T, b *browserFixture, subject string) http.Client {
	t.Helper()
	client := *b.client
	client.Jar, _ = cookiejar.New(nil)
	b.client = &client
	b.mu.Lock()
	b.subject = subject
	b.mu.Unlock()
	b.finish(b.start(b.invite(subject, subject, "client")))
	return client
}

// TestContactTwoClientsCannotSelectAnotherRecord exercises two real sessions
// and rejects browser-selected record IDs and roles without changing either record.
func TestContactTwoClientsCannotSelectAnotherRecord(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	a := b.client
	token := b.csrf("/portal/contact")
	s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", "Alice Fictional", "a@example.invalid", ""), b.site.URL)
	if s != 303 {
		t.Fatalf("save A=%d", s)
	}
	other := independentContactClient(t, b, "client-b")
	token = b.csrf("/portal/contact")
	s, _, _ = b.request("POST", "/portal/contact", contactForm(token, "0", "Bob Fictional", "b@example.invalid", ""), b.site.URL)
	if s != 303 {
		t.Fatalf("save B=%d", s)
	}
	for _, tc := range []struct {
		client     *http.Client
		own, other string
	}{{a, "Alice Fictional", "Bob Fictional"}, {&other, "Bob Fictional", "Alice Fictional"}} {
		b.client = tc.client
		s, body, _ := b.request("GET", "/portal/contact?client_id=client-b&subject=client-b&role=operator&lang=en", nil, "")
		if s != 200 || !strings.Contains(body, tc.own) || strings.Contains(body, tc.other) {
			t.Fatal("URL selected a foreign record")
		}
		token = b.csrf("/portal/contact")
		form := contactForm(token, "1", "Changed", "other@example.invalid", "")
		form.Set("client_id", "client-b")
		form.Set("role", "operator")
		s, _, _ = b.request("POST", "/portal/contact", form, b.site.URL)
		if s != 400 {
			t.Fatalf("forged fields accepted=%d", s)
		}
		_, body, _ = b.request("GET", "/portal/contact?lang=en", nil, "")
		if !strings.Contains(body, tc.own) {
			t.Fatal("rejected form changed record")
		}
	}
}

// TestContactInvalidFormsNeverSave rejects malformed, oversized, forged and
// cross-origin forms through the actual secured handler without storing a row.
func TestContactInvalidFormsNeverSave(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	for _, kind := range []string{"empty name", "long name", "control name", "invalid email", "display-name email", "control email", "long company", "duplicate name", "unknown field", "negative revision", "malformed revision", "oversized body", "missing csrf", "wrong csrf", "cross origin", "absent metadata"} {
		t.Run(kind, func(t *testing.T) {
			token := b.csrf("/portal/contact")
			form := contactForm(token, "0", "Valid Name", "a@example.invalid", "")
			origin := b.site.URL
			want := 400
			switch kind {
			case "empty name":
				form.Set("name", " ")
			case "long name":
				form.Set("name", strings.Repeat("a", 121))
			case "control name":
				form.Set("name", "Name\nSecond")
			case "invalid email":
				form.Set("email", "bad")
			case "display-name email":
				form.Set("email", "Other <a@example.invalid>")
			case "control email":
				form.Set("email", "a@example.invalid\r\n")
			case "long company":
				form.Set("company", strings.Repeat("x", 161))
			case "duplicate name":
				form.Add("name", "Other")
			case "unknown field":
				form.Set("email_verified", "true")
			case "negative revision":
				form.Set("revision", "-1")
			case "malformed revision":
				form.Set("revision", "bad")
			case "oversized body":
				form.Set("name", strings.Repeat("x", 9000))
				// The existing 8 KiB boundary makes nosurf reject the form
				// before the contact handler can parse it. No data is saved.
				want = 403
			case "missing csrf":
				form.Del("csrf_token")
				want = 403
			case "wrong csrf":
				form.Set("csrf_token", "bad")
				want = 403
			case "cross origin":
				origin = "https://other.example.invalid"
				want = 403
			case "absent metadata":
				origin = ""
				form.Del("csrf_token")
				want = 403
			}
			s, _, _ := b.request("POST", "/portal/contact", form, origin)
			if s != want {
				t.Fatalf("invalid %s=%d want=%d", kind, s, want)
			}
			var n int
			if err := b.pool.QueryRow(b.f.ctx, "SELECT count(*) FROM portal_contacts").Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatal("invalid form saved contact")
			}
		})
	}
}

// TestContactInvalidEditRecovers verifies a rejected edit keeps the stored
// revision and only safe single-value input, so correcting it can actually save.
func TestContactInvalidEditRecovers(t *testing.T) {
	for _, kind := range []string{"invalid email", "long name", "control name", "long company", "duplicate name", "unknown field", "invalid revision"} {
		t.Run(kind, func(t *testing.T) {
			b := newBrowserFixture(t)
			b.finish(b.start(b.invite("client-a", "client-a", "client")))
			token := b.csrf("/portal/contact")
			s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", "Stored", "stored@example.invalid", "Stored Company"), b.site.URL)
			if s != 303 {
				t.Fatalf("initial save=%d", s)
			}
			token = b.csrf("/portal/contact")
			form := contactForm(token, "1", "Draft", "bad", "Draft Company")
			wantName, wantEmail, wantCompany := "Draft", "bad", "Draft Company"
			switch kind {
			case "long name":
				form.Set("name", strings.Repeat("x", 121))
				wantName = "Stored"
			case "control name":
				form.Set("name", "Unsafe\nName")
				wantName = "Stored"
			case "long company":
				form.Set("company", strings.Repeat("x", 161))
				wantCompany = "Stored Company"
			case "duplicate name":
				form.Add("name", "Other")
				wantName = "Stored"
			case "unknown field":
				form.Set("email_verified", "true")
			case "invalid revision":
				form.Set("revision", "-1")
				wantName, wantEmail, wantCompany = "Stored", "stored@example.invalid", "Stored Company"
			}
			s, body, _ := b.request("POST", "/portal/contact", form, b.site.URL)
			if s != 400 || !strings.Contains(body, `name="revision" value="1"`) {
				t.Fatalf("rejected edit lost current revision: status=%d body=%s", s, body)
			}
			for _, want := range []string{wantName, wantEmail, wantCompany} {
				if !strings.Contains(body, `value="`+want+`"`) {
					t.Fatalf("recovery lost safe field %q", want)
				}
			}
			var revision int
			if err := b.pool.QueryRow(b.f.ctx, "SELECT revision FROM portal_contacts").Scan(&revision); err != nil || revision != 1 {
				t.Fatal("invalid edit wrote a revision", err)
			}
			token = b.csrf("/portal/contact")
			s, _, _ = b.request("POST", "/portal/contact", contactForm(token, "1", wantName, "corrected@example.invalid", wantCompany), b.site.URL)
			if s != 303 {
				t.Fatalf("corrected edit cannot save=%d", s)
			}
		})
	}
}

// TestContactInvalidStaleEditDoesNotRebase prevents invalid-form recovery from
// blessing stale draft text with a newer revision and overwriting another edit.
func TestContactInvalidStaleEditDoesNotRebase(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token := b.csrf("/portal/contact")
	s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", "Original", "original@example.invalid", ""), b.site.URL)
	if s != 303 {
		t.Fatalf("initial save=%d", s)
	}
	token = b.csrf("/portal/contact")
	s, _, _ = b.request("POST", "/portal/contact", contactForm(token, "1", "Newer", "newer@example.invalid", ""), b.site.URL)
	if s != 303 {
		t.Fatalf("newer save=%d", s)
	}
	token = b.csrf("/portal/contact")
	s, body, _ := b.request("POST", "/portal/contact", contactForm(token, "1", "Stale Draft", "bad", ""), b.site.URL)
	if s != 409 || !strings.Contains(body, `name="revision" value="2"`) || !strings.Contains(body, `value="Newer"`) || strings.Contains(body, `value="Stale Draft"`) {
		t.Fatalf("stale invalid draft rebased or wrong recovery=%d %s", s, body)
	}
	token = b.csrf("/portal/contact")
	s, _, _ = b.request("POST", "/portal/contact", contactForm(token, "1", "Stale Draft", "corrected@example.invalid", ""), b.site.URL)
	if s != 409 {
		t.Fatalf("stale corrected edit overwrote newer data=%d", s)
	}
	_, body, _ = b.request("GET", "/portal/contact", nil, "")
	if !strings.Contains(body, `value="Newer"`) || !strings.Contains(body, `value="newer@example.invalid"`) {
		t.Fatal("invalid/stale edit changed the stored profile")
	}
}

// TestContactWorkspaceNavigation checks both localized client links and the
// actual form layout while keeping client-only navigation out of operator pages.
func TestContactWorkspaceNavigation(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	for _, lang := range []string{"en", "da"} {
		s, body, _ := b.request("GET", "/portal/?lang="+lang, nil, "")
		if s != 200 || !strings.Contains(body, `href="/portal/contact?lang=`+lang+`"`) {
			t.Fatal("client cannot reach contact details from their workspace")
		}
		_, form, _ := b.request("GET", "/portal/contact?lang="+lang, nil, "")
		if !strings.Contains(form, `class="contact-form"`) {
			t.Fatal("contact form does not use its field-spacing layout")
		}
	}
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	b.finish(b.start(b.invite("operator", "operator-client", "operator")))
	_, body, _ := b.request("GET", "/portal/", nil, "")
	if strings.Contains(body, `href="/portal/contact`) {
		t.Fatal("operator workspace offers a client-only profile")
	}
}

// TestContactAnonymousAndExpiredSessionsDeny exercises GET and POST without
// durable live authentication and verifies that neither can save contact data.
func TestContactAnonymousAndExpiredSessionsDeny(t *testing.T) {
	b := newBrowserFixture(t)
	token := b.csrf("/portal/sign-in")
	for _, method := range []string{"GET", "POST"} {
		s, _, _ := b.request(method, "/portal/contact", contactForm(token, "0", "A", "a@example.invalid", ""), b.site.URL)
		if s != 401 {
			t.Fatalf("anonymous %s=%d", method, s)
		}
	}
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token = b.csrf("/portal/contact")
	if _, err := b.pool.Exec(b.f.ctx, "UPDATE portal_sessions SET expiry=now()-interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		s, _, _ := b.request(method, "/portal/contact", contactForm(token, "0", "A", "a@example.invalid", ""), b.site.URL)
		if s != 401 {
			t.Fatalf("expired %s=%d", method, s)
		}
	}
	var n int
	if err := b.pool.QueryRow(b.f.ctx, "SELECT count(*) FROM portal_contacts").Scan(&n); err != nil || n != 0 {
		t.Fatal("unauthenticated form saved a contact", err)
	}
}

// TestContactRevisionPreventsConcurrentLostUpdate races six real requests against
// one expected database revision and requires exactly one accepted update.
func TestContactRevisionPreventsConcurrentLostUpdate(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token := b.csrf("/portal/contact")
	s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", "Original", "a@example.invalid", ""), b.site.URL)
	if s != 303 {
		t.Fatalf("initial save=%d", s)
	}
	token = b.csrf("/portal/contact")
	u, _ := url.Parse(b.site.URL)
	cookies := b.client.Jar.Cookies(u)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			client := *b.client
			client.Jar = nil
			req, _ := http.NewRequest("POST", b.site.URL+"/portal/contact", strings.NewReader(contactForm(token, "1", "Newer", "a@example.invalid", "").Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", b.site.URL)
			for _, c := range cookies {
				req.AddCookie(c)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() {
				if err := res.Body.Close(); err != nil {
					t.Error("fixture contact response cleanup failed")
				}
			}()
			if res.StatusCode == 303 {
				accepted.Add(1)
			} else if res.StatusCode != 409 {
				t.Errorf("concurrent save=%d", res.StatusCode)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted saves=%d want1", accepted.Load())
	}
	var revision int
	var name string
	if err := b.pool.QueryRow(b.f.ctx, "SELECT revision,name FROM portal_contacts WHERE client_id='client-a'").Scan(&revision, &name); err != nil {
		t.Fatal(err)
	}
	if revision != 2 || name != "Newer" {
		t.Fatal("lost update or incorrect revision")
	}
}

// TestContactRevocationOperatorAndClosedFlagsDeny rechecks revoked membership,
// client-only access and false/error/type-mismatched feature-flag boundaries.
func TestContactRevocationOperatorAndClosedFlagsDeny(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token := b.csrf("/portal/contact")
	if err := b.svc.Revoke(b.f.ctx, b.f.issuer, "client-a"); err != nil {
		t.Fatal(err)
	}
	s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", "A", "a@example.invalid", ""), b.site.URL)
	if s != 401 {
		t.Fatalf("revoked save=%d", s)
	}
	b.mu.Lock()
	b.subject = "operator"
	b.mu.Unlock()
	b.finish(b.start(b.invite("operator", "operator-client", "operator")))
	s, _, _ = b.request("GET", "/portal/contact", nil, "")
	if s != 403 {
		t.Fatalf("operator used client profile=%d", s)
	}
	for _, v := range []any{false, nil, "true"} {
		setFlag(t, b.flags, v)
		for _, method := range []string{"GET", "POST"} {
			s, _, h := b.request(method, "/portal/contact", contactForm(token, "0", "A", "a@example.invalid", ""), b.site.URL)
			if s != 404 || len(h.Values("Set-Cookie")) != 0 {
				t.Fatal("closed contact endpoint exposed")
			}
		}
	}
}

// TestContactEscapesPrivateDisplayDataAndLocalizes renders submitted markup as
// escaped data and keeps the unverified-email disclosure in both languages.
func TestContactEscapesPrivateDisplayDataAndLocalizes(t *testing.T) {
	b := newBrowserFixture(t)
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	token := b.csrf("/portal/contact")
	s, _, _ := b.request("POST", "/portal/contact", contactForm(token, "0", `<script>alert("x")</script>`, "a@example.invalid", `<img src=x onerror=alert(1)>`), b.site.URL)
	if s != 303 {
		t.Fatalf("safe escaped data rejected=%d", s)
	}
	for _, lang := range []string{"en", "da"} {
		s, body, _ := b.request("GET", "/portal/contact?lang="+lang, nil, "")
		if s != 200 || strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") || !strings.Contains(body, "&lt;script&gt;") {
			t.Fatal("unescaped private data")
		}
		if lang == "da" && (!strings.Contains(body, "Dine kontaktoplysninger") || !strings.Contains(body, "ikke bekræftet")) {
			t.Fatal("Danish contact state missing")
		}
	}
}
