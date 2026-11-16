package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

// managementDirectory replaces only the external identity-provider boundary.
// Real HTTP handlers, sessions, feature evaluation and PostgreSQL remain in use.
type managementDirectory struct {
	change func(context.Context, string, bool) error
}

func (*managementDirectory) EnsureUser(context.Context, string, string, func() bool) (string, error) {
	return "", ErrDenied
}

func (d *managementDirectory) DeactivateUser(ctx context.Context, user string, allowed func() bool) error {
	if allowed == nil || !allowed() {
		return ErrDenied
	}
	return d.change(ctx, user, false)
}

func (d *managementDirectory) ReactivateUser(ctx context.Context, user string, allowed func() bool) error {
	if allowed == nil || !allowed() {
		return ErrDenied
	}
	return d.change(ctx, user, true)
}

// TestOperatorMemberAccessTransitions catches enabling portal membership before
// provider reactivation, or retaining a customer's old authenticated session.
func TestOperatorMemberAccessTransitions(t *testing.T) {
	b := newBrowserFixture(t)
	b.as(customerSubject)
	b.finish(b.startInvited(customerSubject, "client-a", "client"))
	address, _ := url.Parse(b.site.URL)
	customerCookies := b.client.Jar.Cookies(address)
	b.client.Jar, _ = cookiejar.New(nil)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	operatorJar := b.client.Jar
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{change: func(ctx context.Context, user string, active bool) error {
		if user != "345678901234567890" {
			t.Error("wrong upstream identity", user)
		}
		var revoked bool
		if err := b.pool.QueryRow(ctx, `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil || !revoked {
			t.Error("provider called before portal access was disabled", active, revoked, err)
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"deactivate", "reactivate"} {
		form := url.Values{"workspace": {"client-a"}, "subject": {customerSubject}, "csrf_token": {b.csrf("/portal/")}}
		status, _, _ := b.request("POST", operatorAPIRoot+"/members/"+action, form, b.site.URL)
		if status != http.StatusOK {
			t.Fatalf("%s status=%d", action, status)
		}
		b.client.Jar, _ = cookiejar.New(nil)
		b.client.Jar.SetCookies(address, customerCookies)
		if status, _, _ := b.request("GET", "/portal/", nil, ""); status == http.StatusOK {
			t.Fatalf("old customer session survived %s", action)
		}
		b.client.Jar = operatorJar
	}
	b.client.Jar, _ = cookiejar.New(nil)
	b.as(customerSubject)
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK {
		t.Fatal("reactivated customer could not sign in", status)
	}
}

// TestOperatorProviderFailureLeavesPortalClosed catches a network/API failure
// that rolls local deactivation back or allows reactivation before confirmation.
func TestOperatorProviderFailureLeavesPortalClosed(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,'client-a','client')`, b.f.issuer, customerSubject); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{change: func(context.Context, string, bool) error {
		return errors.New("fictional provider unavailable")
	}}); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"deactivate", "reactivate"} {
		form := url.Values{"workspace": {"client-a"}, "subject": {customerSubject}, "csrf_token": {b.csrf("/portal/")}}
		status, _, _ := b.request("POST", operatorAPIRoot+"/members/"+action, form, b.site.URL)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("failed provider %s status=%d", action, status)
		}
		var revoked bool
		if err := b.pool.QueryRow(t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil || !revoked {
			t.Fatal("provider failure left portal access enabled", action, revoked, err)
		}
	}
}
