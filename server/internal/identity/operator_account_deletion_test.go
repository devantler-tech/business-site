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
	"github.com/jackc/pgx/v5"
)

type fixtureAccountDeletion struct {
	CustomerDirectory
	journal *fixtureDeletionJournal
	current CustomerDeletionTarget
	calls   int
	err     error
}

type fixtureDeletionJournal struct {
	issuer string
	target CustomerDeletionTarget
	err    error
}

func (j *fixtureDeletionJournal) RecordDeletionIntent(_ context.Context, issuer string, target CustomerDeletionTarget) error {
	if j.err != nil {
		return j.err
	}
	if j.issuer != "" && (j.issuer != issuer || j.target != target) {
		return errors.New("fixture journal refused an altered deletion target")
	}
	j.issuer, j.target = issuer, target
	return nil
}

func fixtureDeletionTarget(id string) CustomerDeletionTarget {
	return CustomerDeletionTarget{
		UserID: id, Organization: "fixture-customers",
		CreatedAt: time.Date(2021, time.November, 1, 12, 0, 0, 123456789, time.UTC),
	}
}

func (*fixtureAccountDeletion) Account(_ context.Context, id string) (customeridentity.Account, error) {
	return customeridentity.Account{ID: id, Profile: customeridentity.Profile{Name: "Fixture Customer", Email: "fixture@example.test"}}, nil
}

func (f *fixtureAccountDeletion) AuthorizeDeletion(_ context.Context, id string) (CustomerDeletionTarget, error) {
	if id != f.current.UserID {
		return CustomerDeletionTarget{}, ErrDenied
	}
	return f.current, nil
}

func (f *fixtureAccountDeletion) DeleteCustomerAccount(_ context.Context, target CustomerDeletionTarget, allowed func() bool) error {
	if target != f.current || f.journal.target != target || !allowed() {
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
	journal := &fixtureDeletionJournal{err: errors.New("fixture journal unavailable")}
	original := fixtureDeletionTarget("fixture-deletion")
	provider := &fixtureAccountDeletion{
		CustomerDirectory: &managementDirectory{}, journal: journal, current: original,
		err: errors.New("fixture provider unavailable"),
	}
	if err := b.svc.LinkCustomerDirectory(provider); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkCustomerAccountDeletion(provider, nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("account deletion was linked without independent recovery: %v", err)
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
	if err := b.svc.LinkCustomerAccountDeletion(provider, journal); err != nil {
		t.Fatal(err)
	}
	if status, page, _ := b.request("GET", detail+"&view=delete-account", nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "Delete customer account") || !strings.Contains(page, "Fixture Customer") ||
		!strings.Contains(page, "Type DELETE to confirm") {
		t.Fatalf("the account deletion confirmation was incomplete: %d", status)
	}
	danishDetail := customerAccountsPath + "?lang=da&account=" + subject + "&view=delete-account"
	if status, page, _ := b.request("GET", danishDetail, nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "Slet kundens konto") || !strings.Contains(page, "alle åbne invitationer fjernes") {
		t.Fatalf("the Danish account deletion confirmation was incomplete: %d", status)
	}
	protected := url.Values{"subject": {operatorSubject}, "confirm": {"DELETE"}, "csrf_token": form["csrf_token"]}
	if status, _, _ := b.request("POST", path, protected, b.site.URL); status != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("operator identity reached customer account deletion: %d", status)
	}
	badConfirmation := url.Values{"subject": {subject}, "confirm": {"remove"}, "csrf_token": form["csrf_token"]}
	if status, _, _ := b.request("POST", path, badConfirmation, b.site.URL); status != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("incorrect account confirmation reached provider removal: %d", status)
	}
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusServiceUnavailable || provider.calls != 0 {
		t.Fatalf("unrecorded deletion reached the sign-in provider: %d", status)
	}
	journal.err = nil
	provider.current.CreatedAt = original.CreatedAt.Add(time.Nanosecond)
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusServiceUnavailable || provider.calls != 0 || journal.target != original {
		t.Fatalf("retry under a reused identity did not preserve the original deletion target: %d", status)
	}
	provider.current = original
	if status, _, _ := b.request("POST", path, form, b.site.URL); status != http.StatusServiceUnavailable || provider.calls != 1 ||
		journal.issuer != b.f.issuer || journal.target != original {
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

func TestWorkspaceSwitchRefusesAnAccountDeletedAfterMembershipRead(t *testing.T) {
	b := newBrowserFixture(t)
	target := fixtureDeletionTarget("fixture-switch-deletion")
	subject := customeridentity.DexSubject(target.UserID, customeridentity.ZitadelConnectorID)
	b.as(subject)
	b.finish(b.startInvited(subject, "first", "client"))
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES($1,$2,'second','client')`, b.f.issuer, subject); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"workspace": {"second"}, "csrf_token": {b.csrf("/portal/workspaces?lang=en")}}
	pause := installCompletedRead(t, b, "SELECT m.incarnation::text FROM portal_memberships")
	resume := onceRelease(pause.release)
	defer resume()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	response := make(chan int, 1)
	go func() {
		status, _, _ := b.request("POST", "/portal/workspaces/open?lang=en", form, b.site.URL)
		response <- status
	}()
	select {
	case <-pause.blocked:
	case <-ctx.Done():
		t.Fatal("the workspace switch did not read the membership")
	}
	if err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		if err := lockIdentity(ctx, tx, b.f.issuer, subject); err != nil {
			return err
		}
		return stageCustomerAccountAccessRemoval(ctx, tx, b.f.issuer, subject, target)
	}); err != nil {
		t.Fatal(err)
	}
	resume()
	select {
	case status := <-response:
		if status != http.StatusForbidden {
			t.Fatalf("switching a deleted account returned %d", status)
		}
	case <-ctx.Done():
		t.Fatal("the workspace switch did not finish")
	}
}
