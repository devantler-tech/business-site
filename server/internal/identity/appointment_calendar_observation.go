package identity

import (
	"context"
	"errors"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/devantler-tech/business-site/server/internal/booking/caldav"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pendingIntroCalendarCreation = `
 FROM portal_appointment_calendar_intents q
 JOIN portal_appointment_revisions v USING(reservation_id,revision)
 JOIN portal_appointment_introductions i USING(reservation_id,revision,client_id)
 WHERE q.reservation_id=$1 AND v.starts_at>clock_timestamp()
 AND NOT EXISTS (SELECT 1 FROM portal_appointment_introduction_cancellations c WHERE c.reservation_id=q.reservation_id)
 AND NOT EXISTS (SELECT 1 FROM portal_appointment_calendar_observations o WHERE o.reservation_id=q.reservation_id)`

// reconcileIntroCalendarCreation processes one confirmed free introduction.
// Provider I/O runs without a database transaction. A retry observes the same
// canonical event, and the final check shares the cancellation lock. The
// intent stays available for cleanup if cancellation wins during provider I/O.
func reconcileIntroCalendarCreation(ctx context.Context, pool *pgxpool.Pool, reservation string,
	create func(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error),
) error {
	if create == nil {
		return booking.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var event caldav.OwnedEvent
	err := pool.QueryRow(ctx, `SELECT q.event_key,q.calendar_binding,q.revision,v.starts_at,v.ends_at`+pendingIntroCalendarCreation,
		reservation).Scan(&event.Key, &event.CalendarBinding, &event.Revision, &event.Slot.Start, &event.Slot.End)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return creditWriteError(err)
	}

	observation, err := create(ctx, event)
	outcome := "created"
	var tag *string
	switch {
	case errors.Is(err, caldav.ErrOwnedEventConflict):
		outcome = "conflict"
	case err != nil || !observation.Exists || observation.ETag == "":
		return booking.ErrUnavailable
	default:
		tag = &observation.ETag
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := lockAppointmentCalendarTx(ctx, tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO portal_appointment_calendar_observations(reservation_id,outcome,provider_tag)
 SELECT q.reservation_id,$2,$3`+pendingIntroCalendarCreation+` AND q.event_key=$4 AND q.revision=$5
 ON CONFLICT (reservation_id) DO NOTHING`, reservation, outcome, tag, event.Key, event.Revision)
		return err
	})
	return creditWriteError(err)
}
