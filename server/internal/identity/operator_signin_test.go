package identity

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
)

// Fictional GitHub identities through platform SSO, and a customer through ZITADEL.
var (
	operatorSubject = customeridentity.DexSubject(customeridentity.DexSubject("11111111", "github"), customeridentity.PlatformConnectorID)
	strangerSubject = customeridentity.DexSubject(customeridentity.DexSubject("22222222", "github"), customeridentity.PlatformConnectorID)
	customerSubject = customeridentity.DexSubject("345678901234567890", customeridentity.ZitadelConnectorID)
	sharedOperator  = customeridentity.DexSubject("456789012345678901", customeridentity.ZitadelConnectorID)
)

const operatorLink = "/portal/operator/sign-in?lang=da"

// as makes the synthetic broker issue subject from the next sign-in on.
func (b *browserFixture) as(subject string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subject = subject
}

// memberships counts subject's current memberships with role.
func (b *browserFixture) memberships(subject, role string) int {
	b.t.Helper()
	var n int
	if err := b.pool.QueryRow(b.t.Context(), `SELECT count(*) FROM portal_memberships WHERE subject=$1 AND role=$2 AND NOT revoked`, subject, role).Scan(&n); err != nil {
		b.t.Fatal(err)
	}
	return n
}

// refused requires the ordinary refusal (back to a fresh login, no page), then
// continues in a new browser.
func (b *browserFixture) refused(callback string) {
	b.t.Helper()
	if s, _, h := b.request("GET", callback, nil, ""); s != http.StatusSeeOther || h.Get("Location") != b.site.URL+"/portal/signed-out" {
		b.t.Fatalf("identity was not refused: %d %s", s, h.Get("Location"))
	}
	// A fresh browser, so these checks stay below the refusal limit.
	b.client.Jar, _ = cookiejar.New(nil)
}

func (b *browserFixture) requireClientWorkspace(client string) {
	b.t.Helper()
	if s, body, _ := b.request("GET", "/portal/?lang=da", nil, ""); s != http.StatusOK || !strings.Contains(body, client) {
		b.t.Fatalf("the invited client did not reach their workspace: %d", s)
	}
	if s, _, _ := b.request("GET", "/portal/operator", nil, ""); s != http.StatusForbidden {
		b.t.Fatalf("the invited client reached the operator area: %d", s)
	}
}

func TestLinkOperatorsAcceptsOnlySupportedDexSubjects(t *testing.T) {
	f := &Foundation{}
	many := make([]string, maxOperators+1)
	for i := range many {
		many[i] = customeridentity.DexSubject(strings.Repeat("g", i+1), customeridentity.PlatformConnectorID)
	}
	for name, subjects := range map[string][]string{
		"none":      nil,
		"empty":     {""},
		"unknown":   {operatorSubject, customeridentity.DexSubject("456789012345678901", "other")},
		"malformed": {"operator-subject"},
		"duplicate": {operatorSubject, operatorSubject},
		"too many":  many,
	} {
		if err := f.LinkOperators(subjects); !errors.Is(err, ErrDenied) || f.OperatorsLinked() {
			t.Errorf("%s operator list was accepted", name)
		}
	}
	if err := f.LinkOperators(many[:maxOperators]); err != nil || !f.OperatorsLinked() {
		t.Fatal("a valid operator list was refused")
	}
	if err := f.LinkOperators([]string{sharedOperator, operatorSubject}); err != nil {
		t.Fatal("a list with shared and legacy operator identities was refused:", err)
	}
}

