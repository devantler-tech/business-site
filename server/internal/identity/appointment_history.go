package identity

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

const appointmentHistoryLimit = 20

type appointmentHistoryItem struct {
	Start, End, Date, StartTime, EndTime, Status string
}

func (s *Foundation) appointmentHistoryReady(ctx context.Context) bool {
	if !s.ready(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-appointment-history", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && ctx.Err() == nil
}

func (b *browser) appointmentHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !b.foundation.appointmentHistoryReady(ctx) {
		http.NotFound(w, r)
		return
	}
	client, role, ok := b.browserMember(w, r)
	if !ok {
		return
	}
	if role != "client" {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	snapshot, ok := b.captureDurableResponse(ctx, b.contactAuthority(ctx, client))
	if !ok {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}

	page := b.data(r, "appointments", "", "")
	page.Title, page.Intro = "Your appointments", "Your introductory calls with Devantler Tech."
	if page.Lang == "da" {
		page.Title, page.Intro = "Dine aftaler", "Dine indledende samtaler med Devantler Tech."
	}
	appointments, err := b.foundation.readAppointmentHistory(ctx, client, page.Lang)
	if err != nil {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	page.AppointmentsLimited = len(appointments) > appointmentHistoryLimit
	page.Appointments = appointments[:min(len(appointments), appointmentHistoryLimit)]
	var buffer bytes.Buffer
	if b.templates().ExecuteTemplate(&buffer, "portal.html", page) != nil ||
		!b.foundation.appointmentHistoryReady(ctx) ||
		!b.durableSnapshotAllowed(ctx, snapshot, client, "client", b.foundation.pool) {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buffer.Bytes())
}

func (s *Foundation) readAppointmentHistory(ctx context.Context, client, lang string) ([]appointmentHistoryItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.starts_at,v.ends_at,c.reservation_id IS NOT NULL
 FROM portal_appointment_introductions i JOIN portal_appointment_revisions v USING(reservation_id,revision,client_id)
 LEFT JOIN portal_appointment_introduction_cancellations c ON c.reservation_id=i.reservation_id
 WHERE i.client_id=$1
 ORDER BY CASE WHEN c.reservation_id IS NULL AND v.ends_at>statement_timestamp() THEN v.starts_at END ASC NULLS LAST,
 v.starts_at DESC,i.reservation_id LIMIT $2`, client, appointmentHistoryLimit+1)
	if err != nil {
		return nil, errTriageStorage
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appointmentHistoryItem, error) {
		var start, end time.Time
		var cancelled bool
		if err := row.Scan(&start, &end, &cancelled); err != nil {
			return appointmentHistoryItem{}, errTriageStorage
		}
		return appointmentHistoryLabel(start.In(copenhagen), end.In(copenhagen), cancelled, lang), nil
	})
}

func appointmentHistoryLabel(start, end time.Time, cancelled bool, lang string) appointmentHistoryItem {
	dateFormat := "2 January 2006"
	confirmedLabel, cancelledLabel := "Confirmed", "Cancelled"
	if lang == "da" {
		dateFormat = "02.01.2006"
		confirmedLabel, cancelledLabel = "Bekræftet", "Aflyst"
	}
	item := appointmentHistoryItem{
		Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
		Date: start.Format(dateFormat), StartTime: start.Format("15:04"), EndTime: end.Format("15:04 MST"),
		Status: confirmedLabel,
	}
	if cancelled {
		item.Status = cancelledLabel
	}
	if start.Format("MST") != end.Format("MST") {
		item.StartTime = start.Format("15:04 MST")
	}
	if start.YearDay() != end.YearDay() || start.Year() != end.Year() {
		item.EndTime = end.Format(dateFormat + ", 15:04 MST")
	}
	return item
}
