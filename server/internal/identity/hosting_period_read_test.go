package identity

import (
	"net/http"
	"strings"
	"testing"
)

func TestOperatorReadsOnlyTheGoverningHostingRevision(t *testing.T) {
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)
	ctx := t.Context()
	owner := migrationDB(t)
	const workspace = "private-client-key"
	const current = "CCCCCCCCCCCCCCCCCCCCCCCCCC"
	const future = "DDDDDDDDDDDDDDDDDDDDDDDDDD"
	var operator string
	if err := b.pool.QueryRow(ctx, `SELECT incarnation::text FROM portal_memberships
 WHERE subject='proposal-operator' AND role='operator'`).Scan(&operator); err != nil {
		t.Fatal(err)
	}

	if _, err := owner.Exec(ctx, `INSERT INTO portal_hosting_entitlements(request_id,client_id) VALUES($1,$2)`, proposalRequestID, workspace); err != nil {
		t.Fatal(err)
	}
	insert := func(period string, revision int, start, end, scope, state string) {
		t.Helper()
		_, err := owner.Exec(ctx, `WITH appended AS (INSERT INTO portal_hosting_period_revisions
 (request_id,client_id,period_id,revision,acceptance_id,proposal_version,content_hash,starts_at,ends_at,scope,state,operator_incarnation)
 SELECT a.request_id,a.client_id,$3,$4,a.id,a.proposal_version,a.content_hash,
        transaction_timestamp()+$5::text::interval,transaction_timestamp()+$6::text::interval,$7,$8,$9
 FROM portal_proposal_acceptances a WHERE a.request_id=$1 AND a.client_id=$2
 RETURNING request_id,client_id,period_id,revision)
 INSERT INTO portal_hosting_period_heads(request_id,client_id,period_id,revision)
 SELECT request_id,client_id,period_id,revision FROM appended
 ON CONFLICT(request_id,client_id,period_id) DO UPDATE SET revision=EXCLUDED.revision`,
			proposalRequestID, workspace, period, revision, start, end, scope, state, operator)
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(client, actor, expected, detail string) {
		t.Helper()
		var state, scope string
		err := b.pool.QueryRow(ctx, `SELECT state,coalesce(scope,'') FROM public.portal_operator_hosting_coverage($1,$2,$3,$4,$5::uuid)`,
			proposalRequestID, client, b.f.issuer, "proposal-operator", actor).Scan(&state, &scope)
		if err != nil || state != expected || scope != detail {
			t.Fatalf("hosting coverage = %q/%q, expected %q/%q, error=%v", state, scope, expected, detail, err)
		}
	}
	read(workspace, operator, "unavailable", "")
	insert(future, 1, "30 days", "60 days", "Future renewal", "agreed")
	insert(current, 1, "-30 days", "20 days", "Current scope", "agreed")
	read(workspace, operator, "active", "Current scope")
	flags := []string{"portal-identity", "portal-requests", "portal-triage", "portal-hosting-periods"}
	setFixturePortalFlags(t, b, flags, []any{true, true, true, true})
	url := "/portal/operator/requests/" + proposalRequestID
	if status, page, _ := b.request("GET", url, nil, ""); status != http.StatusForbidden || strings.Contains(page, "Current scope") {
		t.Fatalf("the client could read operator hosting coverage: %d", status)
	}
	b.signOut()
	b.as("proposal-operator")
	b.finish(b.start())
	for lang, heading := range map[string]string{"en": "Hosting period", "da": "Hostingperiode"} {
		status, page, _ := b.request("GET", url+"?lang="+lang, nil, "")
		if status != http.StatusOK || !strings.Contains(page, heading) ||
			!strings.Contains(page, "Current scope") || strings.Contains(page, "Future renewal") {
			t.Fatalf("the operator's %s page did not show the exact current coverage: %d", lang, status)
		}
	}
	setFixturePortalFlags(t, b, flags, []any{true, true, true, false})
	if status, page, _ := b.request("GET", url, nil, ""); status != http.StatusOK || strings.Contains(page, "Current scope") {
		t.Fatalf("the operator page ignored the disabled coverage release: %d", status)
	}
	read("another-workspace", operator, "unavailable", "")
	read(workspace, "00000000-0000-0000-0000-000000000000", "unavailable", "")
	insert(current, 2, "-30 days", "20 days", "Disputed scope", "disputed")
	read(workspace, operator, "disputed", "Disputed scope")
	insert(current, 3, "-30 days", "20 days", "Cancelled scope", "cancelled")
	read(workspace, operator, "cancelled", "Cancelled scope")
	insert(current, 4, "-30 days", "-1 day", "Old scope", "agreed")
	read(workspace, operator, "future", "Future renewal")
	insert(current, 5, "5 days", "15 days", "Cancelled renewal", "cancelled")
	read(workspace, operator, "future", "Future renewal")
	insert(current, 6, "5 days", "15 days", "Disputed renewal", "disputed")
	read(workspace, operator, "future", "Future renewal")
	insert(future, 2, "30 days", "60 days", "Cancelled later renewal", "cancelled")
	read(workspace, operator, "unavailable", "")
	insert(future, 3, "30 days", "60 days", "Future renewal", "agreed")
	insert(current, 7, "-30 days", "50 days", "Overlapping scope", "agreed")
	read(workspace, operator, "unavailable", "")
	insert(future, 4, "-80 days", "-40 days", "Past period", "agreed")
	insert(current, 8, "-30 days", "-1 day", "Old scope", "agreed")
	read(workspace, operator, "ended", "")

	insert(current, 9, "-20 days", "20 days", "Protected maintenance scope", "agreed")
	setFixturePortalFlags(t, b, flags, []any{true, true, true, true})
	status, page := requestAfterReadRevocation(t, b, "LATERAL public.portal_operator_hosting_coverage", url, "proposal-operator")
	if status != http.StatusForbidden || strings.Contains(page, "Protected maintenance scope") {
		t.Fatalf("hosting coverage survived operator revocation after the read: %d", status)
	}
}
