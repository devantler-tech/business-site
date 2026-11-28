package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestCustomerCanChooseBetweenWorkspaceMemberships(t *testing.T) {
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("multi-workspace-customer", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "first-workspace", "client"))
	if err := b.svc.Invite(t.Context(), Invitation{Issuer: b.f.issuer, Subject: subject, ClientID: "second-workspace", Role: "client", Expires: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("a current member could not receive a second invitation: %v", err)
	}
	const choices = "/portal/workspaces?lang=en"
	status, page, _ := b.request("GET", choices, nil, "")
	if status != http.StatusOK || !strings.Contains(page, "first-workspace") || !strings.Contains(page, "second-workspace") {
		t.Fatalf("workspace choice omitted a membership or invitation: %d", status)
	}
	open := func(workspace string) int {
		form := url.Values{"workspace": {workspace}, "csrf_token": {b.csrf(choices)}}
		status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", form, b.site.URL)
		return status
	}
	if status := open("unrelated-workspace"); status != http.StatusForbidden {
		t.Fatalf("opened a workspace without a membership or invitation: %d", status)
	}
	oldToken := b.csrf(choices)
	if status := open("second-workspace"); status != http.StatusSeeOther {
		t.Fatalf("could not accept the second invitation and open its workspace: %d", status)
	}
	staleForm := url.Values{"workspace": {"first-workspace"}, "csrf_token": {oldToken}}
	if status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", staleForm, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("a form rendered before switching retained write authority: %d", status)
	}
	if status, page, _ := b.request("GET", "/portal/?lang=en", nil, ""); status != http.StatusOK || !strings.Contains(page, "second-workspace") {
		t.Fatalf("the session did not enter the selected workspace: %d", status)
	}
	if got := b.memberships(subject, "client"); got != 2 {
		t.Fatalf("the second invitation replaced the original membership: %d", got)
	}
	if status := open("first-workspace"); status != http.StatusSeeOther {
		t.Fatalf("could not switch back to the first workspace: %d", status)
	}
	customerJar := b.client.Jar
	b.client.Jar, _ = cookiejar.New(nil)
	configureOperatorBrowser(t, b)
	remove := operatorMembershipForm(b, subject, "first-workspace")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/remove", remove, b.site.URL); status != http.StatusOK {
		t.Fatalf("operator could not end one workspace membership: %d", status)
	}
	var deadline bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_customer_account_closures)`).Scan(&deadline); err != nil || deadline {
		t.Fatalf("removing one workspace scheduled an account that still has a membership: %v", err)
	}
	b.client.Jar = customerJar
	if status := open("first-workspace"); status != http.StatusForbidden {
		t.Fatalf("removed membership remained selectable: %d", status)
	}
	if status := open("second-workspace"); status != http.StatusSeeOther {
		t.Fatalf("membership removal blocked access to the remaining workspace: %d", status)
	}
}

func TestWorkspaceInvitationStaysPendingWhenCustomerSignsOutDuringRedemption(t *testing.T) {
	checkInterruptedWorkspaceInvitation(t, func(b *browserFixture) { b.signOut() })
}

func TestWorkspaceInvitationStaysPendingWhenPortalIsDisabledDuringRedemption(t *testing.T) {
	checkInterruptedWorkspaceInvitation(t, func(b *browserFixture) { setFlag(t, b.flags, false) })
}

func TestWorkspaceInvitationStaysPendingWhenSessionExpiresDuringFinalEvaluation(t *testing.T) {
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("expiring-invitation-session", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "workspace-one", "client"))
	b.invite(subject, "workspace-two", "client")
	key := base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
	p := Principal{issuer: b.f.issuer, subject: subject, expires: time.Now().Add(time.Minute)}
	snapshot := durableResponseSnapshot{
		key:      key,
		original: contactAuthority{principal: p, deadline: p.expires},
	}
	deadline := time.Now().Add(time.Second)
	if err := b.pool.QueryRow(t.Context(), `UPDATE portal_sessions SET expiry=$2 WHERE token=$1 RETURNING data`,
		key, deadline).Scan(&snapshot.data); err != nil {
		t.Fatal(err)
	}
	provider := installReadProvider(t, b, "portal-identity", 1)
	release := onceRelease(provider.release)
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- b.svc.redeem(ctx, p, "workspace-two", &snapshot) }()
	select {
	case <-provider.blocked:
	case <-ctx.Done():
		t.Fatal("redemption did not reach the final evaluation")
	}
	select {
	case <-time.After(time.Until(deadline)):
	case <-ctx.Done():
		t.Fatal("the fixture session did not expire within the test")
	}
	release()
	select {
	case err := <-done:
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("redemption with an expired session returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("redemption did not complete after the session expired")
	}
	assertWorkspaceInvitationPending(t, b, subject)
}

func checkInterruptedWorkspaceInvitation(t *testing.T, interrupt func(*browserFixture)) {
	t.Helper()
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("invitation-session-customer", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "workspace-one", "client"))
	b.invite(subject, "workspace-two", "client")
	form := url.Values{"workspace": {"workspace-two"}, "csrf_token": {b.csrf("/portal/workspaces?lang=en")}}
	pause := installCompletedRead(t, b, "SELECT COALESCE(min(client_id),'')")
	resume := onceRelease(pause.release)
	defer resume()

	response := make(chan int, 1)
	go func() {
		status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", form, b.site.URL)
		response <- status
	}()
	select {
	case <-pause.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never read the pending invitation")
	}
	interrupt(b)
	resume()
	select {
	case status := <-response:
		if status != http.StatusForbidden {
			t.Fatalf("the interrupted request returned %d", status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupted request did not complete")
	}
	assertWorkspaceInvitationPending(t, b, subject)
}

func assertWorkspaceInvitationPending(t *testing.T, b *browserFixture, subject string) {
	t.Helper()
	if got := b.memberships(subject, "client"); got != 1 {
		t.Fatalf("the interrupted request created a new membership: %d total", got)
	}
	var pending bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS (
 SELECT 1 FROM portal_invitations WHERE issuer=$1 AND subject=$2 AND client_id='workspace-two' AND NOT revoked)`,
		b.f.issuer, subject).Scan(&pending); err != nil || !pending {
		t.Fatalf("the interrupted request consumed the invitation: %v", err)
	}
}

