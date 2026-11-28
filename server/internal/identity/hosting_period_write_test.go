package identity

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"fmt"
)

func TestOperatorRecordsPeriodFromAcceptedHostingTerms(t *testing.T) {
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)
	flags := []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance", "portal-hosting-periods"}
	setFixturePortalFlags(t, b, flags, []any{true, true, true, true, true, true, true})
	b.signOut()
	b.as("proposal-operator")
	b.finish(b.start())

	detail := "/portal/operator/requests/" + proposalRequestID
	target := detail + "/hosting-periods"
	status, page, _ := b.request(http.MethodGet, detail+"?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(page, "Record hosting period") {
		t.Fatalf("operator hosting form unavailable: %d", status)
	}
	field := func(name string) string {
		t.Helper()
		match := regexp.MustCompile(`name="` + name + `" value="([^"]+)"`).FindStringSubmatch(page)
		if match == nil {
			t.Fatalf("missing form field %s", name)
		}
		return html.UnescapeString(match[1])
	}
	var acceptance string
	if err := b.pool.QueryRow(t.Context(), "SELECT id FROM portal_proposal_acceptances WHERE request_id=$1", proposalRequestID).Scan(&acceptance); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Minute).Add(123456 * time.Microsecond)
	initialStart, initialEnd := now.Add(-time.Hour), now.Add(time.Hour)
	const clockFormat = "15:04:05.999999"
	form := url.Values{
		"csrf_token": {field("csrf_token")}, "period_id": {field("period_id")}, "acceptance_id": {acceptance},
		"start_date": {initialStart.Format(time.DateOnly)}, "start_time": {initialStart.Format(clockFormat)},
		"end_date": {initialEnd.Format(time.DateOnly)}, "end_time": {initialEnd.Format(clockFormat)},
		"confirm": {"yes"}, "lang": {"en"},
	}
	setEnd := func(end time.Time) {
		form.Set("end_date", end.Format(time.DateOnly))
		form.Set("end_time", end.Format(clockFormat))
	}
	post := func(want int) {
		t.Helper()
		if got, _, _ := b.request(http.MethodPost, target, form, b.site.URL); got != want {
			t.Fatalf("record period returned %d, want %d", got, want)
		}
	}
	form.Set("acceptance_id", secret())
	post(http.StatusConflict)
	form.Set("acceptance_id", acceptance)
	form.Set("confirm", "no")
	post(http.StatusBadRequest)
	form.Set("confirm", "yes")
	form.Set("start_time", initialStart.Format(clockFormat)+"9")
	post(http.StatusBadRequest)
	form.Set("start_time", initialStart.Format(clockFormat))
	post(http.StatusSeeOther)
	post(http.StatusSeeOther)
	setEnd(now.Add(2 * time.Hour))
	post(http.StatusConflict)
	setEnd(initialEnd)
	form.Set("period_id", secret())
	post(http.StatusConflict)
	form.Set("start_date", form.Get("end_date"))
	form.Set("start_time", form.Get("end_time"))
	setEnd(now.Add(2 * time.Hour))
	post(http.StatusSeeOther)

	owner := migrationDB(t)
	var exact, total int
	if err := owner.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE scope='Hosting and routine maintenance' AND acceptance_id=$1), count(*) FROM portal_hosting_period_revisions`, acceptance).Scan(&exact, &total); err != nil || exact != 2 || total != 2 {
		t.Fatalf("expected exactly two periods with accepted terms, got %d/%d: %v", exact, total, err)
	}
	var savedStart, savedEnd time.Time
	if err := owner.QueryRow(t.Context(), `SELECT starts_at,ends_at FROM portal_hosting_period_revisions
 WHERE request_id=$1 ORDER BY starts_at LIMIT 1`, proposalRequestID).Scan(&savedStart, &savedEnd); err != nil || !savedStart.Equal(initialStart) || !savedEnd.Equal(initialEnd) {
		t.Fatalf("agreed microseconds were not preserved: %s to %s, error=%v", savedStart, savedEnd, err)
	}
	if _, err := b.pool.Exec(t.Context(), "SELECT 1 FROM portal_hosting_period_revisions"); err == nil {
		t.Fatal("runtime can read the raw hosting ledger")
	}
	if status, page, _ := b.request(http.MethodGet, detail+"?lang=da", nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "Registrér hostingperiode") || !strings.Contains(page, "Hosting and routine maintenance") ||
		!strings.Contains(page, initialStart.Format("2006-01-02 "+clockFormat)) {
		t.Fatalf("saved coverage or the Danish operator form was unavailable: %d", status)
	}
	setFixturePortalFlags(t, b, flags, []any{true, true, true, true, true, true, false})
	post(http.StatusNotFound)
}

func TestConcurrentHostingPeriodWritersRecheckOverlapAfterWaiting(t *testing.T) {
	b := acceptedServiceQuoteFixture(t, proposalServiceSoftware)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	const workspace = "private-client-key"
	var acceptance, incarnation string
	if err := b.pool.QueryRow(ctx, `SELECT a.id,m.incarnation::text FROM portal_proposal_acceptances a
 CROSS JOIN portal_memberships m WHERE a.request_id=$1 AND m.subject='proposal-operator'`, proposalRequestID).Scan(&acceptance, &incarnation); err != nil {
		t.Fatal(err)
	}
	begin := time.Now().UTC().Truncate(time.Microsecond)
	end := begin.Add(time.Hour)
	record := func(db queryRower, actor, period string, startsAt, endsAt time.Time) (string, error) {
		var result string
		err := db.QueryRow(ctx, `SELECT public.portal_record_hosting_period($1,$2,'proposal-operator',$3::uuid,$4,$5,$6,$7)`,
			proposalRequestID, b.f.issuer, actor, period, acceptance, startsAt, endsAt).Scan(&result)
		return result, err
	}
	first := beginFixtureTransaction(ctx, t, b.pool)
	if result, err := record(first, incarnation, secret(), begin, end); err != nil || result != "recorded" {
		t.Fatalf("first writer returned %q: %v", result, err)
	}
	second := make(chan error, 1)
	go func() {
		result, err := record(b.pool, incarnation, secret(), end.Add(-time.Microsecond), end.Add(time.Hour))
		if err == nil && result != "conflict" {
			err = fmt.Errorf("second writer returned %q", result)
		}
		second <- err
	}()
	waitForFixtureDatabaseLock(ctx, t, b.pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
   AND classid=$1::oid AND objid=hashtext($2)::oid AND objsubid=2 AND NOT granted)`, workspaceLock, workspace)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("second writer did not reject the one-microsecond overlap: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("second writer never returned after the first committed")
	}
	if result, err := record(b.pool, "00000000-0000-0000-0000-000000000000", secret(), end, end.Add(time.Hour)); err != nil || result != "denied" {
		t.Fatalf("stale operator membership returned %q: %v", result, err)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1) ON CONFLICT DO NOTHING`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(ctx, `INSERT INTO portal_workspace_settings(workspace_id,closed_at) VALUES($1,clock_timestamp())
 ON CONFLICT (workspace_id) DO UPDATE SET closed_at=EXCLUDED.closed_at`, workspace); err != nil {
		t.Fatal(err)
	}
	if result, err := record(b.pool, incarnation, secret(), end, end.Add(time.Hour)); err != nil || result != "missing" {
		t.Fatalf("closed workspace returned %q: %v", result, err)
	}
}