func TestPrivatePreviewOnlyAdmitsItsListedOperator(t *testing.T) {
	b := newBrowserFixture(t)
	// Prepare the same records and token claims an ordinary portal accepts.
	// The preview must refuse them even if such data is present in its database.
	existing := enroll(t, b.svc, b.f, customerSubject, "fixture-client", "client")
	pendingSubject := customeridentity.DexSubject("567890123456789012", customeridentity.ZitadelConnectorID)
	pending := invitedAttempt(t, b.svc, b.f, pendingSubject, "fixture-client", "client")
	pendingToken := b.f.token(t, pendingSubject, pending.Nonce, nil)

	if err := b.svc.LinkPreviewOperator(operatorSubject); !errors.Is(err, ErrDenied) {
		t.Fatal("the preview accepted a legacy sign-in subject")
	}
	if err := b.svc.LinkPreviewOperator(sharedOperator); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.Authorize(t.Context(), existing, "fixture-client", false); !errors.Is(err, ErrDenied) {
		t.Error("an existing customer was authorized in the preview")
	}
	if _, err := b.svc.Complete(t.Context(), pending.State, pending.BrowserBinding, pendingToken); !errors.Is(err, ErrDenied) {
		t.Error("a pending customer invitation was redeemed in the preview")
	}
	newSubject := customeridentity.DexSubject("678901234567890123", customeridentity.ZitadelConnectorID)
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, newSubject, "fixture-client", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
		t.Error("a customer invitation could be issued in the preview")
	}
	b.as(sharedOperator)
	b.finish(b.start())
	if status, _, _ := b.request("GET", "/portal/operator", nil, ""); status != http.StatusOK {
		t.Fatalf("the listed operator could not sign in: %d", status)
	}
	if err := b.svc.Revoke(t.Context(), b.f.issuer, sharedOperator); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := b.request("GET", "/portal/operator", nil, ""); status != http.StatusForbidden {
		t.Fatalf("the operator retained preview access after revocation: %d", status)
	}
}

func TestSharedSignInOffersConfiguredMethods(t *testing.T) {
	b := newBrowserFixture(t)
	for _, path := range []string{"/portal/?lang=da", "/portal/sign-in?lang=da"} {
		s, _, h := b.request("GET", path, nil, "")
		address, err := url.Parse(h.Get("Location"))
		if s != http.StatusSeeOther || err != nil || address.Query().Get("connector_id") != "customer-zitadel" {
			t.Fatalf("%s did not start customer sign-in before operators were configured: status %d", path, s)
		}
	}
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portal/?lang=da", "/portal/sign-in?lang=da"} {
		s, _, h := b.request("GET", path, nil, "")
		address, err := url.Parse(h.Get("Location"))
		if s != http.StatusSeeOther || err != nil || address.Query().Has("connector_id") {
			t.Fatalf("%s pinned a provider instead of opening the shared sign-in page: status %d", path, s)
		}
	}
}

// The operator bookmark still selects platform SSO. A listed GitHub identity
// can enter through either link and keeps the same operator membership.
func TestOperatorSignsInThroughPlatformSSO(t *testing.T) {
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	s, _, h := b.request("GET", operatorLink, nil, "")
	u, err := url.Parse(h.Get("Location"))
	if s != http.StatusSeeOther || err != nil || u.Query().Get("connector_id") != customeridentity.PlatformConnectorID {
		t.Fatalf("the operator bookmark did not select platform SSO: status %d", s)
	}
	b.as(operatorSubject)
	for _, link := range []string{"/portal/?lang=da", operatorLink} {
		b.finish(b.startAt(link))
		if s, body, _ := b.request("GET", "/portal/?lang=da", nil, ""); s != http.StatusOK || !strings.Contains(body, operatorWorkspace) {
			t.Fatalf("the operator did not reach the operator page: %d", s)
		}
	}
	if b.memberships(operatorSubject, "operator") != 1 {
		t.Fatal("the operator does not hold exactly one operator membership")
	}
}

func TestOperatorAndCustomerUseTheSameSignIn(t *testing.T) {
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{sharedOperator, operatorSubject}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/portal/?lang=da", "/portal/sign-in?lang=da", operatorLink} {
		s, _, h := b.request("GET", path, nil, "")
		address, err := url.Parse(h.Get("Location"))
		if s != http.StatusSeeOther || err != nil || address.Query().Get("connector_id") != customeridentity.ZitadelConnectorID {
			t.Fatalf("%s did not start the same sign-in: status %d", path, s)
		}
	}
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, sharedOperator, "client-a", "client", time.Now().Add(time.Hour)}); !errors.Is(err, ErrDenied) {
		t.Fatal("the operator identity could be invited as a client:", err)
	}
	b.configure(map[string]any{"email": "shared-sign-in@example.invalid", "email_verified": true}, false)
	b.as(sharedOperator)
	b.finish(b.start())
	if s, _, _ := b.request("GET", "/portal/operator", nil, ""); s != http.StatusOK {
		t.Fatalf("the configured account did not open the operator area: %d", s)
	}
	if b.memberships(sharedOperator, "operator") != 1 {
		t.Fatal("shared sign-in did not provision exactly one operator membership")
	}
	b.signOut()
	// A different identity with identical broker email claims is still a client.
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, customerSubject, "client-a", "client", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	b.as(customerSubject)
	b.finish(b.start())
	b.requireClientWorkspace("client-a")
	b.signOut()
	b.as(sharedOperator)
	b.finish(b.start())
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := b.request("GET", "/portal/operator", nil, ""); s != http.StatusForbidden {
		t.Fatalf("the operator retained access after removal from the list: %d", s)
	}
	if err := b.svc.LinkOperators([]string{sharedOperator}); err != nil {
		t.Fatal(err)
	}
	if err := b.svc.Revoke(t.Context(), b.f.issuer, sharedOperator); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := b.request("GET", "/portal/operator", nil, ""); s != http.StatusForbidden {
		t.Fatalf("the revoked operator kept access: %d", s)
	}
	b.client.Jar, _ = cookiejar.New(nil)
	b.refused(b.start())
}

