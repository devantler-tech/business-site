package identity

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// confirmPaidAppointment consumes the settled, accepted appointment order for
// its own immutable revision. The reservation stays provisional until the
// original order confirmation has been accepted by the sender and the calendar
// still has capacity. A recorded appointment keeps its slot after hold expiry.
func (s *Foundation) confirmPaidAppointment(ctx context.Context, p Principal, request string, selection appointmentOrderSelection, holds appointmentHoldPolicy) (appointmentHold, error) {
	if p.isClientPreview() || !s.paidAppointmentsReady(ctx) ||
		!opaqueRequestID.MatchString(selection.Reservation) || selection.Revision < 1 {
		return appointmentHold{}, ErrDenied
	}
	tx, client, err := s.beginClientProposalWrite(ctx, p, request)
	if err != nil {
		return appointmentHold{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	now, err := lockAppointmentCalendarTx(ctx, tx)
	if err != nil {
		return appointmentHold{}, err
	}
	hold, err := readLatestAppointmentHold(ctx, tx, selection.Reservation, client)
	if err != nil {
		return appointmentHold{}, err
	}
	if hold.Revision != selection.Revision || (!hold.HeldUntil.After(now) && !hold.PaidConfirmed) || hold.Quote.Request != request {
		return appointmentHold{}, errProposalConflict
	}
	review, acceptance, err := currentInvoiceAcceptance(ctx, tx, request, client)
	if err != nil {
		return appointmentHold{}, err
	}
	if review.Proposal.ContentFormat != 2 || review.Proposal.Service.Kind != proposalServiceAppointment ||
		review.Proposal.Version != hold.Quote.Version || review.Hash != hold.Quote.Hash {
		return appointmentHold{}, errProposalConflict
	}
	order, err := readFundingOrder(ctx, tx, request, client, acceptance)
	if err != nil {
		return appointmentHold{}, err
	}
	if order.appointment != selection || order.OneTimeMinor != review.Proposal.OneTimeMinor ||
		order.OneTimeMinor <= 0 || order.MonthlyMinor != 0 ||
		(order.earlyStart == nil && hold.Slot.Start.Before(withdrawalDeadline(order.placed))) {
		return appointmentHold{}, errProposalConflict
	}
	if !s.collectionReady(ctx, order.Collection) {
		return appointmentHold{}, ErrDenied
	}
	if _, err := readSettledOrderFunding(ctx, tx, order); err != nil {
		return appointmentHold{}, err
	}
	proof, err := readOriginalConfirmation(ctx, tx, request, client, acceptance, order.ID)
	if err != nil {
		return appointmentHold{}, err
	}
	if proof.State != confirmationSenderAccepted {
		return appointmentHold{}, errProposalConflict
	}
	if !hold.PaidConfirmed {
		now, err = holds.checkSlot(ctx, tx, selection.Reservation, hold.Slot, now)
		if err != nil {
			return appointmentHold{}, err
		}
		if !hold.HeldUntil.After(now) {
			return appointmentHold{}, errProposalConflict
		}
		if err := bindServiceFulfillment(ctx, tx, serviceFulfillment{
			request: request, client: client, order: order.ID, acceptance: acceptance, quoteHash: review.Hash,
			quoteVersion: review.Proposal.Version, kind: proposalServiceAppointment, consumer: selection.Reservation,
		}); err != nil {
			return appointmentHold{}, err
		}
		if err := holds.queueCalendarCreation(ctx, tx, selection.Reservation, selection.Revision); err != nil {
			return appointmentHold{}, err
		}
	}
	if err := s.commitPaidAppointment(ctx, p, tx, order.Collection, hold); err != nil {
		return appointmentHold{}, err
	}
	hold.PaidConfirmed = true
	return hold, nil
}

func (s *Foundation) commitPaidAppointment(ctx context.Context, p Principal, tx pgx.Tx, collection string, hold appointmentHold) error {
	var heldUntil *time.Time
	if !hold.PaidConfirmed {
		heldUntil = &hold.HeldUntil
	}
	return s.commitOrderWithHold(ctx, p, tx, func(ctx context.Context) bool {
		return s.collectionReady(ctx, collection) && s.paidAppointmentsReady(ctx)
	}, heldUntil)
}
