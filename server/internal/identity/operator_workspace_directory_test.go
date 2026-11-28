package identity

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceDirectoryOmitsExpiredInvitationOnlyWorkspaces(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("expired-contact", "stale-legacy-space", "client")
	b.invite("waiting-contact", "live-legacy-space", "client")
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_invitations SET expires_at=now()-interval '1 hour'
 WHERE issuer=$1 AND subject='expired-contact'`, b.f.issuer); err != nil {
		t.Fatal(err)
	}
	status, page, _ := b.request("GET", "/portal/operator/customers?lang=en", nil, "")
	if status != http.StatusOK || strings.Contains(page, "stale-legacy-space") || !strings.Contains(page, "live-legacy-space") {
		t.Fatalf("active directory does not distinguish a current invitation from old data: %d", status)
	}
}

func TestWorkspaceNativeCreationRetriesAndReusesClosedName(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	create := url.Values{"display_name": {"North Studio"}, "csrf_token": {b.csrf("/portal/operator/customers")}}
	createOnce := func() string {
		t.Helper()
		status, header := managementNativeRequest(b, operatorAPIRoot+"/workspaces?lang=en", create)
		next, err := url.Parse(header.Get("Location"))
		if status != http.StatusSeeOther || err != nil || next.Query().Get("workspace") == "" {
			t.Fatalf("native workspace creation did not open its detail page: %d", status)
		}
		return next.Query().Get("workspace")
	}
	first := createOnce()
	if got := createOnce(); got != first || first == "North Studio" {
		t.Fatal("a retry made another workspace or used display text as its identity")
	}
	closeForm := url.Values{"workspace": {first}, "confirm": {"North Studio"}, "csrf_token": {create.Get("csrf_token")}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", closeForm, b.site.URL); status != http.StatusOK {
		t.Fatalf("workspace did not close: %d", status)
	}
	if status, page, _ := b.request("GET", "/portal/operator/customers?view=closed", nil, ""); status != http.StatusOK || !strings.Contains(page, "North Studio") {
		t.Fatalf("closed workspace is missing from its own view: %d", status)
	}
	if second := createOnce(); second == first {
		t.Fatal("reusing the name reopened the original workspace")
	}
}

func TestWorkspaceInvitationCleanupRechecksExactSelection(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("expired-one", "north-lab", "client")
	b.invite("still-pending", "north-lab", "client")
	b.invite("expired-other", "south-lab", "client")
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_invitations SET expires_at=now()-interval '1 hour'
 WHERE subject LIKE 'expired-%'`); err != nil {
		t.Fatal(err)
	}
	path := "/portal/operator/customers?view=cleanup-confirm&workspace=north-lab"
	status, body, _ := b.request("GET", path, nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Invitations to remove permanently <strong>1</strong>") {
		t.Fatalf("cleanup did not preview the exact selection: %d", status)
	}
	form := workNativeForm(t, body, `customers/cleanup\?lang=en`)
	b.invite("expired-later", "north-lab", "client")
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_invitations SET revoked=true WHERE subject='expired-later'`); err != nil {
		t.Fatal(err)
	}
	cleanup := "/portal/operator/customers/cleanup?lang=en"
	if status, _, _ := b.request("POST", cleanup, form, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("changed cleanup selection was not refused: %d", status)
	}
	if b.invitationCount() != 4 {
		t.Fatal("stale confirmation removed data")
	}
	_, body, _ = b.request("GET", path, nil, "")
	form = workNativeForm(t, body, `customers/cleanup\?lang=en`)
	forged := url.Values{"workspace": {form.Get("workspace")}, "fingerprint": {form.Get("fingerprint")}, "csrf_token": {"wrong"}}
	if status, _, _ := b.request("POST", cleanup, forged, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("forged cleanup was not refused: %d", status)
	}
	if status, _, _ := b.request("POST", cleanup, form, b.site.URL); status != http.StatusOK {
		t.Fatalf("confirmed cleanup failed: %d", status)
	}
	remaining := b.invitationCount()
	if remaining != 2 {
		t.Fatalf("cleanup did not preserve the live and unrelated invitations: %d remain", remaining)
	}
	var kept bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT FROM portal_invitations WHERE subject='still-pending')
 AND EXISTS(SELECT FROM portal_invitations WHERE subject='expired-other')`).Scan(&kept); err != nil || !kept {
		t.Fatalf("cleanup selected the wrong invitations: %v", err)
	}
}

func TestWorkspaceInvitationSelectionAndRetry(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	creates := inviteDirectory(t, b, true, false, nil)
	form := invitationForm(b)
	path := operatorAPIRoot + "/invitations?focus=workspace"
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusBadRequest || b.invitationCount() != 0 {
		t.Fatalf("an unknown workspace accepted the invitation: %d", status)
	}
	for _, workspace := range []string{"client-b", "client-c"} {
		if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspaces(id) VALUES($1)`, workspace); err != nil {
			t.Fatal(err)
		}
	}
	status, markup, _ := b.request("GET", "/portal/operator/invitations/new?lang=en&workspace=client-b", nil, "")
	if status != http.StatusOK || !strings.Contains(markup, `name="workspace" value="client-b"`) ||
		strings.Contains(markup, `<select id="invite-workspace"`) || strings.Contains(markup, "client-c") {
		t.Fatalf("the invite form escaped the selected workspace: %d", status)
	}
	for range 2 {
		status, body, _ := b.request("POST", path, form, b.site.URL)
		if status != http.StatusOK || !strings.Contains(body, `"workspace":"client-b"`) {
			t.Fatalf("valid invitation or same-workspace retry failed: status=%d", status)
		}
	}
	if creates.Load() != 0 || b.invitationCount() != 1 {
		t.Fatal("retry duplicated an existing account or pending invitation")
	}
	form.Set("workspace", "client-c")
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusOK || b.invitationCount() != 2 {
		t.Fatalf("a second workspace did not preserve both invitations: %d", status)
	}
	var subject string
	var expires time.Time
	if err := b.pool.QueryRow(t.Context(), `SELECT subject,expires_at FROM portal_invitations WHERE client_id='client-b'`).Scan(&subject, &expires); err != nil {
		t.Fatal(err)
	}
	revoke := url.Values{"subject": {subject}, "workspace": {"client-b"}, "expires": {expires.Format(time.RFC3339Nano)}, "csrf_token": {form.Get("csrf_token")}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/invitations/revoke?focus=workspace", revoke, b.site.URL); status != http.StatusOK {
		t.Fatalf("current row could not be revoked: %d", status)
	}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusOK {
		t.Fatalf("customer could not be invited to the new workspace after revocation: %d", status)
	}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/invitations/revoke?focus=workspace", revoke, b.site.URL); status == http.StatusOK {
		t.Fatal("a stale form revoked the replacement invitation")
	}
	var pending bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT FROM portal_invitations WHERE client_id='client-c' AND NOT revoked)`).Scan(&pending); err != nil || !pending {
		t.Fatalf("replacement invitation was lost: %v", err)
	}
}
