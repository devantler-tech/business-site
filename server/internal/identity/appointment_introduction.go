package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const introductionDuration = 30 * time.Minute

// confirmIntroduction commits the exact held revision for the authorized
// client. The caller owns the transaction and the client membership check.
func (holds appointmentHoldPolicy) confirmIntroduction(ctx context.Context, tx pgx.Tx, reservation, client string, revision int) (appointmentHold, error) {
	if !opaqueRequestID.MatchString(reservation) || client == "" || revision < 1 {
		return appointmentHold{}, errProposalConflict
	}
	now, err := lockAppointmentCalendarTx(ctx, tx)
	if err != nil {
		return appointmentHold{}, err
	}
	hold, err := readLatestAppointmentHold(ctx, tx, reservation, client)
	if err != nil {
		return appointmentHold{}, err
	}
	if hold.Revision != revision || hold.Quote != (appointmentQuoteRef{}) || hold.Slot.End.Sub(hold.Slot.Start) != introductionDuration {
		return appointmentHold{}, errProposalConflict
	}
	if hold.IntroductionConfirmed {
		return hold, nil
	}
	if !hold.HeldUntil.After(now) {
		return appointmentHold{}, errProposalConflict
	}
	var hasIntroduction bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_appointment_introductions WHERE client_id=$1)`, client).Scan(&hasIntroduction); err != nil {
		return appointmentHold{}, errTriageStorage
	}
	if hasIntroduction {
		return appointmentHold{}, errProposalConflict
	}
	now, err = holds.checkSlot(ctx, tx, reservation, hold.Slot, now)
	if err != nil {
		return appointmentHold{}, err
	}
	if !hold.HeldUntil.After(now) {
		return appointmentHold{}, errProposalConflict
	}
	if _, err := tx.Exec(ctx, `INSERT INTO portal_appointment_introductions(client_id,reservation_id,revision)
 VALUES($1,$2,$3)`, client, reservation, revision); err != nil {
		return appointmentHold{}, creditWriteError(err)
	}
	hold.IntroductionConfirmed = true
	return hold, nil
}
