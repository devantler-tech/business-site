package identity

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

const calendarConflictPath = "/portal/operator/calendar-conflicts"

func TestOperatorCalendarConflictsReflectRecordedEvidence(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	if status, _, _ := b.request("GET", calendarConflictPath, nil, ""); status != http.StatusNotFound {
		t.Fatalf("calendar conflicts opened while disabled: %d", status)
	}
	enableCalendarConflictFixture(t, b)
	reservation := seedCalendarConflict(t, b)
	const workspace = "Northern & Digital"
	if status, home, _ := b.request("GET", "/portal/operator?lang=en", nil, ""); status != http.StatusOK ||
		!strings.Contains(home, calendarConflictPath+"?lang=en") {
		t.Fatalf("calendar conflicts are missing from operator navigation: %d", status)
	}
	for _, tc := range []struct{ lang, heading, service, state string }{
		{"en", "Calendar review", "Introductory call", "Calendar conflict"},
		{"da", "Kalenderstatus", "Indledende samtale", "Kalenderkonflikt"},
	} {
		status, body, headers := b.request("GET", calendarConflictPath+"?lang="+tc.lang, nil, "")
		body = html.UnescapeString(body)
		if status != http.StatusOK || !strings.Contains(headers.Get("Cache-Control"), "no-store") ||
			!strings.Contains(body, tc.heading) || !strings.Contains(body, tc.service) || !strings.Contains(body, tc.state) || !strings.Contains(body, workspace) ||
			strings.Contains(body, reservation) || strings.Contains(body, fixtureCalendarDestination) {
			t.Fatalf("calendar conflict details were missing, cacheable or overexposed: %d", status)
		}
	}
	if _, err := migrationDB(t).Exec(t.Context(), `UPDATE portal_appointment_calendar_observations
 SET outcome='created',provider_tag='"private-fixture-validator"' WHERE reservation_id=$1`, reservation); err != nil {
		t.Fatal(err)
	}
	if status, page, _ := b.request("GET", calendarConflictPath+"?lang=en", nil, ""); status != http.StatusOK ||
		!strings.Contains(page, "No calendar items need review.") || strings.Contains(page, "private-fixture-validator") {
		t.Fatalf("a successful observation appeared as a conflict or disclosed a validator: %d", status)
	}
	if got := b.auditRows(); len(got) != 3 || got[2] != "operator calendar.conflicts.view allowed" {
		t.Fatalf("calendar conflict views were not audited: %v", got)
	}
	agent := linkFixtureAgent(t, b, nil)
	machine := &http.Client{Transport: b.client.Transport}
	token := fixtureAgentToken(t, b, agent)
	if status, _, _ := agentRequest(t, b, machine, "GET", operatorAPIRoot+"/invitations", token); status != http.StatusOK {
		t.Fatalf("the synthetic machine could not use its ordinary read permission: %d", status)
	}
	if status, page, _ := agentRequest(t, b, machine, "GET", calendarConflictPath, token); status != http.StatusForbidden ||
		strings.Contains(html.UnescapeString(page), workspace) {
		t.Fatalf("a machine identity could open calendar conflicts: %d", status)
	}
	b.signOut()
	triageSignIn(t, b, "calendar-conflicts-customer", "calendar-conflicts-customer", "client")
	if status, page, _ := b.request("GET", calendarConflictPath, nil, ""); status != http.StatusForbidden ||
		strings.Contains(html.UnescapeString(page), workspace) {
		t.Fatalf("a customer could open calendar conflicts: %d", status)
	}
}

func TestOperatorCalendarConflictsWithholdARevokedSession(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	enableCalendarConflictFixture(t, b)
	seedCalendarConflict(t, b)
	status, body := requestAfterReadRevocation(t, b, "FROM portal_appointment_calendar_intents intent", calendarConflictPath, operatorSubject)
	if status != http.StatusServiceUnavailable || strings.Contains(html.UnescapeString(body), "Northern & Digital") {
		t.Fatalf("a revoked operator received calendar conflicts: status=%d", status)
	}
}

func TestOperatorCalendarReviewShowsAnUnconfirmedAttemptUntilObserved(t *testing.T) {
	b := newOperatorBrowserFixture(t)
	enableCalendarConflictFixture(t, b)
	reservation, workspace := confirmedCalendarIntroduction(t, b.pool)
	const name = "Harbour Workshop"
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspace_settings(workspace_id,display_name) VALUES($1,$2)`, workspace, name); err != nil {
		t.Fatal("name the synthetic workspace", err)
	}
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_appointment_calendar_attempts(reservation_id) VALUES($1)`, reservation); err != nil {
		t.Fatal("record a pending calendar write", err)
	}
	for _, tc := range []struct{ lang, status string }{
		{"en", "Awaiting calendar confirmation"},
		{"da", "Afventer kalenderbekræftelse"},
	} {
		status, page, headers := b.request("GET", calendarConflictPath+"?lang="+tc.lang, nil, "")
		body := html.UnescapeString(page)
		if status != http.StatusOK || !strings.Contains(body, tc.status) || !strings.Contains(body, name) ||
			!strings.Contains(headers.Get("Cache-Control"), "no-store") || strings.Contains(body, reservation) {
			t.Fatalf("pending calendar write was missing or overexposed in %s: %d", tc.lang, status)
		}
	}
	const providerTag = `"private-fixture-validator"`
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_appointment_calendar_observations(reservation_id,outcome,provider_tag)
 VALUES($1,'created',$2)`, reservation, providerTag); err != nil {
		t.Fatal("record successful calendar verification", err)
	}
	if status, page, _ := b.request("GET", calendarConflictPath+"?lang=en", nil, ""); status != http.StatusOK ||
		strings.Contains(page, name) || strings.Contains(page, "private-fixture-validator") {
		t.Fatalf("a confirmed event remained in review or exposed provider data: %d", status)
	}
}

func enableCalendarConflictFixture(t *testing.T, b *browserFixture) {
	t.Helper()
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-calendar-conflicts"}, []any{true, true})
}

func seedCalendarConflict(t *testing.T, b *browserFixture) string {
	t.Helper()
	reservation, workspace := confirmedCalendarIntroduction(t, b.pool)
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_workspace_settings(workspace_id,display_name)
 VALUES($1,'Northern & Digital')`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := b.pool.Exec(t.Context(), `INSERT INTO portal_appointment_calendar_observations(reservation_id,outcome)
 VALUES($1,'conflict')`, reservation); err != nil {
		t.Fatal(err)
	}
	return reservation
}
