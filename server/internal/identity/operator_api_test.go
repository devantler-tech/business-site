package identity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newOperatorBrowserFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	return b
}

// auditRows returns the recorded actions as "kind action outcome", oldest first.
func (b *browserFixture) auditRows() []string {
	b.t.Helper()
	rows, err := b.pool.Query(b.t.Context(), `SELECT actor_kind||' '||action||' '||outcome FROM portal_operator_audit ORDER BY at,id`)
	if err != nil {
		b.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var row string
		if err := rows.Scan(&row); err != nil {
			b.t.Fatal(err)
		}
		out = append(out, row)
	}
	return out
}

// TestOperatorOpenAPIDescribesEveryRoute: the OpenAPI file and the registered
// operator routes name the same paths, methods and actions.
func TestOperatorOpenAPIDescribesEveryRoute(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(operatorOpenAPI, &doc); err != nil {
		t.Fatal(err)
	}
	described := 0
	for _, methods := range doc.Paths {
		described += len(methods)
	}
	if described != len(operatorRoutes) {
		t.Fatalf("the OpenAPI file describes %d operations, the API has %d routes", described, len(operatorRoutes))
	}
	actions := map[string]bool{}
	for _, route := range operatorRoutes {
		op, ok := doc.Paths["/operator/api"+route.path][strings.ToLower(route.method)]
		if !ok || op.OperationID != route.action || actions[route.action] {
			t.Errorf("%s %s (%s) is missing from the OpenAPI file or not unique", route.method, route.path, route.action)
		}
		actions[route.action] = true
	}
}

// TestOperatorAPIOnlyForOperators: without a session every route is 401 and
// nothing is recorded; a client is refused (403) and recorded; the operator gets
// the data, and every call is recorded before it is answered.
func TestOperatorAPIOnlyForOperators(t *testing.T) {
	var routes []operatorRoute
	for _, route := range operatorRoutes {
		if route.method == "GET" && route.path != "/workspaces/members" {
			routes = append(routes, route)
		}
	}
	b := newBrowserFixture(t)
	if err := b.svc.LinkOperators([]string{operatorSubject}); err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		if s, _, _ := b.request(route.method, operatorAPIRoot+route.path, nil, ""); s != http.StatusUnauthorized {
			t.Fatalf("%s without a session: %d", route.path, s)
		}
	}
	if s, _, _ := b.request("GET", "/portal/operator/audit", nil, ""); s != http.StatusForbidden || len(b.auditRows()) != 0 {
		t.Fatalf("the audit page answered or recorded an anonymous browser: %d", s)
	}

	b.finish(b.startInvited("client-a", "client-a", "client"))
	b.invite("pending-customer", "client-b", "client")
	for _, route := range routes {
		if s, body, _ := b.request(route.method, operatorAPIRoot+route.path, nil, ""); s != http.StatusForbidden || strings.Contains(body, "client-b") {
			t.Fatalf("a client reached %s: %d", route.path, s)
		}
	}
	if s, _, _ := b.request("GET", "/portal/operator/audit", nil, ""); s != http.StatusForbidden {
		t.Fatalf("a client reached the audit page: %d", s)
	}
	if got := b.auditRows(); len(got) != len(routes)+1 || got[0] != "other invitations.list refused" || got[len(got)-1] != "other audit.view refused" {
		t.Fatalf("the client's refused calls were not each recorded: %q", got)
	}

	b.signOut()
	b.as(operatorSubject)
	b.finish(b.startAt(operatorLink))
	results := map[string]string{}
	for _, route := range routes {
		s, body, h := b.request(route.method, operatorAPIRoot+route.path, nil, "")
		if s != http.StatusOK || h.Get("Content-Type") != "application/json" || h.Get("Cache-Control") != "no-store" {
			t.Fatalf("the operator could not call %s: %d", route.path, s)
		}
		results[route.action] = body
	}
	var invitations []pendingInvitation
	if err := json.Unmarshal([]byte(results["invitations.list"]), &invitations); err != nil || len(invitations) != 1 || invitations[0].Workspace != "client-b" || invitations[0].Subject != "pending-customer" {
		t.Fatalf("pending invitations: %s", results["invitations.list"])
	}
	var workspaces []workspaceView
	if err := json.Unmarshal([]byte(results["workspaces.list"]), &workspaces); err != nil || len(workspaces) != 3 || workspaces[0].ID != "client-a" || workspaces[1].ID != "client-b" || len(workspaces[1].Members) != 0 || workspaces[2].ID != operatorWorkspace || workspaces[2].Members[0].Role != "operator" {
		t.Fatalf("workspaces: %s", results["workspaces.list"])
	}
	var audit []auditEntry
	if err := json.Unmarshal([]byte(results["audit.list"]), &audit); err != nil || len(audit) != len(routes)+3 || audit[0].Action != "workspaces.list" || audit[0].Outcome != "allowed" || audit[0].Kind != "operator" || audit[0].Subject != operatorSubject {
		t.Fatalf("the audit log does not show the operator's own calls, newest first: %s", results["audit.list"])
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, operatorOpenAPI); err != nil || strings.TrimSpace(results["openapi.read"]) != compacted.String() {
		t.Fatal("the API does not serve its own OpenAPI file")
	}
	s, body, _ := b.request("GET", "/portal/operator/audit?lang=da", nil, "")
	if s != http.StatusOK || !strings.Contains(body, "Revisionslog") || !strings.Contains(body, "operator · "+operatorSubject+" · openapi.read · allowed") {
		t.Fatalf("the audit page does not list the calls: %d", s)
	}
	if got := b.auditRows(); got[len(got)-1] != "operator audit.view allowed" {
		t.Fatalf("viewing the audit page was not recorded: %q", got)
	}
}

