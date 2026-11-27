package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// managementDirectory replaces only the external identity-provider boundary.
// Real HTTP handlers, sessions, feature evaluation and PostgreSQL remain in use.
type managementDirectory struct {
	change   func(context.Context, string, bool) error
	profiles func([]string) map[string]customeridentity.Profile
}

func (d *managementDirectory) Profiles(_ context.Context, ids []string) (map[string]customeridentity.Profile, error) {
	if d.profiles == nil {
		return nil, ErrDenied
	}
	return d.profiles(ids), nil
}

func (*managementDirectory) EnsureUser(context.Context, string, string, func() bool) (string, error) {
	return "", ErrDenied
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
		form := operatorMembershipForm(b, customerSubject, "client-a")
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
	var scheduled bool
	err := b.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_customer_account_closures)`).Scan(&scheduled)
	if err != nil || scheduled {
		t.Fatalf("reversible access changes left an account-removal deadline: %v", err)
	}
}

// TestOperatorProviderFailureLeavesPortalClosed catches a network/API failure
// that rolls local deactivation back or allows reactivation before confirmation.
func TestOperatorProviderFailureLeavesPortalClosed(t *testing.T) {
	b := managementMemberFixture(t, func(context.Context, string, bool) error {
		return errors.New("fictional provider unavailable")
	})
	for _, action := range []string{"deactivate", "reactivate"} {
		form := operatorMembershipForm(b, customerSubject, "client-a")
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

// TestOperatorRetriesDeactivationWithoutRestoringAccess catches a phone flow
// that offers only reactivation after provider failure and cannot finish closing
// provider access while the portal membership stays disabled.
func TestOperatorRetriesDeactivationWithoutRestoringAccess(t *testing.T) {
	var calls, enableCalls atomic.Int32
	b := managementMemberFixture(t, func(_ context.Context, _ string, active bool) error {
		if active {
			enableCalls.Add(1)
		}
		if calls.Add(1) == 1 {
			return errors.New("fictional provider unavailable")
		}
		return nil
	})
	form := operatorMembershipForm(b, customerSubject, "client-a")
	if status, _, _ := b.request("POST", operatorAPIRoot+"/members/deactivate", form, b.site.URL); status != http.StatusServiceUnavailable {
		t.Fatal("the failed provider did not retain its failure", status)
	}
	status, body, _ := b.request("GET", "/portal/operator/customers?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Retry deactivation") {
		t.Fatal("disabled member has no deactivation retry in the inventory", status)
	}
	form = workNativeForm(t, body, `members/deactivate\?lang=en`)
	status, headers := managementNativeRequest(b, operatorAPIRoot+"/members/deactivate?lang=en", form)
	if status != http.StatusSeeOther || headers.Get("Location") != "/portal/operator/customers?lang=en" {
		t.Fatal("native deactivation retry did not complete", status)
	}
	var revoked bool
	if err := b.pool.QueryRow(t.Context(), `SELECT revoked FROM portal_memberships WHERE subject=$1`, customerSubject).Scan(&revoked); err != nil || !revoked || calls.Load() != 2 || enableCalls.Load() != 0 {
		t.Fatal("retry restored access or skipped the provider", revoked, calls.Load(), enableCalls.Load(), err)
	}
}
