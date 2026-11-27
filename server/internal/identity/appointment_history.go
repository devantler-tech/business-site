package identity

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
	"github.com/open-feature/go-sdk/openfeature"
)

const appointmentHistoryLimit = 20

type appointmentHistoryItem struct {
	Service, Start, End, Date, StartTime, EndTime, Status string
	Cancellation                                          *appointmentCancellation
}

type appointmentCancellation struct {
	Reservation, Deadline, DeadlineText string
	Revision                            int
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
	client, ok := b.appointmentClient(w, r)
	if !ok {
		return
	}
	snapshot, ok := b.captureDurableResponse(ctx, b.contactAuthority(ctx, client))
	if !ok {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}

	page := b.data(r, "appointments", "", nosurf.Token(r))
	page.ClientTasks = b.clientTasks(ctx, "appointments")
	page.Title, page.Intro = "Your appointments", "Your introductory calls and consultations with Devantler Tech."
	if page.Lang == "da" {
		page.Title, page.Intro = "Dine aftaler", "Dine indledende samtaler og rådgivning med Devantler Tech."
	}
	allowCancellation := b.foundation.appointmentCancellationReady(ctx)
	appointments, err := b.foundation.readAppointmentHistory(ctx, client, page.Lang, allowCancellation)
	if err != nil {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	page.AppointmentsLimited = len(appointments) > appointmentHistoryLimit
	page.Appointments = appointments[:min(len(appointments), appointmentHistoryLimit)]
	var buffer bytes.Buffer
	if b.templates().ExecuteTemplate(&buffer, "portal.html", page) != nil ||
		!b.foundation.appointmentHistoryReady(ctx) ||
		(allowCancellation && !b.foundation.appointmentCancellationReady(ctx)) ||
		!b.durableSnapshotAllowed(ctx, snapshot, client, "client", b.foundation.pool) {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buffer.Bytes())
}

func (b *browser) appointmentClient(w http.ResponseWriter, r *http.Request) (string, bool) {
	client, role, ok := b.browserMember(w, r)
	if !ok {
		return "", false
	}
	if role != "client" {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return "", false
	}
	return client, true
}

func (s *Foundation) readAppointmentHistory(ctx context.Context, client, lang string, allowCancellation bool) ([]appointmentHistoryItem, error) {
	rows, err := s.pool.Query(ctx, `WITH history AS (
 SELECT v.starts_at,v.ends_at,i.reservation_id,false AS consultation,c.reservation_id IS NOT NULL AS cancelled,i.revision,i.cancellable_until
 FROM portal_appointment_introductions i JOIN portal_appointment_revisions v USING(reservation_id,revision,client_id)
 LEFT JOIN portal_appointment_introduction_cancellations c ON c.reservation_id=i.reservation_id
 WHERE i.client_id=$1
 UNION ALL
 SELECT v.starts_at,v.ends_at,v.reservation_id,true,false,NULL::integer,NULL::timestamptz
 FROM portal_orders o
 JOIN portal_service_fulfillments f ON f.order_id=o.id AND f.client_id=o.client_id AND f.service_kind='appointment'
 JOIN portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind=f.service_kind
   AND c.aggregate_id=o.appointment_reservation_id::text
 JOIN portal_appointment_revisions v ON v.reservation_id=o.appointment_reservation_id
   AND v.revision=o.appointment_revision AND v.client_id=o.client_id
 WHERE o.client_id=$1
 )
 SELECT starts_at,ends_at,consultation,cancelled,reservation_id,revision,
 CASE WHEN $3::boolean AND NOT cancelled AND statement_timestamp()<=cancellable_until THEN cancellable_until END
 FROM history
 ORDER BY CASE WHEN NOT cancelled AND ends_at>statement_timestamp() THEN starts_at END ASC NULLS LAST,
 starts_at DESC,reservation_id LIMIT $2`, client, appointmentHistoryLimit+1, allowCancellation)
	if err != nil {
		return nil, errTriageStorage
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (appointmentHistoryItem, error) {
		var start, end time.Time
		var consultation, cancelled bool
		var reservation string
		var revision *int
		var deadline *time.Time
		if err := row.Scan(&start, &end, &consultation, &cancelled, &reservation, &revision, &deadline); err != nil {
			return appointmentHistoryItem{}, errTriageStorage
		}
		item := appointmentHistoryLabel(start.In(copenhagen), end.In(copenhagen), consultation, cancelled, lang)
		if deadline != nil && revision != nil {
			format := "2 January 2006, 15:04 MST"
			if lang == "da" {
				format = "02.01.2006 kl. 15:04 MST"
			}
			local := deadline.In(copenhagen)
			item.Cancellation = &appointmentCancellation{
				Reservation: reservation, Revision: *revision, Deadline: local.Format(time.RFC3339), DeadlineText: local.Format(format),
			}
		}
		return item, nil
	})
}

func appointmentHistoryLabel(start, end time.Time, consultation, cancelled bool, lang string) appointmentHistoryItem {
	dateFormat := "2 January 2006"
	service, status, cancelledLabel := "Introductory call", "Confirmed", "Cancelled"
	if lang == "da" {
		dateFormat = "02.01.2006"
		service, status, cancelledLabel = "Indledende samtale", "Bekræftet", "Aflyst"
	}
	if consultation {
		service, status = "Consultation", "Booking recorded"
		if lang == "da" {
			service, status = "Rådgivning", "Booking registreret"
		}
	}
	if cancelled {
		status = cancelledLabel
	}
	item := appointmentHistoryItem{
		Service: service, Status: status,
		Start: start.Format(time.RFC3339), End: end.Format(time.RFC3339),
		Date: start.Format(dateFormat), StartTime: start.Format("15:04"), EndTime: end.Format("15:04 MST"),
	}
	if start.Format("MST") != end.Format("MST") {
		item.StartTime = start.Format("15:04 MST")
	}
	if start.YearDay() != end.YearDay() || start.Year() != end.Year() {
		item.EndTime = end.Format(dateFormat + ", 15:04 MST")
	}
	return item
}
