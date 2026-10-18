package identity

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func contactVerificationFlags(t *testing.T, b *browserFixture, value any) {
	t.Helper()
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-contact-verification"}, []any{true, value})
}

func assertContactUnconfirmed(t *testing.T, b *browserFixture) {
	t.Helper()
	var unchanged bool
	if err := b.pool.QueryRow(t.Context(), `SELECT revision=1 AND NOT email_verified AND email_verified_at IS NULL FROM portal_contacts WHERE client_id='client-a'`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("refused proof changed the saved contact", err)
	}
}

func TestContactVerificationRefusesUnsupportedSignedClaims(t *testing.T) {
	for _, change := range []struct {
		name   string
		claims map[string]any
	}{
		{"missing verified claim", map[string]any{"email": "a@example.invalid"}},
		{"false", map[string]any{"email": "a@example.invalid", "email_verified": false}},
		{"string", map[string]any{"email": "a@example.invalid", "email_verified": "true"}},
		{"null", map[string]any{"email": "a@example.invalid", "email_verified": nil}},
		{"different email", map[string]any{"email": "other@example.invalid", "email_verified": true}},
		{"different case", map[string]any{"email": "A@example.invalid", "email_verified": true}},
		{"bad email type", map[string]any{"email": []string{"a@example.invalid"}, "email_verified": true}},
		{"wrong issuer", map[string]any{"iss": "https://wrong.example.invalid", "email": "a@example.invalid", "email_verified": true}},
		{"wrong audience", map[string]any{"aud": "other", "email": "a@example.invalid", "email_verified": true}},
		{"wrong nonce", map[string]any{"nonce": "wrong", "email": "a@example.invalid", "email_verified": true}},
		{"expired token", map[string]any{"exp": time.Now().Add(-time.Hour).Unix(), "email": "a@example.invalid", "email_verified": true}},
	} {
		t.Run(change.name, func(t *testing.T) {
			b := savedVerificationContact(t)
			b.configure(change.claims, false)
			status, _, _ := b.request("GET", verificationStart(t, b, "1"), nil, "")
			if status != http.StatusBadRequest {
				t.Fatalf("unsupported signed claim status=%d", status)
			}
			assertContactUnconfirmed(t, b)
		})
	}
}

func TestContactVerificationBindsCurrentClientAndSavedRevision(t *testing.T) {
	for _, change := range []string{"account", "revocation", "operator", "client binding", "contact revision", "attempt expiry"} {
		t.Run(change, func(t *testing.T) {
			b := savedVerificationContact(t)
			b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
			callback := verificationStart(t, b, "1")
			var query string
			switch change {
			case "account":
				b.mu.Lock()
				b.subject = "client-b"
				b.mu.Unlock()
			case "revocation":
				query = `UPDATE portal_memberships SET revoked=true WHERE subject='client-a'`
			case "operator":
				query = `UPDATE portal_memberships SET role='operator' WHERE subject='client-a'`
			case "client binding":
				query = `UPDATE portal_memberships SET client_id='client-b' WHERE subject='client-a'`
			case "contact revision":
				query = `UPDATE portal_contacts SET revision=2 WHERE client_id='client-a'`
			case "attempt expiry":
				query = `UPDATE portal_login_attempts SET expires_at=now()-interval '1 minute' WHERE purpose='contact'`
			}
			if query != "" {
				if _, err := b.pool.Exec(t.Context(), query); err != nil {
					t.Fatal("fixture authority change failed", err)
				}
			}
			status, _, _ := b.request("GET", callback, nil, "")
			if status != http.StatusBadRequest {
				t.Fatalf("changed binding accepted: %d", status)
			}
			var confirmed bool
			if err := b.pool.QueryRow(t.Context(), `SELECT email_verified FROM portal_contacts WHERE client_id='client-a'`).Scan(&confirmed); err != nil || confirmed {
				t.Fatal("changed client/revision gained confirmation", err)
			}
		})
	}
}