func TestOperatorGrantDoesNotPromoteAnExistingClient(t *testing.T) {
	b := newBrowserFixture(t)
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, sharedOperator, "client-a", "client", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	b.as(sharedOperator)
	b.finish(b.start())
	b.signOut()
	if err := b.svc.LinkOperators([]string{sharedOperator}); err != nil {
		t.Fatal(err)
	}
	b.refused(b.start())
	if b.memberships(sharedOperator, "operator") != 0 || b.memberships(sharedOperator, "client") != 1 {
		t.Fatal("the configured grant altered an existing client membership")
	}
}

// TestGitHubIdentityIsListedOperatorOrNobody: an unlisted GitHub identity is
// refused like an uninvited customer and gains nothing; no invitation can name
// one; it never holds another role; revoking an operator or removing it from the list ends its
// access; and a customer never holds the operator role.
func TestGitHubIdentityIsListedOperatorOrNobody(t *testing.T) {
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	b.as(strangerSubject)
	b.refused(b.start())
	if b.memberships(strangerSubject, "operator")+b.memberships(strangerSubject, "client") != 0 {
		t.Fatal("an unlisted GitHub identity gained a membership")
	}
	for _, i := range []Invitation{
		{b.f.issuer, strangerSubject, "client-a", "client", time.Now().Add(time.Hour)},
		{b.f.issuer, operatorSubject, operatorWorkspace, "operator", time.Now().Add(time.Hour)},
		{b.f.issuer, customerSubject, operatorWorkspace, "operator", time.Now().Add(time.Hour)},
	} {
		if err := b.svc.Invite(t.Context(), i); !errors.Is(err, ErrDenied) {
			t.Fatalf("an invitation named %s as %s", i.Subject, i.Role)
		}
	}
	if err := b.svc.Invite(t.Context(), Invitation{b.f.issuer, customerSubject, "client-a", "client", time.Now().Add(time.Hour)}); err != nil {
		t.Fatal("a customer could not be invited as a client:", err)
	}
	b.as(customerSubject)
	b.finish(b.start())
	b.requireClientWorkspace("client-a")
	b.client.Jar, _ = cookiejar.New(nil)

	// A listed GitHub identity holding the client role is refused.
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,'client-a','client')`, b.f.issuer, operatorSubject); err != nil {
		t.Fatal(err)
	}
	b.as(operatorSubject)
	b.refused(b.startAt(operatorLink))

	// A revoked operator stays refused while it is still listed.
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET role='operator', client_id=$2, revoked=true WHERE subject=$1`, operatorSubject, operatorWorkspace); err != nil {
		t.Fatal(err)
	}
	b.refused(b.startAt(operatorLink))

	// Removing a signed-in operator from the list ends its access at the next request.
	if _, err := b.pool.Exec(t.Context(), `UPDATE portal_memberships SET revoked=false WHERE subject=$1`, operatorSubject); err != nil {
		t.Fatal(err)
	}
	b.finish(b.startAt(operatorLink))
	if err := b.svc.LinkOperators([]string{strangerSubject}); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := b.request("GET", "/portal/operator", nil, ""); s != http.StatusForbidden {
		t.Fatalf("an unlisted operator kept the operator page: %d", s)
	}

	// A customer through ZITADEL with an operator membership is refused.
	customerOperator := customeridentity.DexSubject("customer-operator", customeridentity.ZitadelConnectorID)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES($1,$2,$3,'operator')`, b.f.issuer, customerOperator, operatorWorkspace); err != nil {
		t.Fatal(err)
	}
	b.as(customerOperator)
	b.refused(b.start())
}