func TestWorkspaceSwitchCannotReplaceSignedOutSession(t *testing.T) {
	b := newBrowserFixture(t)
	subject := customeridentity.DexSubject("workspace-switch-customer", customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "workspace-one", "client"))
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES($1,$2,'workspace-two','client')`, b.f.issuer, subject); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"workspace": {"workspace-two"}, "csrf_token": {b.csrf("/portal/workspaces?lang=en")}}
	logout, err := b.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logout.Rollback(context.Background()) }()
	var logoutPID int32
	if err := logout.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&logoutPID); err != nil {
		t.Fatal(err)
	}
	key := base64.RawURLEncoding.EncodeToString(digest(b.cookie().Value))
	if tag, err := logout.Exec(ctx, `DELETE FROM portal_sessions WHERE token=$1`, key); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("fixture could not delete the original session: %v", err)
	}
	result := make(chan int, 1)
	go func() {
		status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", form, b.site.URL)
		result <- status
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
waiting:
	for {
		select {
		case status := <-result:
			t.Fatalf("workspace switch returned %d before reaching the session lock", status)
		case <-ctx.Done():
			t.Fatal("workspace switch did not reach the session lock")
		case <-tick.C:
			var blocked bool
			if err := b.pool.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, logoutPID).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break waiting
			}
		}
	}
	if err := logout.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-result:
		if status != http.StatusForbidden {
			t.Fatalf("workspace switch after sign-out returned %d", status)
		}
	case <-ctx.Done():
		t.Fatal("workspace switch did not finish after sign-out")
	}
}
