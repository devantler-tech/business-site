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

type appointmentHold struct {
	Revision   int
	Slot       booking.Interval
	HeldUntil  time.Time
	recordedAt time.Time
}

// appendAppointmentHold compares and appends a provisional hold in the caller's
// transaction. The caller supplies the server-bound client and checks booking
// policy, calendar availability and authority before committing the hold.
func appendAppointmentHold(ctx context.Context, tx pgx.Tx, reservation, client string, expected int, slot booking.Interval, lifetime time.Duration) (appointmentHold, error) {
	duration := slot.End.Sub(slot.Start)
	if !opaqueRequestID.MatchString(reservation) || client == "" || expected < 0 || expected >= math.MaxInt32 ||
		duration <= 0 || duration > 24*time.Hour || duration%time.Minute != 0 || slot.Start.Nanosecond()%int(time.Microsecond) != 0 ||
		lifetime <= 0 || lifetime > 10*time.Minute || lifetime%time.Microsecond != 0 {
		return appointmentHold{}, errProposalConflict
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, appointmentCalendarLock); err != nil {
		return appointmentHold{}, errTriageStorage
	}
	// Take a fresh statement snapshot and database time after waiting. A stronger
	// isolation mode can retain a snapshot from before the other writer committed.
	var now time.Time
	var isolation string
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp(),current_setting('transaction_isolation')`).Scan(&now, &isolation); err != nil || isolation != "read committed" {
		return appointmentHold{}, errTriageStorage
	}
	prior, err := readLatestAppointmentHold(ctx, tx, reservation, client)
	if err != nil {
		return appointmentHold{}, err
	}
	if prior.Revision == expected+1 && prior.HeldUntil.After(now) && prior.Slot.Start.Equal(slot.Start) && prior.Slot.End.Equal(slot.End) &&
		prior.HeldUntil.Sub(prior.recordedAt) == lifetime {
		return prior, nil
	}
	if prior.Revision != expected || (prior.Revision > 0 && !prior.HeldUntil.After(now)) {
		return appointmentHold{}, errProposalConflict
	}
	until := now.Add(lifetime)
	if until.After(slot.Start) {
		return appointmentHold{}, errProposalConflict
	}
	var occupied bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM portal_appointment_revisions r
 WHERE r.reservation_id<>$1 AND r.held_until>$2 AND r.starts_at<$4 AND r.ends_at>$3
   AND NOT EXISTS(SELECT 1 FROM portal_appointment_revisions later
     WHERE later.reservation_id=r.reservation_id AND later.revision>r.revision))`, reservation, now, slot.Start, slot.End).Scan(&occupied)
	if err != nil {
		return appointmentHold{}, errTriageStorage
	}
	if occupied {
		return appointmentHold{}, errProposalConflict
	}
	if expected == 0 {
		tag, err := tx.Exec(ctx, `INSERT INTO portal_appointment_reservations(id,client_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, reservation, client)
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
	return appointmentHold{Revision: revision, Slot: slot, HeldUntil: until, recordedAt: now}, nil
}

// readLatestAppointmentHold scopes the stored interval to its real client. Any
// caller making a decision uses the same calendar lock through commit.
func readLatestAppointmentHold(ctx context.Context, tx pgx.Tx, reservation, client string) (appointmentHold, error) {
	var hold appointmentHold
	err := tx.QueryRow(ctx, `SELECT revision,starts_at,ends_at,held_until,recorded_at
 FROM portal_appointment_revisions WHERE reservation_id=$1 AND client_id=$2
 ORDER BY revision DESC LIMIT 1`, reservation, client).Scan(&hold.Revision, &hold.Slot.Start, &hold.Slot.End, &hold.HeldUntil, &hold.recordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return appointmentHold{}, nil
	}
	if err != nil {
		return appointmentHold{}, errTriageStorage
	}
	return hold, nil
}
