package identity

import (
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestOperatorAuditFiltersAndPages(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	if _, err := b.pool.Exec(t.Context(), `
 INSERT INTO portal_operator_audit(id,at,actor_issuer,actor_subject,actor_kind,action,target,outcome)
 SELECT ('f0000000-0000-4000-8000-'||lpad(n::text,12,'0'))::uuid,'2026-03-04 09:00Z'::timestamptz,$1,
 'fixture-agent','agent','workspaces.rename','North '||lpad(n::text,2,'0'),'refused'
 FROM generate_series(1,31) n
 UNION ALL SELECT 'f0000000-0000-4000-8000-000000000032'::uuid,'2026-03-04 09:00Z'::timestamptz,$1,
 'fixture-operator','operator','workspaces.rename','North 32','allowed'
 UNION ALL SELECT 'f0000000-0000-4000-8000-000000000033'::uuid,'2026-03-05 00:00Z'::timestamptz,$1,
 'fixture-agent','agent','workspaces.rename','North 33','refused'`, b.f.issuer); err != nil {
		t.Fatal(err)
	}
	path := "/portal/operator/audit?lang=en&q=north&actor=agent&outcome=refused&from=2026-03-04&to=2026-03-04"
	status, first, _ := b.request("GET", path, nil, "")
	if status != http.StatusOK || !strings.Contains(first, "North 31") || strings.Contains(first, "North 01") ||
		strings.Contains(first, "North 32") || strings.Contains(first, "North 33") {
		t.Fatalf("first filtered audit page is incorrect: %d", status)
	}
	older := auditPageLink(t, first, "Older events")
	status, second, _ := b.request("GET", older, nil, "")
	if status != http.StatusOK || !strings.Contains(second, "North 01") || strings.Contains(second, "North 31") {
		t.Fatalf("second audit page repeated or lost tied events: %d", status)
	}
	status, returned, _ := b.request("GET", auditPageLink(t, second, "Newer events"), nil, "")
	if status != http.StatusOK || !strings.Contains(returned, "North 31") || strings.Contains(returned, "North 01") {
		t.Fatalf("returning to newer events lost the filters: %d", status)
	}
	for _, invalid := range []string{"?before=bad", "?actor=admin", "?to=2026-02-30", "?q=x&q=y"} {
		if status, _, _ := b.request("GET", "/portal/operator/audit"+invalid, nil, ""); status != http.StatusBadRequest {
			t.Errorf("invalid search returned %d for %s", status, invalid)
		}
	}
	b.signOut()
	status, body, _ := b.request("GET", path, nil, "")
	if status != http.StatusForbidden || strings.Contains(body, "North 31") {
		t.Fatalf("signed-out request disclosed audit data: %d", status)
	}
}

func auditPageLink(t *testing.T, body, label string) string {
	t.Helper()
	pattern := regexp.MustCompile(fmt.Sprintf(`href="([^"]+)">%s</a>`, regexp.QuoteMeta(label)))
	match := pattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("missing audit page link %q", label)
	}
	return html.UnescapeString(match[1])
}
