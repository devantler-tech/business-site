package identity

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

func TestReturningCustomerReceivesWorkspaceInvitation(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	ctx := t.Context()
	sender := new(fakeMailer)
	if err := b.svc.EnableMail(sender); err != nil {
		t.Fatal(err)
	}
	created := inviteDirectory(t, b, false, false, nil)
	form := invitationForm(b)
	if status, _, _ := b.request("POST", operatorAPIRoot+"/invitations", form, b.site.URL); status != http.StatusOK {
		t.Fatalf("initial invite returned %d", status)
	}
	if err := b.svc.SendQueuedMail(ctx); err != nil || len(sender.messages()) != 0 || created.Load() != 1 {
		t.Fatalf("new account did not rely on the native setup email: %v", err)
	}
	customer := customeridentity.DexSubject("customer-42", customeridentity.ZitadelConnectorID)
	b.signOut()
	b.as(customer)
	b.finish(b.start())
	b.signOut()
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	token := b.csrf("/portal/operator/customers")
	closeForm := url.Values{"workspace": {"client-b"}, "confirm": {"client-b"}, "csrf_token": {token}}
	if status, _, _ := b.request("POST", operatorAPIRoot+"/workspaces/delete", closeForm, b.site.URL); status != http.StatusOK {
		t.Fatalf("closing former workspace returned %d", status)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES('new-workspace');
 INSERT INTO portal_workspace_settings(workspace_id,display_name) VALUES('new-workspace','New studio')`); err != nil {
		t.Fatal(err)
	}
	created = inviteDirectory(t, b, true, false, nil)
	form.Set("workspace", "new-workspace")
	form.Set("csrf_token", token)
	path := operatorAPIRoot + "/invitations?focus=workspace&lang=en"
	for range 2 {
		if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusOK {
			t.Fatalf("returning customer invitation returned %d", status)
		}
	}
	if created.Load() != 0 {
		t.Fatal("the provider account was replaced")
	}
	const workspacePage = "/portal/operator/customers?lang=en&workspace=new-workspace"
	if status, page, _ := b.request("GET", workspacePage, nil, ""); status != http.StatusOK || !strings.Contains(page, "Email pending") {
		t.Fatalf("the workspace did not show a pending email: %d", status)
	}
	sender.fail = true
	if err := b.svc.SendQueuedMail(ctx); err == nil {
		t.Fatal("SMTP failure was reported as delivery")
	}
	if status, page, _ := b.request("GET", workspacePage, nil, ""); status != http.StatusOK || !strings.Contains(page, "Retrying email") {
		t.Fatalf("the workspace did not show that delivery would retry: %d", status)
	}
	sender.fail = false
	for range 2 {
		if err := b.svc.SendQueuedMail(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mail := sender.messages()
	if len(mail) != 1 || mail[0].To != form.Get("email") ||
		!strings.Contains(mail[0].Body, "sign in") || !strings.Contains(mail[0].Body, b.site.URL+"/portal/sign-in") {
		t.Fatalf("returning customer did not receive a single shared-login invitation: %d messages", len(mail))
	}
	if status, page, _ := b.request("GET", workspacePage, nil, ""); status != http.StatusOK || !strings.Contains(page, "Email sent") {
		t.Fatalf("the workspace did not show delivery: %d", status)
	}

	b.signOut()
	b.as(customer)
	b.finish(b.start())
	status, page, _ := b.request("GET", "/portal/", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "New studio") || strings.Contains(page, "client-b") {
		t.Fatalf("the shared login did not land in the newly invited workspace: %d", status)
	}
}
