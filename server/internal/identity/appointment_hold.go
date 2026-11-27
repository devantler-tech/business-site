package identity

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/devantler-tech/business-site/server/internal/booking"
	"github.com/jackc/pgx/v5"
)

// There is one business calendar. Its reservation writers share this lock.
const appointmentCalendarLock = int64(0x61707074686f6c64)

const appointmentHoldLifetime = 10 * time.Minute

type appointmentHold struct {
	Revision              int
	Slot                  booking.Interval
	HeldUntil             time.Time
	Quote                 appointmentQuoteRef
	IntroductionConfirmed bool
	IntroductionCancelled bool
	PaidConfirmed         bool
	cancellableUntil      *time.Time
	recordedAt            time.Time
	orderPlaced           bool
}

// appointmentQuoteRef identifies the immutable quote selected for a hold. A
// zero value leaves the hold unquoted. This reference grants no payment,
// acceptance, or confirmation authority.
type appointmentQuoteRef struct {
	Request string
	Version int
	Hash    string
}

// appointmentHoldPolicy uses the server's booking policy and calendar. The
// caller supplies the authorized client, reservation and meeting duration.
type appointmentHoldPolicy struct {
	policy          booking.Policy
	provider        booking.BusyProvider
	calendarBinding string
}

// append compares and appends a provisional hold while serializing the
// availability read and write through the caller's transaction.
func (holds appointmentHoldPolicy) append(ctx context.Context, tx pgx.Tx, reservation, client string, expected int, slot booking.Interval, quote appointmentQuoteRef) (appointmentHold, error) {
	duration := slot.End.Sub(slot.Start)
	if !opaqueRequestID.MatchString(reservation) || client == "" || expected < 0 || expected >= math.MaxInt32 ||
		duration <= 0 || duration > 24*time.Hour || duration%time.Minute != 0 || slot.Start.Nanosecond()%int(time.Microsecond) != 0 {
		return appointmentHold{}, errProposalConflict
	}
	now, err := lockAppointmentCalendarTx(ctx, tx)
	if err != nil {
		return appointmentHold{}, err
	}
	prior, err := readLatestAppointmentHold(ctx, tx, reservation, client)
	if err != nil {
		return appointmentHold{}, err
	}
	if prior.IntroductionConfirmed || prior.PaidConfirmed || (prior.Revision > 0 && prior.Quote != quote) {
		return appointmentHold{}, errProposalConflict
	}
	if prior.Revision == expected+1 && prior.HeldUntil.After(now) && prior.Slot.Start.Equal(slot.Start) && prior.Slot.End.Equal(slot.End) &&
		prior.HeldUntil.Sub(prior.recordedAt) == appointmentHoldLifetime {
		return prior, nil
	}
	if prior.Revision != expected || prior.orderPlaced || (prior.Revision > 0 && !prior.HeldUntil.After(now)) {
		return appointmentHold{}, errProposalConflict
	}
	if now.Add(appointmentHoldLifetime).After(slot.Start) {
		return appointmentHold{}, errProposalConflict
	}
	if quote != (appointmentQuoteRef{}) {
		var minutes int
		err := tx.QueryRow(ctx, `SELECT duration_minutes FROM portal_proposals
 WHERE request_id=$1 AND client_id=$2 AND version=$3 AND content_sha256=$4
   AND content_format=2 AND service_kind='appointment'`, quote.Request, client, quote.Version, quote.Hash).Scan(&minutes)
		if errors.Is(err, pgx.ErrNoRows) {
			return appointmentHold{}, errProposalConflict
		}
		if err != nil {
			return appointmentHold{}, errTriageStorage
		}
		if minutes <= 0 || minutes > 1440 || time.Duration(minutes)*time.Minute != duration {
			return appointmentHold{}, errProposalConflict
		}
	}
	now, err = holds.checkSlot(ctx, tx, reservation, slot, now)
	if err != nil {
		return appointmentHold{}, err
	}
	until := now.Add(appointmentHoldLifetime)
	if until.After(slot.Start) || (prior.Revision > 0 && !prior.HeldUntil.After(now)) {
		return appointmentHold{}, errProposalConflict
	}
	if expected == 0 {
		tag, err := tx.Exec(ctx, `INSERT INTO portal_appointment_reservations(id,client_id,quote_request_id,quote_version,quote_sha256)
 VALUES($1,$2,NULLIF($3,''),NULLIF($4,0),NULLIF($5,'')) ON CONFLICT DO NOTHING`, reservation, client, quote.Request, quote.Version, quote.Hash)
		if err != nil {
			return appointmentHold{}, errTriageStorage
		}
		if tag.RowsAffected() != 1 {
			return appointmentHold{}, errProposalConflict
		}
	}
	revision := expected + 1
	if _, err := tx.Exec(ctx, `INSERT INTO portal_appointment_revisions(reservation_id,client_id,revision,starts_at,ends_at,held_until,recorded_at)
 VALUES($1,$2,$3,$4,$5,$6,$7)`, reservation, client, revision, slot.Start, slot.End, until, now); err != nil {
		return appointmentHold{}, creditWriteError(err)
	}
	return appointmentHold{Revision: revision, Slot: slot, HeldUntil: until, Quote: quote, recordedAt: now}, nil
}

