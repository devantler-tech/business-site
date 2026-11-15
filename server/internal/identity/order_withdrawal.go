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
// 14th day. A last day that is a public holiday, a Saturday, Constitution Day,
// 24 or 31 December moves to the next working day (section 19(6)).
func withdrawalDeadline(placed time.Time) time.Time {
	local := placed.In(copenhagen)
	last := time.Date(local.Year(), local.Month(), local.Day()+withdrawalDays, 0, 0, 0, 0, time.UTC)
	for closedDay(last) {
		last = last.AddDate(0, 0, 1)
	}
	return time.Date(last.Year(), last.Month(), last.Day()+1, 0, 0, 0, 0, copenhagen)
}

// closedDay reports a date on which a withdrawal period cannot end: a
// weekend, a Danish public holiday, Constitution Day, 24 or 31 December.
func closedDay(d time.Time) bool {
	if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		return true
	}
	switch d.Month() {
	case time.January:
		if d.Day() == 1 {
			return true
		}
	case time.June:
		if d.Day() == 5 {
			return true
		}
	case time.December:
		if d.Day() == 24 || d.Day() == 25 || d.Day() == 26 || d.Day() == 31 {
			return true
		}
	}
	// Maundy Thursday, Good Friday, Easter Monday, Ascension Day and Whit
	// Monday; Easter and Whit Sunday are Sundays.
	easter := easterSunday(d.Year())
	for _, offset := range []int{-3, -2, 1, 39, 50} {
		if d.Equal(easter.AddDate(0, 0, offset)) {
			return true
		}
	}
	return false
}

// easterSunday is Easter Sunday of a Gregorian year (the anonymous algorithm).
func easterSunday(year int) time.Time {
	a, b, c := year%19, year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	return time.Date(year, time.Month((h+l-7*m+114)/31), (h+l-7*m+114)%31+1, 0, 0, 0, 0, time.UTC)
}

// WithdrawUntil is the last day the client may withdraw, as YYYY-MM-DD.
func (o projectOrder) WithdrawUntil() string {
	return withdrawalDeadline(o.Placed).AddDate(0, 0, -1).Format("2006-01-02")
}

// WorkStartsFrom is the first day work may start without the client's
// request to start early: the day after the withdrawal period, as YYYY-MM-DD.
func (o projectOrder) WorkStartsFrom() string {
	return withdrawalDeadline(o.Placed).Format("2006-01-02")
}

// RightEnded reports that early-started work is completely finished, which
// ends the right of withdrawal (section 18(2)(2)).
func (o projectOrder) RightEnded() bool { return o.EarlyStart != nil && o.Completed != nil }

// Withdrawable reports whether the client may still withdraw.
func (o projectOrder) Withdrawable() bool {
	return o.Withdrawn == nil && !o.RightEnded() && time.Now().Before(withdrawalDeadline(o.Placed))
}

// OwedDKK is a withdrawal's recorded amount owed for the work done.
func (o projectOrder) OwedDKK() int64 { return *o.Owed / 100 }

// Refunds reports that what was paid exceeds the amount owed for the work done.
func (o projectOrder) Refunds() bool { return o.Owed != nil && o.Paid != nil && *o.Paid > *o.Owed }

// readWithdrawal adds the order's withdrawal, if any, to an order read in tx.
func readWithdrawal(ctx context.Context, tx pgx.Tx, o *projectOrder) error {
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT w.withdrawn_at,(SELECT max(x.cancel_at) FROM portal_withdrawal_cancellations x WHERE x.order_id=w.order_id),
 EXISTS (SELECT 1 FROM portal_card_checkouts c WHERE c.order_id=w.order_id AND c.purpose='monthly' AND c.subscription_id IS NOT NULL),
 (SELECT s.owed_minor FROM portal_withdrawal_settlements s WHERE s.order_id=w.order_id),
 (SELECT sum(c.gross_minor)::bigint FROM portal_order_charges c WHERE c.order_id=w.order_id AND c.state='settled')
 FROM portal_order_withdrawals w WHERE w.order_id=$1`, o.ID).Scan(&at, &o.WithdrawalEnd, &o.Subscribed, &o.Owed, &o.Paid)
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
// the operator, by the same payment method, within 14 days (section 22), less
// the work done if the client asked for an early start (section 21).
func (s *Foundation) withdrawOrder(ctx context.Context, p Principal, id, orderID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginClientProposalWrite, false)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if o.Withdrawn == nil && (o.RightEnded() || !time.Now().Before(withdrawalDeadline(o.Placed))) {
		return errProposalConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_order_withdrawals(order_id,client_issuer,client_subject) VALUES($1,$2,$3) ON CONFLICT(order_id) DO NOTHING`, o.ID, p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	if err = s.commitOrder(ctx, p, tx, s.orderReady); err != nil {
		return err
	}
	if o.Collection == "card" && o.MonthlyMinor > 0 && s.endWithdrawnSubscription(ctx, o.ID) != nil {
		// The withdrawal is recorded. The operator's page shows the
		// unconfirmed end, to finish in the Stripe Dashboard.
		log.Printf("portal: withdrawn card subscription end not confirmed")
	}
	return nil
}

// endWithdrawnSubscription ends a withdrawn order's card subscription when it
// would first charge, or at once if it already has, and records Stripe's
// confirmation. It takes precedence over an end scheduled earlier unless that
// one comes first, and changes none of the recorded history. It does nothing
// for an order that is not withdrawn, has no subscription or is confirmed.
func (s *Foundation) endWithdrawnSubscription(ctx context.Context, order string) error {
	if !s.cardBillingReady(ctx) {
		return ErrDenied
	}
	var subscription string
	var from time.Time
	var scheduled *time.Time
	err := s.pool.QueryRow(ctx, `SELECT c.subscription_id,o.monthly_from,(SELECT x.cancel_at FROM portal_card_cancellations x WHERE x.subscription_id=c.subscription_id)
 FROM portal_order_withdrawals w JOIN portal_orders o ON o.id=w.order_id JOIN portal_card_checkouts c ON c.order_id=w.order_id AND c.purpose='monthly' AND c.subscription_id IS NOT NULL
 WHERE w.order_id=$1 AND NOT EXISTS (SELECT 1 FROM portal_withdrawal_cancellations y WHERE y.subscription_id=c.subscription_id) ORDER BY c.attempt DESC LIMIT 1`, order).Scan(&subscription, &from, &scheduled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	api, now := s.cards.Load().Stripe, time.Now()
	var confirmed time.Time
	switch {
	case scheduled != nil && (!scheduled.After(now) || !scheduled.After(from)):
		// Stripe already ends it no later than the withdrawal would.
		confirmed = *scheduled
	case from.After(now):
		confirmed, err = api.CancelSubscriptionAt(ctx, subscription, from)
	default:
		confirmed, err = api.CancelSubscriptionNow(ctx, subscription)
	}
	if err != nil {
		return err
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO portal_withdrawal_cancellations(subscription_id,order_id,cancel_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, subscription, order, confirmed); err != nil {
		return errTriageStorage
	}
	return nil
}
