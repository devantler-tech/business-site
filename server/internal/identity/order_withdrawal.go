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
