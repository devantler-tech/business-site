package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPreviewRoutesAreClosedOnTheOrdinaryPortal(t *testing.T) {
	b := newBrowserFixture(t)
	b.as(customerSubject)
	b.finish(b.startInvited(customerSubject, "fixture-customer", "client"))
	form := url.Values{"csrf_token": {b.csrf("/portal/")}, "lang": {"en"}}
	for _, path := range []string{"/portal/preview", "/portal/preview/leave"} {
		if status, _, _ := b.request(http.MethodPost, path, form, b.site.URL); status != http.StatusNotFound {
			t.Errorf("ordinary portal exposed a preview action: status %d", status)
		}
	}
}

func TestPrivatePreviewSwitchesToOneFictionalClient(t *testing.T) {
	b := previewOperatorFixture(t)
	portal, err := url.Parse(b.site.URL)
	if err != nil {
		t.Fatal(err)
	}
	enter := "/portal/preview"
	leave := "/portal/preview/leave"
	home := "/portal/?lang=en"
	operator := "/portal/operator?lang=en"
	operatorCookies := b.client.Jar.Cookies(portal)
	form := url.Values{"csrf_token": {b.csrf(operator)}, "lang": {"en"}}
	forged := url.Values{"csrf_token": form["csrf_token"], "lang": {"en"}, "client": {"real-client"}}
	if status, _, _ := b.request(http.MethodPost, enter, forged, b.site.URL); status != http.StatusBadRequest {
		t.Errorf("preview did not reject a browser-selected client as invalid: status %d", status)
	}
	if status, _, headers := b.request(http.MethodPost, enter, form, b.site.URL); status != http.StatusSeeOther || headers.Get("Location") != home {
		t.Fatalf("preview entry did not open the client workspace: status %d", status)
	}
	if status, page, _ := b.request(http.MethodGet, home, nil, ""); status != http.StatusOK || !strings.Contains(page, "Demo workspace") {
		t.Errorf("preview did not serve its fixed demo workspace: status %d", status)
	}
	if status, _, _ := b.request(http.MethodGet, operator, nil, ""); status != http.StatusForbidden {
		t.Errorf("client preview retained operator authority: status %d", status)
	}
	if status, page, _ := b.request(http.MethodGet, "/portal/contact?lang=en", nil, ""); status != http.StatusOK || !strings.Contains(page, "Return to operator workspace") {
		t.Errorf("client preview did not reach its scoped contact page: status %d", status)
	}
	requirePreviewCookieRetired(t, b, portal, operatorCookies, operator)

	form["csrf_token"] = []string{b.csrf(home)}
	previewCookies := b.client.Jar.Cookies(portal)
	if status, _, headers := b.request(http.MethodPost, leave, form, b.site.URL); status != http.StatusSeeOther || headers.Get("Location") != operator {
		t.Fatalf("preview did not return to the operator workspace: status %d", status)
	}
	if status, _, _ := b.request(http.MethodGet, operator, nil, ""); status != http.StatusOK {
		t.Errorf("return did not restore current operator access: status %d", status)
	}
	requirePreviewCookieRetired(t, b, portal, previewCookies, home)

	form["csrf_token"] = []string{b.csrf(operator)}
	if status, _, _ := b.request(http.MethodPost, enter, form, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("could not re-enter the demo workspace: status %d", status)
	}
	revokeFixtureAccess(t.Context(), t, b, sharedOperator)
	if status, _, _ := b.request(http.MethodGet, home, nil, ""); status == http.StatusOK {
		t.Fatal("a revoked operator could still use the preview")
	}
}

func TestPreviewCanReturnAfterItsFictionalMembershipIsRevoked(t *testing.T) {
	b := previewOperatorFixture(t)
	form := url.Values{"csrf_token": {b.csrf("/portal/operator")}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, "/portal/preview", form, b.site.URL); status != http.StatusSeeOther {
		t.Fatalf("preview entry returned %d", status)
	}
	form.Set("csrf_token", b.csrf("/portal/"))
	revokeFixtureAccess(t.Context(), t, b, previewSubject)
	if status, _, headers := b.request(http.MethodPost, "/portal/preview/leave", form, b.site.URL); status != http.StatusSeeOther || headers.Get("Location") != "/portal/operator?lang=en" {
		t.Fatalf("leaving a revoked demo workspace returned %d", status)
	}
}