func TestContactVerificationDefaultOffAndFormAuthority(t *testing.T) {
	b := savedVerificationContact(t)
	for _, value := range []any{nil, false, "true"} {
		contactVerificationFlags(t, b, value)
		status, body, _ := b.request("GET", "/portal/contact", nil, "")
		if status != http.StatusOK || strings.Contains(body, `action="/portal/contact/verify"`) {
			t.Fatal("closed confirmation gate exposed a control")
		}
		status, _, _ = b.request("POST", "/portal/contact/verify", nil, b.site.URL)
		if status != http.StatusNotFound {
			t.Fatal("closed confirmation endpoint remained available")
		}
	}
	contactVerificationFlags(t, b, true)
	for _, key := range []string{"email", "client_id", "role", "email_verified", "duplicate revision", "missing csrf", "cross origin"} {
		form := url.Values{"csrf_token": {b.csrf("/portal/contact")}, "revision": {"1"}, "lang": {"en"}}
		origin, want := b.site.URL, http.StatusBadRequest
		switch key {
		case "duplicate revision":
			form.Add("revision", "1")
		case "missing csrf":
			form.Del("csrf_token")
			want = http.StatusForbidden
		case "cross origin":
			origin = "https://other.example.invalid"
			want = http.StatusForbidden
		default:
			form.Set(key, "forged")
		}
		status, _, _ := b.request("POST", "/portal/contact/verify", form, origin)
		if status != want {
			t.Fatalf("form %s status=%d want%d", key, status, want)
		}
	}
	assertContactUnconfirmed(t, b)
}

func TestContactVerificationLossAndEditsClearProof(t *testing.T) {
	b := savedVerificationContact(t)
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	callback := verificationStart(t, b, "1")
	contactVerificationFlags(t, b, false)
	status, _, _ := b.request("GET", callback, nil, "")
	if status != http.StatusNotFound {
		t.Fatal("callback ignored disabled verification gate")
	}
	assertContactUnconfirmed(t, b)
	contactVerificationFlags(t, b, true)
	status, _, _ = b.request("GET", callback, nil, "")
	if status != http.StatusSeeOther {
		t.Fatal("unconsumed, still-valid authorized callback was lost")
	}
	status, _, _ = b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "2", "Updated client", "new@example.invalid", ""), b.site.URL)
	if status != http.StatusSeeOther {
		t.Fatal("contact update after proof failed")
	}
	var reset bool
	if err := b.pool.QueryRow(t.Context(), `SELECT revision=3 AND email='new@example.invalid' AND NOT email_verified AND email_verified_at IS NULL FROM portal_contacts WHERE client_id='client-a'`).Scan(&reset); err != nil || !reset {
		t.Fatal("contact edit retained old proof", err)
	}
}

func TestContactVerificationConcurrentCallbackOnlyConfirmsOnce(t *testing.T) {
	b := savedVerificationContact(t)
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	callback := verificationStart(t, b, "1")
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		wg.Go(func() { status, _, _ := b.request("GET", callback, nil, ""); results <- status })
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[http.StatusSeeOther] != 1 || counts[http.StatusBadRequest] != 1 {
		t.Fatal("concurrent callbacks were not single-use", counts)
	}
	var once bool
	if err := b.pool.QueryRow(t.Context(), `SELECT email_verified AND email_verified_at IS NOT NULL AND revision=2 FROM portal_contacts WHERE client_id='client-a'`).Scan(&once); err != nil || !once {
		t.Fatal("concurrent proof changed contact twice", err)
	}
}

func TestContactVerificationPurposeConsumersAreIndependent(t *testing.T) {
	b := savedVerificationContact(t)
	p := Principal{b.f.issuer, "client-a", "portal-fixture", time.Now().Add(time.Hour)}
	a, err := b.svc.beginContactAttempt(t.Context(), p, 1)
	if err != nil {
		t.Fatal("valid confirmation attempt failed", err)
	}
	if _, _, err = b.svc.consumeAttempt(t.Context(), a.State, a.BrowserBinding); err == nil {
		t.Fatal("ordinary login consumed confirmation purpose")
	}
	if _, _, _, err = b.svc.consumeContactVerification(t.Context(), a.State, a.BrowserBinding); err != nil {
		t.Fatal("wrong consumer destroyed the valid confirmation attempt", err)
	}
	a, err = b.svc.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = b.svc.consumeContactVerification(t.Context(), a.State, a.BrowserBinding); err == nil {
		t.Fatal("confirmation consumed ordinary login purpose")
	}
	if _, _, err = b.svc.consumeAttempt(t.Context(), a.State, a.BrowserBinding); err != nil {
		t.Fatal("wrong consumer destroyed ordinary login", err)
	}
}

