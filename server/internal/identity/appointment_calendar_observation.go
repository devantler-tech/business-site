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

const calendarReconciliationLock = 1869820526

type calendarCreationCandidate struct {
	event                              caldav.OwnedEvent
	client, request, acceptance, order string
}

type appointmentCalendarProvider interface {
	calendarRemovalProvider
	CreateOwnedEvent(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error)
	ReadOwnedEvent(context.Context, caldav.OwnedEvent) (caldav.OwnedEventObservation, error)
}

// reconcileAppointmentCalendar checks the confirmed reservation before and
// after provider I/O. A cancelled introduction can use a saved creation
// observation to remove its event. Unresolved creation keeps its attempt.
func reconcileAppointmentCalendar(ctx context.Context, pool *pgxpool.Pool, reservation string,
	provider appointmentCalendarProvider,
) error {
	if provider == nil {
		return booking.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Take the per-reservation lock before any request, calendar or charge
	// lock. It serializes creation and removal through provider I/O.
	connection, release, err := holdSessionLock(ctx, pool, calendarReconciliationLock, reservation)
	if err != nil {
		return creditWriteError(err)
	}
	defer release()

	var event caldav.OwnedEvent
	operation := provider.ReadOwnedEvent
	err = pgx.BeginFunc(ctx, connection, func(tx pgx.Tx) error {
		candidate, err := lockPendingCalendarCreation(ctx, tx, reservation)
		if err != nil {
			return err
		}
		event = candidate.event
		var firstAttempt bool
		// A replay may observe the exact event but cannot issue a second PUT
		// when a prior worker could have left one in flight.
		if err := tx.QueryRow(ctx, `WITH timely AS (
 SELECT $1::public.portal_consent_key AS reservation_id WHERE clock_timestamp()<$2::timestamptz
), attempt AS (
 INSERT INTO public.portal_appointment_calendar_attempts(reservation_id)
 SELECT reservation_id FROM timely ON CONFLICT DO NOTHING RETURNING reservation_id
)
SELECT EXISTS(SELECT 1 FROM attempt) FROM timely`, reservation, event.Slot.Start).Scan(&firstAttempt); err != nil {
			return err
		}
		if firstAttempt {
			operation = provider.CreateOwnedEvent
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return reconcileIntroductionCalendarRemoval(ctx, connection, reservation, provider)
	}
	if err != nil {
		return creditWriteError(err)
	}

	observation, err := operation(ctx, event)
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

	err = pgx.BeginFunc(ctx, connection, func(tx pgx.Tx) error {
		current, err := lockPendingCalendarCreation(ctx, tx, reservation)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.event.Key != event.Key || current.event.CalendarBinding != event.CalendarBinding || current.event.Revision != event.Revision {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO portal_appointment_calendar_observations(reservation_id,outcome,provider_tag)
 SELECT $1::public.portal_consent_key,$2::text,$3::text WHERE clock_timestamp()<$4::timestamptz
 ON CONFLICT (reservation_id) DO NOTHING`, reservation, outcome, tag, current.event.Slot.Start)
		return err
	})
	return creditWriteError(err)
}

// lockPendingCalendarCreation uses the same request, calendar and charge lock
// order as paid confirmation. Resolve the immutable order before taking those
// locks, then read again under them before accepting the candidate.
func lockPendingCalendarCreation(ctx context.Context, tx pgx.Tx, reservation string) (calendarCreationCandidate, error) {
	initial, err := readPendingCalendarCreation(ctx, tx, reservation)
	if err != nil {
		return calendarCreationCandidate{}, err
	}
	if initial.request != "" {
		if err := lockProposalRequest(ctx, tx, initial.request); err != nil {
			return calendarCreationCandidate{}, err
		}
	}
	if _, err := lockAppointmentCalendarTx(ctx, tx); err != nil {
		return calendarCreationCandidate{}, err
	}
	current, err := readPendingCalendarCreation(ctx, tx, reservation)
	if err != nil {
		return calendarCreationCandidate{}, err
	}
	if current.order != initial.order {
		return calendarCreationCandidate{}, pgx.ErrNoRows
	}
	if current.order != "" {
		if err := checkCalendarCreationFunding(ctx, tx, reservation, current); err != nil {
			return calendarCreationCandidate{}, err
		}
	}
	var future bool
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()<$1::timestamptz`, current.event.Slot.Start).Scan(&future); err != nil {
		return calendarCreationCandidate{}, err
	}
	if !future {
		return calendarCreationCandidate{}, pgx.ErrNoRows
	}
	return current, nil
}

func checkCalendarCreationFunding(ctx context.Context, tx pgx.Tx, reservation string, candidate calendarCreationCandidate) error {
	order, err := readFundingOrder(ctx, tx, candidate.request, candidate.client, candidate.acceptance)
	if errors.Is(err, errProposalConflict) {
		return pgx.ErrNoRows
	}
	if err != nil {
		return err
	}
	if order.ID != candidate.order || order.appointment != (appointmentOrderSelection{Reservation: reservation, Revision: candidate.event.Revision}) ||
		order.OneTimeMinor <= 0 || order.MonthlyMinor != 0 {
		return pgx.ErrNoRows
	}
	_, err = readSettledOrderFunding(ctx, tx, order)
	if errors.Is(err, errProposalConflict) {
		return pgx.ErrNoRows
	}
	return err
}

func readPendingCalendarCreation(ctx context.Context, tx pgx.Tx, reservation string) (calendarCreationCandidate, error) {
	var item calendarCreationCandidate
	err := tx.QueryRow(ctx, `SELECT q.event_key,q.calendar_binding,q.revision,v.starts_at,v.ends_at,v.client_id,
 COALESCE(paid.request_id,''),COALESCE(paid.acceptance_id,''),COALESCE(paid.id,'')
 FROM portal_appointment_calendar_intents q JOIN portal_appointment_revisions v USING(reservation_id,revision)
 LEFT JOIN LATERAL (
  SELECT o.request_id,o.acceptance_id,o.id FROM portal_orders o
  JOIN portal_service_fulfillments f ON f.order_id=o.id AND f.client_id=o.client_id AND f.service_kind='appointment'
  JOIN portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind=f.service_kind
  WHERE (o.appointment_reservation_id,o.appointment_revision,o.client_id)=(v.reservation_id,v.revision,v.client_id)
    AND c.aggregate_id=v.reservation_id::text
 ) paid ON true
 WHERE q.reservation_id=$1 AND v.starts_at>clock_timestamp()
 AND (paid.id IS NOT NULL OR EXISTS (
   SELECT 1 FROM portal_appointment_introductions i
   WHERE (i.reservation_id,i.revision,i.client_id)=(v.reservation_id,v.revision,v.client_id)
     AND NOT EXISTS (SELECT 1 FROM portal_appointment_introduction_cancellations c WHERE c.reservation_id=i.reservation_id)
 ))
 AND NOT EXISTS (SELECT 1 FROM portal_appointment_calendar_observations o WHERE o.reservation_id=q.reservation_id)`, reservation).
		Scan(&item.event.Key, &item.event.CalendarBinding, &item.event.Revision, &item.event.Slot.Start, &item.event.Slot.End,
			&item.client, &item.request, &item.acceptance, &item.order)
	return item, err
}
