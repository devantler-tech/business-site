package identity

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

const introductionDuration = 30 * time.Minute

func lockAppointmentHoldRevision(ctx context.Context, tx pgx.Tx, reservation, client string, revision int) (appointmentHold, time.Time, error) {
	if !opaqueRequestID.MatchString(reservation) || client == "" || revision < 1 {
		return appointmentHold{}, time.Time{}, errProposalConflict
	}
	now, err := lockAppointmentCalendarTx(ctx, tx)
	if err != nil {
		return appointmentHold{}, time.Time{}, err
	}
	hold, err := readLatestAppointmentHold(ctx, tx, reservation, client)
	if err != nil {
		return appointmentHold{}, time.Time{}, err
	}
	if hold.Revision != revision {
		return appointmentHold{}, time.Time{}, errProposalConflict
	}
	return hold, now, nil
}

// confirmIntroduction commits the exact held revision for the authorized
// client. The caller owns the transaction and the client membership check.
func (holds appointmentHoldPolicy) confirmIntroduction(ctx context.Context, tx pgx.Tx, reservation, client string, revision int) (appointmentHold, error) {
	hold, now, err := lockAppointmentHoldRevision(ctx, tx, reservation, client, revision)
	if err != nil {
		return appointmentHold{}, err
	}
	if hold.Quote != (appointmentQuoteRef{}) || hold.Slot.End.Sub(hold.Slot.Start) != introductionDuration {
		return appointmentHold{}, errProposalConflict
	}
	if hold.IntroductionCancelled {
		return appointmentHold{}, errProposalConflict
	}
	if hold.IntroductionConfirmed {
		return hold, nil
	}
	if !hold.HeldUntil.After(now) {
		return appointmentHold{}, errProposalConflict
	}
	var sequence int
	var active bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(i.sequence),0),COALESCE(bool_or(c.reservation_id IS NULL),false)
 FROM portal_appointment_introductions i LEFT JOIN portal_appointment_introduction_cancellations c
 ON c.reservation_id=i.reservation_id WHERE i.client_id=$1`, client).Scan(&sequence, &active); err != nil {
		return appointmentHold{}, errTriageStorage
	}
	if active || sequence >= math.MaxInt32 {
		return appointmentHold{}, errProposalConflict
	}
	now, err = holds.checkSlot(ctx, tx, reservation, hold.Slot, now)
	if err != nil {
		return appointmentHold{}, err
	}
	if !hold.HeldUntil.After(now) {
		return appointmentHold{}, errProposalConflict
	}
	notice := int(*holds.policy.CancellationNotice / time.Minute)
	if err := tx.QueryRow(ctx, `INSERT INTO portal_appointment_introductions
 (client_id,sequence,reservation_id,revision,cancellation_notice_minutes,cancellation_terms_version)
 VALUES($1,$2,$3,$4,$5,$6) RETURNING cancellable_until`,
		client, sequence+1, reservation, revision, notice, holds.policy.CancellationTermsVersion).Scan(&hold.cancellableUntil); err != nil {
		return appointmentHold{}, creditWriteError(err)
	}
	hold.IntroductionConfirmed = true
	return hold, nil
}

// cancelIntroduction releases a confirmed slot within the deadline recorded
// at confirmation. The original slot remains part of the client's history.
func cancelIntroduction(ctx context.Context, tx pgx.Tx, reservation, client string, revision int) (appointmentHold, error) {
	hold, now, err := lockAppointmentHoldRevision(ctx, tx, reservation, client, revision)
	if err != nil {
		return hold, err
	}
	if !hold.IntroductionConfirmed || hold.cancellableUntil == nil {
		return appointmentHold{}, errProposalConflict
	}
	if hold.IntroductionCancelled {
		return hold, nil
	}
	if now.After(*hold.cancellableUntil) {
		return appointmentHold{}, errProposalConflict
	}
	result, err := tx.Exec(ctx, `INSERT INTO portal_appointment_introduction_cancellations(reservation_id)
 SELECT reservation_id FROM portal_appointment_introductions WHERE reservation_id=$1 AND client_id=$2 AND revision=$3`,
		reservation, client, revision)
	if err != nil {
		return appointmentHold{}, creditWriteError(err)
	}
	if result.RowsAffected() != 1 {
		return appointmentHold{}, errProposalConflict
	}
	hold.IntroductionCancelled = true
	return hold, nil
}
