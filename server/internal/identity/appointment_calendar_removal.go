package identity

import (
	"context"
	"errors"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/devantler-tech/business-site/server/internal/booking/caldav"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type calendarRemovalProvider interface {
	ReadOwnedEvent(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error)
	DeleteOwnedEvent(context.Context, caldav.OwnedEvent, string) error
}

const cancelledIntroductionCalendar = `
 FROM portal_appointment_introduction_cancellations cancellation
 JOIN portal_appointment_introductions introduction USING(reservation_id)
 JOIN portal_appointment_calendar_intents intent USING(reservation_id,revision)
 JOIN portal_appointment_revisions slot USING(reservation_id,revision)`

const pendingIntroductionCalendarRemoval = cancelledIntroductionCalendar + `
 JOIN portal_appointment_calendar_observations observation USING(reservation_id)
 WHERE intent.reservation_id=$1 AND observation.outcome='created'
 AND NOT EXISTS (SELECT 1 FROM portal_appointment_calendar_removals removal
   WHERE removal.reservation_id=intent.reservation_id)`

const unconfirmedCancelledCalendarCreation = cancelledIntroductionCalendar + `
 JOIN portal_appointment_calendar_attempts attempt USING(reservation_id)
 WHERE intent.reservation_id=$1
 AND NOT EXISTS (SELECT 1 FROM portal_appointment_calendar_observations observation
   WHERE observation.reservation_id=intent.reservation_id)`

// The caller holds the reservation lock. Read back an unconfirmed creation
// before the existing removal path looks for a saved validator.
func reconcileIntroductionCalendarRemoval(ctx context.Context, connection *pgxpool.Conn, reservation string, provider calendarRemovalProvider) error {
	if err := observeCancelledCalendarCreation(ctx, connection, reservation, provider); err != nil {
		return err
	}
	var event caldav.OwnedEvent
	var tag string
	err := connection.QueryRow(ctx, `SELECT intent.event_key,intent.calendar_binding,intent.revision,
 slot.starts_at,slot.ends_at,observation.provider_tag`+pendingIntroductionCalendarRemoval, reservation).
		Scan(&event.Key, &event.CalendarBinding, &event.Revision, &event.Slot.Start, &event.Slot.End, &tag)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return creditWriteError(err)
	}

	outcome := "removed"
	err = provider.DeleteOwnedEvent(ctx, event, tag)
	switch {
	case errors.Is(err, caldav.ErrOwnedEventConflict):
		outcome = "conflict"
	case err != nil:
		return booking.ErrUnavailable
	}

	// Keep the provider receipt only if its original evidence survived the
	// call. Account retention can erase these facts while the provider waits.
	_, err = connection.Exec(ctx, `INSERT INTO portal_appointment_calendar_removals(reservation_id,outcome)
 SELECT intent.reservation_id,$2::text`+pendingIntroductionCalendarRemoval+`
 AND (intent.event_key,intent.calendar_binding,intent.revision,observation.provider_tag)=($3,$4,$5,$6)
 ON CONFLICT (reservation_id) DO NOTHING`, reservation, outcome, event.Key, event.CalendarBinding, event.Revision, tag)
	return creditWriteError(err)
}

func observeCancelledCalendarCreation(ctx context.Context, connection *pgxpool.Conn, reservation string, provider calendarRemovalProvider) error {
	var event caldav.OwnedEvent
	err := connection.QueryRow(ctx, `SELECT intent.event_key,intent.calendar_binding,intent.revision,
 slot.starts_at,slot.ends_at`+unconfirmedCancelledCalendarCreation, reservation).
		Scan(&event.Key, &event.CalendarBinding, &event.Revision, &event.Slot.Start, &event.Slot.End)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return creditWriteError(err)
	}

	observation, err := provider.ReadOwnedEvent(ctx, event)
	outcome := "created"
	var tag *string
	switch {
	case errors.Is(err, caldav.ErrOwnedEventConflict):
		outcome = "conflict"
	case err != nil:
		return booking.ErrUnavailable
	case !observation.Exists:
		// A delayed create may still become visible.
		return nil
	case observation.ETag == "":
		return booking.ErrUnavailable
	default:
		tag = &observation.ETag
	}

	_, err = connection.Exec(ctx, `INSERT INTO portal_appointment_calendar_observations(reservation_id,outcome,provider_tag)
 SELECT intent.reservation_id,$2::text,$3::text`+unconfirmedCancelledCalendarCreation+`
 AND (intent.event_key,intent.calendar_binding,intent.revision)=($4,$5,$6)
 ON CONFLICT (reservation_id) DO NOTHING`, reservation, outcome, tag, event.Key, event.CalendarBinding, event.Revision)
	return creditWriteError(err)
}
