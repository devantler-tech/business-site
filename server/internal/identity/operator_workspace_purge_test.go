package identity

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestOperatorPermanentlyDeletesExactClosedWorkspace(t *testing.T) {
	b := operatorWithWorkspaceCustomer(t)
	ctx := t.Context()
	db := migrationDB(t)
	if _, err := db.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('closed-sibling');
 INSERT INTO portal_workspace_settings(workspace_id,display_name,closed_at)
 VALUES('closed-sibling','North Lab','2026-11-30 10:00:00+01')`); err != nil {
		t.Fatal(err)
	}
	token := b.csrf("/portal/operator/customers")
	b.invite("old-subject", "fixture-workspace", "client")
	closeForm := url.Values{"workspace": {"fixture-workspace"}, "confirm": {"fixture-workspace"}, "csrf_token": {token}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", closeForm, b.site.URL); status != http.StatusOK {
		t.Fatalf("close fixture workspace: %d", status)
	}
	status, page, _ := b.request("GET", "/portal/operator/customers?view=closed&lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Retention ends") || !strings.Contains(page, "2029-11-30") ||
		!strings.Contains(page, "Delete permanently") {
		t.Fatalf("Closed workspaces did not show the retention date and deletion action: %d", status)
	}

	confirmPath := "/portal/operator/customers?view=purge-confirm&workspace=fixture-workspace&lang=en"
	if status, page, _ := b.request("GET", confirmPath, nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "Approved customer deletion request") || !strings.Contains(page, "Sign-in accounts") {
		t.Fatalf("deletion review did not show the scope and reason: %d", status)
	}
	var closed string
	if err := b.pool.QueryRow(ctx, `SELECT to_char(closed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
 FROM portal_workspace_settings WHERE workspace_id='fixture-workspace'`).Scan(&closed); err != nil {
		t.Fatal(err)
	}
	path := "/portal/operator/customers/purge?lang=en"
	form := url.Values{"workspace": {"fixture-workspace"}, "closed": {closed}, "confirm": {"fixture-workspace"},
		"reason": {"request"}, "csrf_token": {token}}
	for field, bad := range map[string]string{"closed": "2020-01-01T00:00:00Z", "confirm": "other", "reason": "other"} {
		valid := form.Get(field)
		form.Set(field, bad)
		if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusBadRequest {
			t.Fatalf("invalid %s returned %d", field, status)
		}
		form.Set(field, valid)
	}
	if status, _, _ := b.request("POST", path, form, ""); status != http.StatusForbidden {
		t.Fatalf("request without same-origin proof returned %d", status)
	}
	status, header := managementNativeRequest(b, path, form)
	if status != http.StatusSeeOther || !strings.Contains(header.Get("Location"), "view=closed") {
		t.Fatalf("confirmed deletion did not return to Closed workspaces: %d", status)
	}
	var deleted, sibling, deadline, audit bool
	if err := db.QueryRow(ctx, `SELECT
 NOT EXISTS (SELECT 1 FROM portal_workspaces WHERE id='fixture-workspace')
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings WHERE workspace_id='fixture-workspace')
 AND NOT EXISTS (SELECT 1 FROM portal_invitations WHERE client_id='fixture-workspace'),
 EXISTS (SELECT 1 FROM portal_workspaces WHERE id='closed-sibling'),
 EXISTS (SELECT 1 FROM portal_customer_account_closures c JOIN portal_membership_transitions h
 ON h.incarnation=c.membership_event WHERE h.client_id='fixture-workspace'),
 EXISTS (SELECT 1 FROM portal_operator_audit WHERE action='workspaces.purge-request'
 AND target='fixture-workspace' AND outcome='allowed')`).Scan(&deleted, &sibling, &deadline, &audit); err != nil ||
		!deleted || !sibling || !deadline || !audit {
		t.Fatalf("scope or durable deletion incorrect (%v %v %v %v): %v", deleted, sibling, deadline, audit, err)
	}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("replay did not stop at the deleted workspace: %d", status)
	}
}

func TestEarlyWorkspaceDeletionRefusesExistingAccess(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	ctx := t.Context()
	db := migrationDB(t)
	b.invite("current-contact", "still-has-access", "client")
	if _, err := db.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('still-has-access');
 INSERT INTO portal_workspace_settings(workspace_id,closed_at) VALUES('still-has-access','2026-10-31 12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	status, page, _ := b.request("GET", "/portal/operator/customers?view=closed&lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Access is still open") || strings.Contains(page, "view=purge-confirm") {
		t.Fatalf("the directory advertised early deletion of a workspace with access: %d", status)
	}
	form := url.Values{"workspace": {"still-has-access"}, "closed": {"2026-10-31T12:00:00Z"}, "confirm": {"still-has-access"},
		"reason": {"test"}, "csrf_token": {b.csrf("/portal/operator/customers")}}
	if status, _, _ := b.request("POST", "/portal/operator/customers/purge", form, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("early deletion with existing access returned %d", status)
	}
	if _, err := db.Exec(ctx, `UPDATE portal_invitations SET revoked=true WHERE client_id='still-has-access';
 INSERT INTO portal_numina_contacts VALUES('still-has-access','bookkeeping-fixture')`); err != nil {
		t.Fatal(err)
	}
	status, page, _ = b.request("GET", "/portal/operator/customers?view=closed&lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Records require retention") || strings.Contains(page, "view=purge-confirm") {
		t.Fatalf("the directory advertised early deletion of a workspace with accounting records: %d", status)
	}
	if status, _, _ := b.request("POST", "/portal/operator/customers/purge", form, b.site.URL); status != http.StatusBadRequest {
		t.Fatalf("early deletion with accounting records returned %d", status)
	}
	var retained bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_numina_contacts WHERE client_id='still-has-access')`).Scan(&retained); err != nil || !retained {
		t.Fatalf("accounting records were lost: %v", err)
	}
}