func lockAppointmentCalendarTx(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, appointmentCalendarLock); err != nil {
		return time.Time{}, errTriageStorage
	}
	// Take a fresh statement snapshot and database time after waiting. A stronger
	// isolation mode can retain a snapshot from before the other writer committed.
	var now time.Time
	var isolation string
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp(),current_setting('transaction_isolation')`).Scan(&now, &isolation); err != nil || isolation != "read committed" {
		return time.Time{}, errTriageStorage
	}
	return now, nil
}

// checkSlot performs a fresh calendar read and checks the requested interval
// against the policy and commitments under the caller's calendar lock.
func (holds appointmentHoldPolicy) checkSlot(ctx context.Context, tx pgx.Tx, reservation string, slot booking.Interval, now time.Time) (time.Time, error) {
	calendarRange, commitmentRange, err := booking.RequiredCoverage(holds.policy, now, slot)
	if err != nil || holds.provider == nil {
		return time.Time{}, booking.ErrUnavailable
	}
	// Bound the provider read while other reservation writers wait on the
	// same calendar lock.
	calendarCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	calendar, err := holds.provider.Busy(calendarCtx, calendarRange)
	cancel()
	if err != nil {
		return time.Time{}, booking.ErrUnavailable
	}
	// Measure the hold lifetime and snapshot freshness after the provider read.
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, errTriageStorage
	}
	commitments, err := readAppointmentCommitments(ctx, tx, reservation, now, commitmentRange)
	if err != nil {
		return time.Time{}, err
	}
	// The adapter records observation time on the application host. Preserve
	// its age when comparing it with database time for the reservation.
	if !calendar.ObservedAt.IsZero() {
		calendar.ObservedAt = now.Add(-time.Since(calendar.ObservedAt))
	}
	available, err := booking.Available(holds.policy, booking.Input{
		Now: now, Search: slot, Duration: slot.End.Sub(slot.Start), Calendar: calendar, Commitments: commitments,
		CommitmentsCoverage: commitmentRange, CommitmentsComplete: true,
	})
	if err != nil {
		return time.Time{}, err
	}
	if len(available) != 1 || !available[0].Start.Equal(slot.Start) || !available[0].End.Equal(slot.End) {
		return time.Time{}, errProposalConflict
	}
	return now, nil
}

func readAppointmentCommitments(ctx context.Context, tx pgx.Tx, reservation string, now time.Time, coverage booking.Interval) ([]booking.Interval, error) {
	rows, err := tx.Query(ctx, `SELECT r.starts_at,r.ends_at FROM portal_appointment_revisions r
 WHERE r.reservation_id<>$1 AND r.starts_at<$4 AND r.ends_at>$3 AND (
   (r.held_until>$2 AND NOT EXISTS(SELECT 1 FROM portal_appointment_revisions later
     WHERE later.reservation_id=r.reservation_id AND later.revision>r.revision)
     AND NOT EXISTS(SELECT 1 FROM portal_appointment_introductions i WHERE i.reservation_id=r.reservation_id))
   OR EXISTS(SELECT 1 FROM portal_appointment_introductions i
     WHERE i.reservation_id=r.reservation_id AND i.revision=r.revision
       AND NOT EXISTS(SELECT 1 FROM portal_appointment_introduction_cancellations c WHERE c.reservation_id=i.reservation_id))
   OR EXISTS(SELECT 1 FROM portal_orders o
     JOIN portal_service_fulfillments f ON f.order_id=o.id
     JOIN portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind='appointment'
     WHERE o.appointment_reservation_id=r.reservation_id AND o.appointment_revision=r.revision
       AND c.aggregate_id=r.reservation_id::text))
 LIMIT $5`, reservation, now, coverage.Start, coverage.End, booking.MaxIntervals+1)
	if err != nil {
		return nil, errTriageStorage
	}
	holds, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (booking.Interval, error) {
		var interval booking.Interval
		err := row.Scan(&interval.Start, &interval.End)
		return interval, err
	})
	if err != nil {
		return nil, errTriageStorage
	}
	return holds, nil
}

// readLatestAppointmentHold scopes the stored interval to its real client. Any
// caller making a decision uses the same calendar lock through commit.
func readLatestAppointmentHold(ctx context.Context, tx pgx.Tx, reservation, client string) (appointmentHold, error) {
	var hold appointmentHold
	err := tx.QueryRow(ctx, `SELECT v.revision,v.starts_at,v.ends_at,v.held_until,v.recorded_at,
 COALESCE(r.quote_request_id,''),COALESCE(r.quote_version,0),COALESCE(r.quote_sha256::text,''),
 i.reservation_id IS NOT NULL,c.reservation_id IS NOT NULL,i.cancellable_until,
 EXISTS(SELECT 1 FROM portal_orders o WHERE o.appointment_reservation_id=r.id),
 EXISTS(SELECT 1 FROM portal_orders o
   JOIN portal_service_fulfillments f ON f.order_id=o.id
   JOIN portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind='appointment'
   WHERE o.appointment_reservation_id=r.id AND o.appointment_revision=v.revision AND c.aggregate_id=r.id::text)
 FROM portal_appointment_revisions v JOIN portal_appointment_reservations r
 ON r.id=v.reservation_id AND r.client_id=v.client_id
 LEFT JOIN portal_appointment_introductions i ON i.reservation_id=r.id AND i.revision=v.revision
 LEFT JOIN portal_appointment_introduction_cancellations c ON c.reservation_id=i.reservation_id
 WHERE v.reservation_id=$1 AND v.client_id=$2
 ORDER BY v.revision DESC LIMIT 1`, reservation, client).
		Scan(&hold.Revision, &hold.Slot.Start, &hold.Slot.End, &hold.HeldUntil, &hold.recordedAt,
			&hold.Quote.Request, &hold.Quote.Version, &hold.Quote.Hash,
			&hold.IntroductionConfirmed, &hold.IntroductionCancelled, &hold.cancellableUntil, &hold.orderPlaced, &hold.PaidConfirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return appointmentHold{}, nil
	}
	if err != nil {
		return appointmentHold{}, errTriageStorage
	}
	return hold, nil
}