func TestContactVerificationOrdinaryLoginNeverConfirmsAddress(t *testing.T) {
	b := savedVerificationContact(t)
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	b.finish(b.start(""))
	assertContactUnconfirmed(t, b)
}

func TestContactVerificationRejectedDraftNeverClaimsConfirmation(t *testing.T) {
	b := savedVerificationContact(t)
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	if status, _, _ := b.request("GET", verificationStart(t, b, "1"), nil, ""); status != http.StatusSeeOther {
		t.Fatal("fixture confirmation failed")
	}
	form := contactForm(b.csrf("/portal/contact"), "2", "Fictional Client", "unconfirmed@example.invalid", "")
	form.Set("unknown", "rejected")
	status, body, _ := b.request("POST", "/portal/contact", form, b.site.URL)
	if status != http.StatusBadRequest || !strings.Contains(body, "unconfirmed@example.invalid") || strings.Contains(body, "Confirmed by your sign-in provider") {
		t.Fatal("rejected draft claimed the saved address's confirmation")
	}
	var saved bool
	if err := b.pool.QueryRow(t.Context(), `SELECT email='a@example.invalid' AND email_verified AND revision=2 FROM portal_contacts WHERE client_id='client-a'`).Scan(&saved); err != nil || !saved {
		t.Fatal("rejected draft altered saved proof", err)
	}
}

func TestContactVerificationFinalWriteChecksExpiryFlagsAndCancellation(t *testing.T) {
	for _, change := range []string{"unchanged", "identity flag", "verification flag", "expiry", "delayed expiry", "cancel"} {
		t.Run(change, func(t *testing.T) {
			b := savedVerificationContact(t)
			p := Principal{b.f.issuer, "client-a", "portal-fixture", time.Now().Add(time.Hour)}
			d, raw, nonce := prepareContactConfirmation(t, b, p, change == "delayed expiry")
			tracer := installCompletedRead(t, b, "UPDATE portal_contacts SET email_verified=true")
			// Start the real expiry window only after all fixture preparation.
			// The checkpoint must still be reached with live authority.
			if change == "expiry" || change == "delayed expiry" {
				p.expires = time.Now().Add(2 * time.Second)
			}
			ctx, cancel, done := startContactConfirmation(t, b, p, d, raw, nonce)
			select {
			case <-tracer.blocked:
			case <-ctx.Done():
				t.Fatal("confirmation did not reach actual uncommitted write")
			}
			if !time.Now().Before(p.expires) {
				t.Fatal("fixture authority expired before its uncommitted-write checkpoint")
			}
			switch change {
			case "identity flag":
				setFixturePortalFlags(t, b, []string{"portal-identity", "portal-contact-verification"}, []any{false, true})
			case "verification flag":
				contactVerificationFlags(t, b, false)
			case "expiry", "delayed expiry":
				waitForFixtureExpiry(ctx, t, p.expires)
			case "cancel":
				cancel()
			}
			close(tracer.release)
			var err error
			select {
			case err = <-done:
			case <-ctx.Done():
				select {
				case err = <-done:
				case <-time.After(time.Second):
					t.Fatal("confirmation did not return after cancellation")
				}
			}
			if change == "unchanged" {
				if err != nil {
					t.Fatal("valid confirmation was refused", err)
				}
			} else {
				if err == nil {
					t.Fatal("closed write authority was accepted")
				}
				assertContactUnconfirmed(t, b)
			}
		})
	}
}

