package identity

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

const operatorCalendarReviewLimit = 20

type calendarReviewEntry struct {
	appointmentHistoryItem
	Workspace            string
	AwaitingConfirmation bool
}

type operatorCalendarConflictPage struct {
	pageData
	CalendarReviewItems   []calendarReviewEntry
	CalendarReviewLimited bool
}

func (s *Foundation) calendarConflictReviewReady(ctx context.Context) bool {
	if !s.ready(ctx) {
		return false
	}
	on, err := s.flags.BooleanValue(ctx, "portal-calendar-conflicts", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && on && ctx.Err() == nil
}

func (b *browser) operatorCalendarConflicts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if !b.foundation.calendarConflictReviewReady(ctx) {
		http.NotFound(w, r)
		return
	}
	status, result := b.operatorCall(r.WithContext(ctx), "calendar.conflicts.view", func(ctx context.Context, client string) ([]byte, error) {
		authority := humanOperatorAuthority(ctx)
		if authority == nil {
			return nil, ErrDenied
		}
		current := authority.current
		authority.current = func(ctx context.Context, query privateResponseQuery) bool {
			return b.foundation.calendarConflictReviewReady(ctx) && current(ctx, query)
		}
		data := b.operatorData(r.WithContext(ctx), "operator-calendar-conflicts", client)
		items, err := b.foundation.readOperatorCalendarReview(ctx, data.Lang)
		if err != nil {
			return nil, err
		}
		page := operatorCalendarConflictPage{
			pageData:              data,
			CalendarReviewItems:   items[:min(len(items), operatorCalendarReviewLimit)],
			CalendarReviewLimited: len(items) > operatorCalendarReviewLimit,
		}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", page)
		return buffer.Bytes(), err
	})
	b.writeOperatorActionPage(w, r, status, result)
}

func (s *Foundation) readOperatorCalendarReview(ctx context.Context, lang string) ([]calendarReviewEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT COALESCE(settings.display_name,''),revision.starts_at,revision.ends_at,
 reservation.quote_request_id IS NOT NULL,observation.reservation_id IS NULL
 FROM portal_appointment_calendar_intents intent
 JOIN portal_appointment_revisions revision USING(reservation_id,revision)
 JOIN portal_appointment_reservations reservation ON reservation.id=revision.reservation_id AND reservation.client_id=revision.client_id
 LEFT JOIN portal_appointment_calendar_observations observation USING(reservation_id)
 LEFT JOIN portal_appointment_calendar_attempts attempt USING(reservation_id)
 LEFT JOIN portal_workspace_settings settings ON settings.workspace_id=revision.client_id
 WHERE observation.outcome='conflict' OR (observation.reservation_id IS NULL AND attempt.reservation_id IS NOT NULL)
 ORDER BY COALESCE(observation.observed_at,attempt.attempted_at) DESC,intent.reservation_id LIMIT $1`, operatorCalendarReviewLimit+1)
	if err != nil {
		return nil, errTriageStorage
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (calendarReviewEntry, error) {
		var name string
		var start, end time.Time
		var consultation, awaitingConfirmation bool
		if err := row.Scan(&name, &start, &end, &consultation, &awaitingConfirmation); err != nil {
			return calendarReviewEntry{}, errTriageStorage
		}
		if name == "" {
			name = "Unnamed workspace"
			if lang == "da" {
				name = "Arbejdsområde uden navn"
			}
		}
		item := appointmentHistoryLabel(start.In(copenhagen), end.In(copenhagen), consultation, false, lang)
		return calendarReviewEntry{appointmentHistoryItem: item, Workspace: name, AwaitingConfirmation: awaitingConfirmation}, nil
	})
}
