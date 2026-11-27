package identity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAppointmentCancellationUsesTheClientsSavedIntroduction(t *testing.T) {
	b := newBrowserFixture(t)
	const first, second, operator = "intro-cancellation-client", "other-intro-client", "intro-cancellation-operator"
	b.invite(first, first, "client")
	b.invite(second, second, "client")
	b.invite(operator, first, "operator")
	seedAppointmentWorkspaces(t, b.pool, first, second)
	start, policy := appointmentHoldFixture(t)
	addHistoryIntroduction(t, b, policy, first, start, false)
	addHistoryIntroduction(t, b, policy, second, start.Add(2*time.Hour), false)

	flags := []string{"portal-identity", "portal-appointment-history", "portal-appointment-cancellation"}
	setFixturePortalFlags(t, b, flags, []any{true, true, false})
	b.as(first)
	b.finish(b.start())
	path := "/portal/appointments?lang=da"
	if status, body, _ := b.request("GET", path, nil, ""); status != http.StatusOK || strings.Contains(body, introductionCancellationPath) {
		t.Fatalf("the disabled action appeared in history: status=%d", status)
	}
	if status, _, _ := b.request("POST", introductionCancellationPath, url.Values{}, b.site.URL); status != http.StatusNotFound {
		t.Fatalf("the cancellation gate did not refuse the route: status=%d", status)
	}

	setFixturePortalFlags(t, b, flags, []any{true, true, true})
	status, body, _ := b.request("GET", path, nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Bekræft aflysning") || !strings.Contains(body, "Aflys senest") {
		t.Fatalf("the booked introduction lacks a Danish confirmation and deadline: status=%d", status)
	}
	form := workNativeForm(t, body, "appointments/cancel")
	reservation := form.Get("reservation")
	if reservation == "" || form.Get("revision") != "1" || form.Get("lang") != "da" {
		t.Fatal("the generated form did not bind the appointment and its original revision")
	}
	forged := url.Values{"reservation": {reservation}, "revision": {"1"}}
	if status, _, _ := b.request("POST", introductionCancellationPath, forged, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("a cancellation without CSRF passed: status=%d", status)
	}
	for range 2 {
		status, _, headers := b.request("POST", introductionCancellationPath, form, b.site.URL)
		if status != http.StatusSeeOther || headers.Get("Location") != path {
			t.Fatalf("cancellation did not return to Danish appointment history: status=%d", status)
		}
	}
	status, body, _ = b.request("GET", path, nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Aflyst") || strings.Contains(body, introductionCancellationPath) {
		t.Fatalf("history did not retain the cancelled introduction: status=%d", status)
	}

	b.signOut()
	b.as(second)
	b.finish(b.start())
	_, body, _ = b.request("GET", "/portal/appointments", nil, "")
	if !strings.Contains(body, "Confirm cancellation") || !strings.Contains(body, "Cancel by") {
		t.Fatal("English appointment history lacks the confirmation and deadline")
	}
	other := workNativeForm(t, body, "appointments/cancel")
	other.Set("reservation", reservation)
	if status, _, _ := b.request("POST", introductionCancellationPath, other, b.site.URL); status != http.StatusConflict {
		t.Fatalf("a foreign appointment was not refused: status=%d", status)
	}

	b.signOut()
	b.as(operator)
	b.finish(b.start())
	other.Set("csrf_token", b.csrf("/portal/operator"))
	if status, _, _ := b.request("POST", introductionCancellationPath, other, b.site.URL); status != http.StatusForbidden {
		t.Fatalf("the operator used the customer cancellation route: status=%d", status)
	}
	var count int
	if err := b.pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_appointment_introduction_cancellations`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("valid and rejected requests left %d cancellation records: %v", count, err)
	}
}

func TestAppointmentCancellationRefusesStaleRevisionAndElapsedDeadline(t *testing.T) {
	b, form := bookedCancellationFixture(t)
	form.Set("revision", "2")
	status, body, _ := b.request("POST", introductionCancellationPath, form, b.site.URL)
	if status != http.StatusConflict || !strings.Contains(body, `href="/portal/appointments?lang=da"`) {
		t.Fatalf("a stale revision did not point back to localized history: status=%d", status)
	}
	form.Set("revision", "1")
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `UPDATE portal_appointment_introductions
 SET cancellable_until=clock_timestamp()-interval '1 second' WHERE reservation_id=$1`, form.Get("reservation")); err != nil {
		t.Fatal(err)
	}
	status, body, _ = b.request("GET", "/portal/appointments", nil, "")
	if status != http.StatusOK || strings.Contains(body, introductionCancellationPath) {
		t.Fatalf("history still offered cancellation after its deadline: status=%d", status)
	}
	if status, _, _ = b.request("POST", introductionCancellationPath, form, b.site.URL); status != http.StatusConflict {
		t.Fatalf("a previously rendered form cancelled after the deadline: status=%d", status)
	}
	assertNoIntroductionCancellation(t, b)
}

func TestAppointmentCancellationRefusesSessionDeletedWhileWaiting(t *testing.T) {
	b, form := bookedCancellationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	migrator := migrationDB(t)
	blocker, err := migrator.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackProposalWrite(ctx, blocker)
	if _, err = lockAppointmentCalendarTx(ctx, blocker); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, "POST", b.site.URL+introductionCancellationPath, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", b.site.URL)
	response := make(chan int, 1)
	go func() {
		res, err := b.client.Do(request)
		if err != nil {
			response <- 0
			return
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		response <- res.StatusCode
	}()
	waitForFixtureDatabaseLock(ctx, t, b.pool, `SELECT EXISTS(SELECT 1 FROM pg_locks
 WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())
 AND objsubid=1 AND objid=($1::bigint & 4294967295)::oid AND NOT granted)`, appointmentCalendarLock)
	if _, err = migrator.Exec(ctx, `DELETE FROM portal_sessions`); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if status := <-response; status != http.StatusForbidden {
		t.Fatalf("a session deleted before the database lock cleared got status=%d", status)
	}
	assertNoIntroductionCancellation(t, b)
}

func TestAppointmentCancellationNeverCancelsPaidConsultations(t *testing.T) {
	b, reservation, _ := confirmedCalendarConsultation(t)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-appointment-history", "portal-appointment-cancellation"}, []any{true, true, true})
	status, body, _ := b.request("GET", "/portal/appointments", nil, "")
	if status != http.StatusOK || !strings.Contains(body, "Booking recorded") || strings.Contains(body, introductionCancellationPath) {
		t.Fatalf("the paid consultation exposed the introduction action: status=%d", status)
	}
	form := url.Values{"csrf_token": {b.csrf("/portal/")}, "reservation": {reservation}, "revision": {"1"}}
	if status, _, _ := b.request("POST", introductionCancellationPath, form, b.site.URL); status != http.StatusConflict {
		t.Fatalf("the introduction action accepted a paid consultation: status=%d", status)
	}
	assertNoIntroductionCancellation(t, b)
}

func bookedCancellationFixture(t *testing.T) (*browserFixture, url.Values) {
	t.Helper()
	b, _ := appointmentHistoryFixture(t, "booked-intro-client", true)
	status, body, _ := b.request("GET", "/portal/appointments", nil, "")
	if status != http.StatusOK {
		t.Fatalf("the fictional client's appointment history got status=%d", status)
	}
	return b, workNativeForm(t, body, "appointments/cancel")
}

func assertNoIntroductionCancellation(t *testing.T, b *browserFixture) {
	t.Helper()
	var stored bool
	if err := b.pool.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM portal_appointment_introduction_cancellations)`).Scan(&stored); err != nil || stored {
		t.Fatalf("a rejected request left a cancellation: stored=%t, err=%v", stored, err)
	}
}