func TestContactVerificationRechecksAuthorityAfterFinalSDKEvaluation(t *testing.T) {
	for _, change := range []string{"expiry", "delayed expiry", "identity flag"} {
		t.Run(change, func(t *testing.T) {
			b := savedVerificationContact(t)
			p := Principal{b.f.issuer, "client-a", "portal-fixture", time.Now().Add(time.Minute)}
			d, raw, nonce := prepareContactConfirmation(t, b, p, change == "delayed expiry")
			provider := installReadProvider(t, b, "portal-contact-verification", 2)
			if change == "expiry" || change == "delayed expiry" {
				p.expires = time.Now().Add(2 * time.Second)
			}
			ctx, _, done := startContactConfirmation(t, b, p, d, raw, nonce)
			select {
			case <-provider.blocked:
			case <-ctx.Done():
				t.Fatal("final SDK evaluation was not reached")
			}
			if !time.Now().Before(p.expires) {
				t.Fatal("fixture authority expired before its final SDK checkpoint")
			}
			if change == "expiry" || change == "delayed expiry" {
				waitForFixtureExpiry(ctx, t, p.expires)
			} else {
				setFixturePortalFlags(t, b, []string{"portal-identity", "portal-contact-verification"}, []any{false, true})
			}
			close(provider.release)
			var err error
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("final SDK evaluation did not return")
			}
			if err == nil {
				t.Fatal("closed client authority committed after final SDK wait")
			}
			assertContactUnconfirmed(t, b)
		})
	}
}

// prepareContactConfirmation completes the real broker/SQL fixture work before
// callers arm the short expiry window for their SQL or SDK checkpoint.
func prepareContactConfirmation(t *testing.T, b *browserFixture, p Principal, delayed bool) (contactVerification, string, string) {
	t.Helper()
	a, err := b.svc.beginContactAttempt(t.Context(), p, 1)
	if err != nil {
		t.Fatal("confirmation fixture attempt failed", err)
	}
	d, nonce, _, err := b.svc.consumeContactVerification(t.Context(), a.State, a.BrowserBinding)
	if err != nil {
		t.Fatal("confirmation fixture consumption failed", err)
	}
	raw := b.f.token(t, "client-a", nonce, map[string]any{"email": "a@example.invalid", "email_verified": true})
	if delayed {
		<-time.After(750 * time.Millisecond)
	}
	return d, raw, nonce
}

// startContactConfirmation owns and joins the same bounded real write worker
// for SQL and SDK checkpoints, before fixture pools and brokers are closed.
func startContactConfirmation(t *testing.T, b *browserFixture, p Principal, d contactVerification, raw, nonce string) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := b.svc.confirmContact(ctx, p, d, raw, nonce)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("confirmation worker did not finish")
		}
	})
	return ctx, cancel, done
}

func TestContactVerificationControlEvaluationPrecedesPrivateRead(t *testing.T) {
	for _, change := range []string{"revocation", "client binding"} {
		t.Run(change, func(t *testing.T) {
			b := savedVerificationContact(t)
			provider := installReadProvider(t, b, "portal-contact-verification", 1)
			var released sync.Once
			release := func() { released.Do(func() { close(provider.release) }) }
			type result struct {
				status int
				body   string
			}
			results := make(chan result, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				status, body, _ := b.request("GET", "/portal/contact", nil, "")
				results <- result{status, body}
			}()
			defer func() {
				release()
				select {
				case <-finished:
				case <-time.After(12 * time.Second):
					t.Error("private-page worker did not finish")
				}
			}()
			select {
			case <-provider.blocked:
			case <-time.After(3 * time.Second):
				t.Fatal("display SDK evaluation was not reached")
			}
			query := `UPDATE portal_memberships SET revoked=true WHERE subject='client-a'`
			if change == "client binding" {
				query = `UPDATE portal_memberships SET client_id='client-b' WHERE subject='client-a'`
			}
			if _, err := b.pool.Exec(t.Context(), query); err != nil {
				t.Fatal("fixture current-access change failed", err)
			}
			release()
			var got result
			select {
			case got = <-results:
			case <-time.After(12 * time.Second):
				t.Fatal("private-page result did not return")
			}
			if strings.Contains(got.body, "Fictional Client") || strings.Contains(got.body, "a@example.invalid") {
				t.Fatal("display SDK wait released a prior client's contact profile")
			}
		})
	}
}

