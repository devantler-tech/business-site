package identity

import (
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
)

func TestAppointmentHistoryListsOnlyCurrentWorkspace(t *testing.T) {
	b := newBrowserFixture(t)
	const first, second = "calendar-client-one", "calendar-client-two"
	b.invite(first, first, "client")
	b.invite(second, second, "client")
	b.as(first)
	b.finish(b.start())
	const path = "/portal/appointments"
	if status, _, _ := b.request("GET", path, nil, ""); status != http.StatusNotFound {
		t.Fatalf("appointment history opened with its switch off: status=%d", status)
	}

	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-appointment-history"}, []any{true, true})
	seedAppointmentWorkspaces(t, b.pool, first, second)
	start, policy := appointmentHoldFixture(t)
	addHistoryIntroduction(t, b, policy, first, start, true)
	next := start.Add(time.Hour)
	addHistoryIntroduction(t, b, policy, first, next, false)
	unrelated := start.Add(2 * time.Hour)
	addHistoryIntroduction(t, b, policy, second, unrelated, false)
	status, home, _ := b.request("GET", "/portal/?lang=en", nil, "")
	if status != http.StatusOK || !strings.Contains(home, path+"?lang=en") {
		t.Fatalf("the signed-in workspace does not link to appointment history: status=%d", status)
	}

	status, body, headers := b.request("GET", path+"?lang=en&workspace="+second, nil, "")
	if status != http.StatusOK || !strings.Contains(headers.Get("Cache-Control"), "no-store") {
		t.Fatalf("appointment history was unavailable or cacheable: status=%d", status)
	}
	body = html.UnescapeString(body)
	upcoming, cancelled := strings.Index(body, next.Format(time.RFC3339)), strings.Index(body, start.Format(time.RFC3339))
	if upcoming < 0 || cancelled < upcoming || !strings.Contains(body, "Confirmed") || !strings.Contains(body, "Cancelled") ||
		strings.Contains(body, unrelated.Format(time.RFC3339)) || strings.Contains(body, second) {
		t.Fatal("history lost its upcoming and cancelled entries or crossed a workspace boundary")
	}

	b.signOut()
	b.as(second)
	b.finish(b.start())
	status, body, _ = b.request("GET", path+"?lang=da", nil, "")
	body = html.UnescapeString(body)
	if status != http.StatusOK || !strings.Contains(body, "Dine aftaler") || !strings.Contains(body, "Bekræftet") ||
		!strings.Contains(body, unrelated.Format(time.RFC3339)) || strings.Contains(body, next.Format(time.RFC3339)) {
		t.Fatal("the second workspace did not receive only its own localized appointment")
	}

	b.signOut()
	if status, _, _ := b.request("GET", path, nil, ""); status != http.StatusUnauthorized {
		t.Fatalf("signed-out appointment history status=%d", status)
	}
	const operator = "calendar-operator"
	b.invite(operator, first, "operator")
	b.as(operator)
	b.finish(b.start())
	if status, _, _ := b.request("GET", path+"?workspace="+first, nil, ""); status != http.StatusForbidden {
		t.Fatalf("operator reached a workspace's client appointment history: status=%d", status)
	}
}

func TestAppointmentHistoryRefusesARevokedSessionAfterReading(t *testing.T) {
	b := newBrowserFixture(t)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-appointment-history"}, []any{true, true})
	const subject = "appointment-history-reader"
	b.invite(subject, subject, "client")
	b.as(subject)
	b.finish(b.start())
	seedAppointmentWorkspaces(t, b.pool, subject)
	start, policy := appointmentHoldFixture(t)
	addHistoryIntroduction(t, b, policy, subject, start, false)

	status, body := requestAfterReadRevocation(t, b, "FROM portal_appointment_introductions i JOIN", "/portal/appointments", subject)
	if status != http.StatusServiceUnavailable || strings.Contains(html.UnescapeString(body), start.Format(time.RFC3339)) {
		t.Fatalf("a revoked session received private appointment history: status=%d", status)
	}
}

func TestAppointmentHistoryMakesTheAutumnClockChangeUnambiguous(t *testing.T) {
	start := time.Date(2026, 10, 25, 0, 45, 0, 0, time.UTC).In(copenhagen)
	end := start.Add(30 * time.Minute)
	item := appointmentHistoryLabel(start, end, false, false, "da")
	if item.Date != "25.10.2026" || item.StartTime != "02:45 CEST" || item.EndTime != "02:15 CET" ||
		item.Start != "2026-10-25T02:45:00+02:00" || item.End != "2026-10-25T02:15:00+01:00" {
		t.Fatalf("the fall clock change is ambiguous: %+v", item)
	}
}

func addHistoryIntroduction(t *testing.T, b *browserFixture, policy appointmentHoldPolicy, client string, start time.Time, cancel bool) {
	t.Helper()
	ctx := t.Context()
	reservation := secret()
	slot := booking.Interval{Start: start, End: start.Add(introductionDuration)}
	err := pgx.BeginFunc(ctx, b.pool, func(tx pgx.Tx) error {
		if _, err := policy.append(ctx, tx, reservation, client, 0, slot, appointmentQuoteRef{}); err != nil {
			return err
		}
		if _, err := policy.confirmIntroduction(ctx, tx, reservation, client, 1); err != nil {
			return err
		}
		if cancel {
			_, err := cancelIntroduction(ctx, tx, reservation, client, 1)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal("create a fictional history appointment", err)
	}
}
