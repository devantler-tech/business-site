package identity

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
)

// withdrawalDays is the consumer's withdrawal period (Consumer Contracts Act,
// section 19). The portal cannot tell a business from a consumer, so it
// offers the withdrawal to every client (section 2).
const withdrawalDays = 14

// withdrawalDeadline is when the right of withdrawal ends: the period starts
// the day after the order, so it ends at midnight in Copenhagen after the
// 14th day.
func withdrawalDeadline(placed time.Time) time.Time {
	local := placed.In(copenhagen)
	return time.Date(local.Year(), local.Month(), local.Day()+withdrawalDays+1, 0, 0, 0, 0, copenhagen)
}

// WithdrawUntil is the last day the client may withdraw, as YYYY-MM-DD.
func (o projectOrder) WithdrawUntil() string {
	return withdrawalDeadline(o.Placed).AddDate(0, 0, -1).Format("2006-01-02")
}

// Withdrawable reports whether the client may still withdraw.
func (o projectOrder) Withdrawable() bool {
	return o.Withdrawn == nil && time.Now().Before(withdrawalDeadline(o.Placed))
}

// readWithdrawal adds the order's withdrawal, if any, to an order read in tx.
func readWithdrawal(ctx context.Context, tx pgx.Tx, o *projectOrder) error {
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT withdrawn_at FROM portal_order_withdrawals WHERE order_id=$1`, o.ID).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	o.Withdrawn = &at
	return nil
}

// withdrawOrder records the client's withdrawal once, within the period, and
// stops all further billing: no new charge or Checkout, monthly billing ends
// before its first month, and a card subscription ends before it would first
// renew, or at once if a month has been charged. What was paid is refunded by
// the operator, by the same payment method, within 14 days (section 22).
func (s *Foundation) withdrawOrder(ctx context.Context, p Principal, id, orderID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginClientProposalWrite, false)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if o.Withdrawn == nil && !time.Now().Before(withdrawalDeadline(o.Placed)) {
		return errProposalConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_order_withdrawals(order_id,client_issuer,client_subject) VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING`, o.ID, p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	if o.MonthlyMinor > 0 {
		// The month before the first billed one: no month is billed. An end
		// already recorded stands; billing still stops at the withdrawal.
		if _, err = tx.Exec(ctx, `INSERT INTO portal_order_ends(order_id,last_month,source,client_issuer,client_subject) VALUES($1,$2,'client',$3,$4) ON CONFLICT(order_id) DO NOTHING`, o.ID, o.MonthlyFrom.AddDate(0, -1, 0), p.issuer, p.subject); err != nil {
			return errTriageStorage
		}
	}
	if err = s.commitOrder(ctx, p, tx, s.orderReady); err != nil {
		return err
	}
	if o.Collection == "card" && o.MonthlyMinor > 0 && s.scheduleCardEnd(ctx, o.ID) != nil {
		// The withdrawal is recorded; the operator's page offers to ask
		// Stripe again until it confirms.
		log.Printf("portal: card subscription end not confirmed")
	}
	return nil
}