// TestOperatorResponseRequiresDurableAuthority invalidates the actual session
// while the membership SDK evaluation or private database read is paused.
func TestOperatorResponseRequiresDurableAuthority(t *testing.T) {
	for _, path := range []string{"/api/invitations", "/api/workspaces", "/api/audit", "/api/openapi.json", "/audit"} {
		for _, checkpoint := range []string{"membership", "data"} {
			if checkpoint == "data" && path == "/api/openapi.json" {
				continue
			}
			for _, change := range []string{"unchanged", "deleted", "expired"} {
				t.Run(path+"/"+checkpoint+"/"+change, func(t *testing.T) {
					b := newOperatorBrowserFixture(t)
					b.invite("pending-customer", "client-b", "client")
					if status, _, _ := b.request("GET", operatorAPIRoot+"/openapi.json", nil, ""); status != http.StatusOK {
						t.Fatal("seed audit call failed", status)
					}
					var blocked, release chan struct{}
					if checkpoint == "membership" {
						pause := installReadProvider(t, b, "portal-identity", 2)
						blocked, release = pause.blocked, pause.release
					} else {
						query := "FROM portal_operator_audit ORDER BY"
						switch path {
						case "/api/invitations":
							query = "FROM portal_invitations WHERE"
						case "/api/workspaces":
							query = "FROM inventory i LEFT JOIN portal_memberships"
						}
						pause := installCompletedRead(t, b, query)
						blocked, release = pause.blocked, pause.release
						b.mu.Lock()
						b.handler = browserHandler(t, b.svc, b.site.URL+"/portal/callback")
						b.mu.Unlock()
					}
					resume := onceRelease(release)
					result := make(chan contactResponse, 1)
					done := make(chan struct{})
					go func() {
						defer close(done)
						status, body, _ := b.request("GET", "/portal/operator"+path, nil, "")
						result <- contactResponse{status, body}
					}()
					t.Cleanup(func() {
						resume()
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("operator reader did not finish")
						}
					})
					select {
					case <-blocked:
					case <-time.After(5 * time.Second):
						t.Fatal("operator read missed its authority checkpoint")
					}
					query := ""
					switch change {
					case "deleted":
						query = "DELETE FROM portal_sessions"
					case "expired":
						query = "UPDATE portal_sessions SET expiry=now()-interval '1 minute'"
					}
					if query != "" {
						if _, err := b.pool.Exec(t.Context(), query); err != nil {
							t.Fatal(err)
						}
					}
					resume()
					got := <-result
					if change == "unchanged" {
						if got.status != http.StatusOK {
							t.Fatal("current operator lost access", got.status)
						}
					} else if got.status != http.StatusServiceUnavailable || strings.Contains(got.body, "pending-customer") || strings.Contains(got.body, operatorSubject) || strings.Contains(got.body, "openapi.read") {
						t.Fatalf("%s session released private operator output: status=%d", change, got.status)
					}
				})
			}
		}
	}
}

// TestOperatorAPIFailsWithoutAudit: a call that cannot be recorded is not answered.
func TestOperatorAPIFailsWithoutAudit(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	b.invite("pending-customer", "client-b", "client")
	migrator := migrationDB(t)
	if _, err := migrator.Exec(b.f.ctx, "REVOKE INSERT ON portal_operator_audit FROM portal_runtime"); err != nil {
		t.Fatal(err)
	}
	s, body, _ := b.request("GET", operatorAPIRoot+"/invitations", nil, "")
	if _, err := migrator.Exec(b.f.ctx, "GRANT INSERT ON portal_operator_audit TO portal_runtime"); err != nil {
		t.Fatal(err)
	}
	if s != http.StatusServiceUnavailable || strings.Contains(body, "client-b") {
		t.Fatalf("an unrecorded call was answered: %d %s", s, body)
	}
}

// TestOperatorListsRefuseOversizedResults prevents a bounded response from
// silently presenting an incomplete invitation or membership inventory.
func TestOperatorListsRefuseOversizedResults(t *testing.T) {
	for _, kind := range []string{"invitations", "workspaces"} {
		t.Run(kind, func(t *testing.T) {
			b := newOperatorBrowserFixture(t)
			query := `INSERT INTO portal_memberships(issuer,subject,client_id,role) SELECT $1,'large-'||n,'private-large-workspace','client' FROM generate_series(1,1001) n`
			if kind == "invitations" {
				query = `INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at) SELECT decode(md5(n::text)||md5(n::text),'hex'),$1,'large-'||n,'private-large-workspace','client',now()+interval '1 day' FROM generate_series(1,1001) n`
			}
			if _, err := b.pool.Exec(t.Context(), query, b.f.issuer); err != nil {
				t.Fatal(err)
			}
			status, body, _ := b.request("GET", operatorAPIRoot+"/"+kind, nil, "")
			if status != http.StatusServiceUnavailable || strings.Contains(body, "private-large-workspace") {
				t.Fatalf("oversized %s inventory was returned: %d", kind, status)
			}
		})
	}
}