func TestContactVerificationV8PreservesChecksumsAndOrdinaryAttempts(t *testing.T) {
	b := savedVerificationContact(t)
	migrator := migrationDB(t)
	rewindContactVerificationFixture(t, migrator)
	var before string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(t.Context(), `INSERT INTO portal_login_attempts VALUES(decode(repeat('aa',32),'hex'),decode(repeat('bb',32),'hex'),'old-nonce',repeat('v',43),now()+interval '1 hour')`); err != nil {
		t.Fatal("actual v7 attempt fixture failed", err)
	}
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal("v8 historical upgrade failed", err)
		}
	}
	var after, purpose string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=7`).Scan(&after); err != nil || before != after {
		t.Fatal("v8 changed historical migration checksums", err)
	}
	if err := migrator.QueryRow(t.Context(), `SELECT purpose FROM portal_login_attempts WHERE nonce='old-nonce' AND pkce_verifier=repeat('v',43)`).Scan(&purpose); err != nil || purpose != "sign-in" {
		t.Fatal("ordinary historical attempt changed", err)
	}
	assertContactUnconfirmed(t, b)
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_contacts SET email_verified=true WHERE client_id='client-a'`); err == nil {
		t.Fatal("confirmation without provenance was accepted by schema")
	}
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_login_attempts SET purpose='contact' WHERE nonce='old-nonce'`); err == nil {
		t.Fatal("unbound contact purpose accepted by schema")
	}
}

func savedVerificationContact(t *testing.T) *browserFixture {
	t.Helper()
	b := newBrowserFixture(t)
	b.client.Timeout = 10 * time.Second
	b.finish(b.start(b.invite("client-a", "client-a", "client")))
	contactVerificationFlags(t, b, true)
	status, _, _ := b.request("POST", "/portal/contact", contactForm(b.csrf("/portal/contact"), "0", "Fictional Client", "a@example.invalid", ""), b.site.URL)
	if status != http.StatusSeeOther {
		t.Fatalf("save before confirmation status=%d", status)
	}
	return b
}

// verificationStart exercises the actual protected form and exact scope before
// obtaining one code from the real synthetic TLS broker, never a stub callback.
func verificationStart(t *testing.T, b *browserFixture, revision string) string {
	t.Helper()
	form := url.Values{"csrf_token": {b.csrf("/portal/contact")}, "revision": {revision}, "lang": {"en"}}
	status, _, headers := b.request("POST", "/portal/contact/verify", form, b.site.URL)
	if status != http.StatusSeeOther {
		t.Fatalf("explicit confirmation start status=%d want303", status)
	}
	u, err := url.Parse(headers.Get("Location"))
	if err != nil || u.Query().Get("scope") != "openid email" {
		t.Fatal("confirmation did not request exactly the broker email scope")
	}
	b.mu.Lock()
	b.expectedScope = "openid email"
	b.mu.Unlock()
	client := b.broker.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(headers.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error("confirmation fixture response cleanup failed")
	}
	if response.StatusCode != http.StatusSeeOther {
		t.Fatal("broker rejected confirmation authorization")
	}
	u, err = url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.RequestURI()
}

// Removing the purpose-bound callback write must leave the contact unconfirmed.
func TestContactVerificationExplicitBrokerProofPersists(t *testing.T) {
	b := savedVerificationContact(t)
	b.configure(map[string]any{"email": "a@example.invalid", "email_verified": true}, false)
	callback := verificationStart(t, b, "1")
	status, _, headers := b.request("GET", callback, nil, "")
	if status != http.StatusSeeOther || headers.Get("Location") != "/portal/contact?lang=en" {
		t.Fatalf("confirmation callback=%d", status)
	}
	b.reconnect(context.Background())
	status, body, _ := b.request("GET", "/portal/contact", nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Confirmed by your sign-in provider") || !strings.Contains(body, `name="revision" value="2"`) {
		t.Fatal("confirmed contact was not retained after reconnect")
	}
	status, _, _ = b.request("GET", callback, nil, "")
	if status != http.StatusBadRequest {
		t.Fatal("confirmation callback replay was accepted")
	}
}
