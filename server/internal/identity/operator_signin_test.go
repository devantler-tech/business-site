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

// TestLinkOperatorsAcceptsOnlyGitHubIdentities: the server list names GitHub
// identities through platform SSO, a few at most, never a customer.
func TestLinkOperatorsAcceptsOnlyGitHubIdentities(t *testing.T) {
	f := &Foundation{}
	many := make([]string, maxOperators+1)
	for i := range many {
		many[i] = customeridentity.DexSubject(strings.Repeat("g", i+1), customeridentity.PlatformConnectorID)
	}
	for name, subjects := range map[string][]string{
		"none":      nil,
		"empty":     {""},
		"customer":  {operatorSubject, customerSubject},
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
}

// TestOperatorSignsInThroughPlatformSSO: the operator link exists only with
// configured operators and names the platform connector; customers' sign-in
// names ZITADEL. A listed GitHub identity becomes the operator on first sign-in
// and keeps that one membership.
func TestOperatorSignsInThroughPlatformSSO(t *testing.T) {
	b := newBrowserFixture(t)
	if s, _, _ := b.request("GET", operatorLink, nil, ""); s != http.StatusNotFound {
		t.Fatalf("the operator link exists without configured operators: %d", s)
	}
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	for path, connector := range map[string]string{"/portal/sign-in": customeridentity.ZitadelConnectorID, operatorLink: customeridentity.PlatformConnectorID} {
		s, _, h := b.request("GET", path, nil, "")
		u, err := url.Parse(h.Get("Location"))
		if s != http.StatusSeeOther || err != nil || u.Query().Get("connector_id") != connector {
			t.Fatalf("%s did not name the %s connector: %d %s", path, connector, s, h.Get("Location"))
		}
	}
	b.as(operatorSubject)
	for range 2 {
		b.finish(b.startAt(operatorLink))
		if s, body, _ := b.request("GET", "/portal/operator?lang=da", nil, ""); s != http.StatusOK || !strings.Contains(body, operatorWorkspace) {
			t.Fatalf("the operator did not reach the operator page: %d", s)
		}
	}
	if b.memberships(operatorSubject, "operator") != 1 {
		t.Fatal("the operator does not hold exactly one operator membership")
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
	b.refused(b.startAt(operatorLink))
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
