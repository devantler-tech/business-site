package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// managementDirectory replaces only the external identity-provider boundary.
// Real HTTP handlers, sessions, feature evaluation and PostgreSQL remain in use.
type managementDirectory struct {
	change   func(context.Context, string, bool) error
	profiles func([]string) map[string]customeridentity.Profile
}

func (*managementDirectory) SearchAccounts(context.Context, string, uint64) ([]customeridentity.Account, bool, error) {
	return nil, false, ErrDenied
}

func (*managementDirectory) Account(context.Context, string) (customeridentity.Account, error) {
	return customeridentity.Account{}, ErrDenied
}

func (d *managementDirectory) Profiles(_ context.Context, ids []string) (map[string]customeridentity.Profile, error) {
	if d.profiles == nil {
		return nil, ErrDenied
	}
	return d.profiles(ids), nil
}

func (*managementDirectory) EnsureUser(context.Context, string, string, func() bool) (string, bool, error) {
	return "", false, ErrDenied
}

func (*managementDirectory) VerifyUser(context.Context, string) error {
	return ErrDenied
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

// managementMemberFixture creates a current customer and replaces only the
// provider call; management requests still use the real authority boundary.
func managementMemberFixture(t *testing.T, change func(context.Context, string, bool) error) *browserFixture {
	t.Helper()
	b := newOperatorBrowserFixture(t)
	return linkManagedMember(t, b, change)
}

func linkManagedMember(t *testing.T, b *browserFixture, change func(context.Context, string, bool) error) *browserFixture {
	t.Helper()
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,'client-a','client')`, b.f.issuer, customerSubject); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.LinkCustomerDirectory(&managementDirectory{change: change}); err != nil {
		t.Fatal(err)
	}
	return b
}

func operatorMembershipForm(b *browserFixture, subject, workspace string) url.Values {
	b.t.Helper()
	return url.Values{"workspace": {workspace}, "subject": {subject}, "csrf_token": {b.csrf("/portal/")}}
}

func (b *browserFixture) customerRevoked() bool {
	b.t.Helper()
	var revoked bool
	if err := b.pool.QueryRow(b.t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil {
		b.t.Fatal(err)
	}
	return revoked
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
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE subject=$1`, customerSubject); err != nil {
		t.Fatal(err)
	}
	form := operatorMembershipForm(b, customerSubject, "client-a")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/reactivate", form, b.site.URL); status != http.StatusOK {
		t.Fatalf("legacy recovery status=%d", status)
	}
	b.client.Jar, _ = cookiejar.New(nil)
	b.client.Jar.SetCookies(address, customerCookies)
	if status, _, _ := b.request("GET", "/portal/contact", nil, ""); status == http.StatusOK {
		t.Fatal("old customer session retained workspace access after legacy recovery")
	}
	b.client.Jar, _ = cookiejar.New(nil)
	b.as(customerSubject)
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK {
		t.Fatal("reactivated customer could not sign in", status)
	}
	var scheduled bool
	err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_customer_account_closures)`).Scan(&scheduled)
	if err != nil || scheduled {
		t.Fatalf("reversible access changes left an account-removal deadline: %v", err)
	}
}

// TestOperatorProviderFailureLeavesPortalClosed catches access being granted
// before recovery of a legacy revoked membership is confirmed.
func TestOperatorProviderFailureLeavesPortalClosed(t *testing.T) {
	b := managementMemberFixture(t, func(context.Context, string, bool) error {
		return errors.New("fictional provider unavailable")
	})
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=true WHERE subject=$1`, customerSubject); err != nil {
		t.Fatal(err)
	}
	form := operatorMembershipForm(b, customerSubject, "client-a")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/reactivate", form, b.site.URL); status != http.StatusServiceUnavailable {
		t.Fatalf("failed provider recovery status=%d", status)
	}
	if !b.customerRevoked() {
		t.Fatal("provider failure left portal access enabled")
	}
}
