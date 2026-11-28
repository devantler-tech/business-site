package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

type fixtureAccountDeletion struct {
	CustomerDirectory
	calls int
	err   error
}

func (*fixtureAccountDeletion) Account(_ context.Context, id string) (customeridentity.Account, error) {
	return customeridentity.Account{ID: id, Profile: customeridentity.Profile{Name: "Fixture Customer", Email: "fixture@example.test"}}, nil
}

func (f *fixtureAccountDeletion) AuthorizeDeletion(_ context.Context, id string) error {
	if id != "fixture-deletion" {
		return ErrDenied
	}
	return nil
}

func (f *fixtureAccountDeletion) DeleteCustomerAccount(_ context.Context, id string, allowed func() bool) error {
	if id != "fixture-deletion" || !allowed() {
		return ErrDenied
	}
	f.calls++
	return f.err
}

func TestDeleteCustomerAccountRequiresVerifiedProviderRemoval(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	subject := customeridentity.DexSubject("fixture-deletion", customeridentity.ZitadelConnectorID)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES ($1,$2,'client-a','client'),($1,$2,'client-b','client')`, b.f.issuer, subject); err != nil {
		t.Fatal(err)
	}
	b.invite(subject, "client-c", "client")
	operatorJar := b.client.Jar
	b.client.Jar, _ = cookiejar.New(nil)
	b.as(subject)
	b.finish(b.start())
	customerJar := b.client.Jar
	b.client.Jar = operatorJar
	provider := &fixtureAccountDeletion{CustomerDirectory: &managementDirectory{}, err: errors.New("fixture provider unavailable")}
	if err := b.svc.LinkCustomerDirectory(provider); err != nil {
		t.Fatal(err)
	}
	detail := customerAccountsPath + "?lang=en&account=" + subject
	if status, page, _ := b.request("GET", detail, nil, ""); status != http.StatusOK || strings.Contains(page, "Delete customer account") {
		t.Fatalf("the default directory offered provider deletion: %d", status)
	}
	path := operatorAPIRoot + "/customers/delete-account"
	form := url.Values{"subject": {subject}, "confirm": {"DELETE"}, "csrf_token": {b.csrf("/portal/")}}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status == http.StatusOK {
		t.Fatal("the default directory admitted provider deletion")
	}
	if err := b.svc.LinkCustomerAccountDeletion(provider); err != nil {
		t.Fatal(err)
	}
	if status, page, _ := b.request("GET", detail+"&view=delete-account", nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "Delete customer account") || !strings.Contains(page, "Fixture Customer") ||
		!strings.Contains(page, "Type DELETE to confirm") {
		t.Fatalf("the account deletion confirmation was incomplete: %d", status)
	}
	protected := url.Values{"subject": {operatorSubject}, "confirm": {"DELETE"}, "csrf_token": form["csrf_token"]}
	if status, _, _ := b.request("POST", path, protected, b.site.URL); status != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("operator identity reached customer account deletion: %d", status)
	}
	badConfirmation := url.Values{"subject": {subject}, "confirm": {"remove"}, "csrf_token": form["csrf_token"]}
	if status, _, _ := b.request("POST", path, badConfirmation, b.site.URL); status != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("incorrect account confirmation reached provider removal: %d", status)
	}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusServiceUnavailable || provider.calls != 1 {
		t.Fatalf("unconfirmed provider removal appeared successful: %d", status)
	}
	var accessEnded bool
	if err := b.pool.QueryRow(t.Context(), `SELECT
 NOT EXISTS (SELECT 1 FROM portal_memberships WHERE issuer=$1 AND subject=$2)
 AND NOT EXISTS (SELECT 1 FROM portal_invitations WHERE issuer=$1 AND subject=$2 AND NOT revoked)
 AND EXISTS (SELECT 1 FROM portal_customer_deletions WHERE issuer=$1 AND subject=$2 AND confirmed_at IS NULL)`,
		b.f.issuer, subject).Scan(&accessEnded); err != nil || !accessEnded {
		t.Fatalf("provider failure left stale workspace access or lost the deletion intent: %v", err)
	}
	pending := Invitation{Issuer: b.f.issuer, Subject: subject, ClientID: "client-d", Role: "client", Expires: time.Now().Add(time.Hour)}
	if err := b.svc.Invite(t.Context(), pending); !errors.Is(err, ErrDenied) {
		t.Fatalf("pending deletion allowed re-invitation of the old identity: %v", err)
	}

	b.client.Jar = customerJar
	if status, _, _ := b.request("GET", "/portal/workspaces?lang=en", nil, ""); status != http.StatusForbidden {
		t.Fatalf("pending deletion left the old session usable: %d", status)
	}
	b.client.Jar = operatorJar
	provider.err = nil
	if status, result, _ := b.request("POST", path, form, b.site.URL); status != http.StatusOK || !strings.Contains(result, "\"deleted\":true") || provider.calls != 2 {
		t.Fatalf("retry did not confirm deletion: status=%d effects=%d", status, provider.calls)
	}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusOK || provider.calls != 2 {
		t.Fatalf("completed deletion repeated the provider operation: %d", status)
	}
	b.signOut()
	b.as(subject)
	b.refused(b.start())
}
