package identity

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/open-feature/go-sdk/openfeature/memprovider"
)

func TestOperatorSupportViewKeepsOperatorAndCustomerBoundaries(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	other := customeridentity.DexSubject("other-account", customeridentity.ZitadelConnectorID)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES ($1,$2,'fixture-client','client'), ($1,$3,'other-client','client')`, b.f.issuer, customerSubject, other); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_requests(id,client_id,submission_key,product,title,brief,submitted_at) VALUES
 ('AAAAAAAAAAAAAAAAAAAAAAAAAA','fixture-client','AAAAAAAAAAAAAAAAAAAAAAAAAA','other','Private title','Private brief','2026-10-18T12:00:00Z'),
 ('BBBBBBBBBBBBBBBBBBBBBBBBBB','other-client','BBBBBBBBBBBBBBBBBBBBBBBBBB','app','Other title','Other brief','2026-09-01T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"subject": {customerSubject}, "workspace": {"fixture-client"}, "case": {"CASE-1234"}, "confirmed": {"yes"}, "csrf_token": {b.csrf("/portal/")}}
	openSupport := func() (int, string) {
		status, body, _ := b.request("POST", "/portal/operator/support?lang=en", form, b.site.URL)
		return status, body
	}
	if status, _ := openSupport(); status != http.StatusNotFound {
		t.Fatalf("the optional support route was not closed by default: %d", status)
	}
	supportFlags := map[string]memprovider.InMemoryFlag{}
	for _, key := range []string{"portal-identity", "portal-requests", "portal-support"} {
		supportFlags[key] = memprovider.InMemoryFlag{Key: key, State: memprovider.Enabled, DefaultVariant: "on", Variants: map[string]any{"on": true}}
	}
	if err := b.flags.SetProviderAndWait(t.Context(), memprovider.NewInMemoryProvider(supportFlags)); err != nil {
		t.Fatal(err)
	}
	form.Del("confirmed")
	if status, body := openSupport(); status < 400 || strings.Contains(body, "2026-10-18 12:00") {
		t.Fatal("support was granted without recording the client's request")
	}
	form.Set("confirmed", "yes")
	status, body := openSupport()
	if status != http.StatusOK || !strings.Contains(body, "Read-only support view") || !strings.Contains(body, "2026-10-18 12:00") {
		t.Fatalf("support did not show the selected workspace state: %d %s", status, body)
	}
	if !strings.Contains(body, "<strong>Other requests</strong>") {
		t.Errorf("support did not show the English label for an other request")
	}
	for _, value := range []string{"Private title", "Private brief", "2026-09-01 09:00", `action="/portal/requests`} {
		if strings.Contains(body, value) {
			t.Errorf("support disclosed unrelated data or a customer action: %q", value)
		}
	}
	var actor, target string
	if err := b.pool.QueryRow(t.Context(), `SELECT actor_subject,target FROM portal_operator_audit
 WHERE action='support.view' AND outcome='allowed'`).Scan(&actor, &target); err != nil || actor != operatorSubject || !strings.HasPrefix(target, "CASE-1234/") {
		t.Fatalf("the operator and case were not bound to the audit: %v", err)
	}
	var recordedBeforeDisclosure bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS (
 SELECT 1 FROM portal_operator_audit recorded JOIN portal_operator_audit disclosed ON disclosed.target=recorded.target
 WHERE recorded.action='support.request-recorded' AND recorded.actor_subject=$1
   AND disclosed.action='support.view' AND disclosed.outcome='allowed' AND recorded.at<disclosed.at)`,
		operatorSubject).Scan(&recordedBeforeDisclosure); err != nil || !recordedBeforeDisclosure {
		t.Fatalf("documented request was not recorded before disclosure: %v", err)
	}
	status, body, _ = b.request("POST", "/portal/operator/support?lang=da", form, b.site.URL)
	if status != http.StatusOK || !strings.Contains(body, "<strong>Andre opgaver</strong>") {
		t.Errorf("support did not show the Danish label for an other request: %d", status)
	}
	if status, body, _ := b.request("GET", "/portal/", nil, ""); status != http.StatusOK || !strings.Contains(body, "Operator workspace") {
		t.Fatal("support changed the operator session")
	}
	form.Set("workspace", "other-client")
	if status, body := openSupport(); status < 400 || strings.Contains(body, "2026-09-01 09:00") {
		t.Fatalf("a mismatched workspace reached another customer: %d", status)
	}
	form.Set("workspace", "fixture-client")
	form.Set("case", "private.email@example.test")
	if status, _ := openSupport(); status < 400 {
		t.Fatal("a free-text case reference was accepted")
	}
	form.Set("case", "CASE-1234")
	restoreAudit := pauseOperatorAudit(t)
	if status, body := openSupport(); status != http.StatusServiceUnavailable || strings.Contains(body, "2026-10-18 12:00") {
		t.Fatalf("support disclosed data when its audit log could not be written: %d", status)
	}
	restoreAudit()
	b.signOut()
	b.as(customerSubject)
	b.finish(b.start())
	form.Set("csrf_token", b.csrf("/portal/"))
	if status, body := openSupport(); status < 400 || strings.Contains(body, "2026-10-18 12:00") {
		t.Fatalf("an ordinary customer entered operator support: %d", status)
	}
	b.signOut()
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	form.Set("csrf_token", b.csrf("/portal/"))
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `CREATE FUNCTION fixture_revoke_support_member() RETURNS trigger
 LANGUAGE plpgsql AS $$
 BEGIN
   IF NEW.action='support.view' AND NEW.outcome='allowed' THEN
     UPDATE portal_memberships SET revoked=true WHERE client_id='fixture-client' AND role='client';
   END IF;
   RETURN NEW;
 END;
 $$;
 CREATE TRIGGER fixture_revoke_support_member AFTER INSERT ON portal_operator_audit
 FOR EACH ROW EXECUTE FUNCTION fixture_revoke_support_member()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := migrator.Exec(ctx, `DROP TRIGGER fixture_revoke_support_member ON portal_operator_audit;
 DROP FUNCTION fixture_revoke_support_member()`); err != nil {
			t.Error(err)
		}
	})
	if status, body := openSupport(); status != http.StatusServiceUnavailable || strings.Contains(body, "2026-10-18 12:00") {
		t.Fatalf("support disclosed data after the target membership was revoked: %d", status)
	}
}