func TestOrdinaryProvisioningReservesTheFictionalWorkspace(t *testing.T) {
	b := newBrowserFixture(t)
	invitation := Invitation{b.svc.broker.issuer, customerSubject, previewWorkspace, "client", time.Now().Add(time.Hour)}
	if err := b.svc.Invite(t.Context(), invitation); !errors.Is(err, ErrDenied) {
		t.Fatalf("ordinary provisioning accepted the reserved demo workspace: %v", err)
	}
	if validCustomerWorkspace(previewWorkspace) {
		t.Fatal("operator provisioning accepted the reserved demo workspace")
	}
}

func TestPreviewRefusesAPreviouslyProvisionedWorkspace(t *testing.T) {
	b := previewOperatorFixture(t)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspaces(id) VALUES ($1)`, previewWorkspace); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf_token": {b.csrf("/portal/operator")}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, "/portal/preview", form, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("preview accepted an existing operator-created workspace: %d", status)
	}
}

func TestPreviewPreservesOperatorSessionIfRotationFails(t *testing.T) {
	b := previewOperatorFixture(t)
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `ALTER TABLE portal_sessions
 ADD CONSTRAINT fixture_fail_session_write CHECK (false) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := migrator.Exec(context.Background(), `ALTER TABLE portal_sessions DROP CONSTRAINT fixture_fail_session_write`); err != nil {
			t.Error(err)
		}
	}()
	form := url.Values{"csrf_token": {b.csrf("/portal/operator")}, "lang": {"en"}}
	if status, _, _ := b.request(http.MethodPost, "/portal/preview", form, b.site.URL); status != http.StatusServiceUnavailable {
		t.Fatalf("failed session write returned %d", status)
	}
	if status, _, _ := b.request(http.MethodGet, "/portal/operator", nil, ""); status != http.StatusOK {
		t.Fatalf("failed rotation discarded the starting operator session: %d", status)
	}
}

func TestConcurrentPreviewTransitionsConsumeTheSourceSessionOnce(t *testing.T) {
	b := previewOperatorFixture(t)
	form := url.Values{"csrf_token": {b.csrf("/portal/operator")}, "lang": {"en"}}
	portal, err := url.Parse(b.site.URL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := b.client.Jar.Cookies(portal)
	key := base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	lock, err := b.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("failed to release disposable session lock")
		}
	}()
	var locked bool
	err = lock.QueryRow(ctx, `SELECT true FROM portal_sessions WHERE token=$1 FOR SHARE`, key).Scan(&locked)
	if err != nil || !locked {
		t.Fatal("could not hold the starting session in the disposable fixture")
	}

	results := make(chan int, 2)
	client := *b.client
	client.Jar = nil
	var requests sync.WaitGroup
	t.Cleanup(requests.Wait)
	for range 2 {
		requests.Go(func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.site.URL+"/portal/preview", strings.NewReader(form.Encode()))
			if err != nil {
				results <- 0
				return
			}
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", b.site.URL)
			resp, err := client.Do(req)
			if err != nil {
				results <- 0
				return
			}
			if resp.Body.Close() != nil {
				results <- 0
				return
			}
			results <- resp.StatusCode
		})
	}
	waitForFixtureDatabaseLock(ctx, t, b.pool, `SELECT count(*)=2 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'UPDATE portal_sessions SET token=%'`)
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got := map[int]int{<-results: 1}
	got[<-results]++
	if got[http.StatusSeeOther] != 1 || got[http.StatusForbidden] != 1 {
		t.Fatalf("two transitions could not arbitrate the same source session: %v", got)
	}
}

func previewOperatorFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := newBrowserFixture(t)
	if err := b.svc.LinkPreviewOperator(sharedOperator); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
	b.mu.Unlock()
	b.as(sharedOperator)
	b.finish(b.start())
	return b
}

func requirePreviewCookieRetired(t *testing.T, b *browserFixture, portal *url.URL, cookies []*http.Cookie, path string) {
	t.Helper()
	current := b.client.Jar
	defer func() { b.client.Jar = current }()
	stale, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	stale.SetCookies(portal, cookies)
	b.client.Jar = stale
	if status, _, _ := b.request(http.MethodGet, path, nil, ""); status == http.StatusOK {
		t.Fatal("a replaced preview or operator cookie was still authorized")
	}
}
