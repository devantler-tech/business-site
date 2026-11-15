package identity

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// orderEnd is an order's last billed month (#591) and, for a card order, the
// subscription end Stripe has confirmed.
type orderEnd struct {
	LastMonth time.Time
	Source    string
	CancelAt  *time.Time
}

// Last is the last billed month, as YYYY-MM.
func (e orderEnd) Last() string { return e.LastMonth.Format("2006-01") }

// currentMonth is the first of the current Copenhagen month, as a UTC date.
func currentMonth(now time.Time) time.Time {
	local := now.In(copenhagen)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// EndFrom is the earliest month an operator may choose as the last billed one.
// Earlier months may already be invoiced or charged.
func (o projectOrder) EndFrom() string { return currentMonth(time.Now()).Format("2006-01") }

// parseOrderEndForm admits exactly the operator's end form: the order and a
// last billed month within ten years either way. endMonthlyBilling requires a
// new end to start from the current month; a recorded one can always be retried.
func parseOrderEndForm(r *http.Request, now time.Time) (string, time.Time, bool) {
	if err := r.ParseForm(); err != nil {
		return "", time.Time{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return "", time.Time{}, false
		}
		switch name {
		case "csrf_token", "lang", "order", "last_month":
		default:
			return "", time.Time{}, false
		}
	}
	order := r.PostForm.Get("order")
	last, err := time.Parse("2006-01", r.PostForm.Get("last_month"))
	from := currentMonth(now)
	if err != nil || !opaqueRequestID.MatchString(order) || last.Before(from.AddDate(-10, 0, 0)) || last.After(from.AddDate(10, 0, 0)) {
		return "", time.Time{}, false
	}
	return order, last, true
}

// endOrder lets a current operator end an order's monthly billing.
func (b *browser) endOrder(w http.ResponseWriter, r *http.Request) {
	order, last, valid := parseOrderEndForm(r, time.Now())
	if !valid {
		b.render(w, r, 400, "error", "", "")
		return
	}
	err := b.foundation.endMonthlyBilling(r.Context(), b.principal(r.Context()), r.PathValue("id"), order, last)
	b.proposalMutationResult(w, r, err, "error")
}

// parseClientOrderForm admits exactly a client's end or withdrawal form: the order.
func parseClientOrderForm(r *http.Request) (string, bool) {
	if err := r.ParseForm(); err != nil {
		return "", false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return "", false
		}
		switch name {
		case "csrf_token", "lang", "order":
		default:
			return "", false
		}
	}
	order := r.PostForm.Get("order")
	return order, opaqueRequestID.MatchString(order)
}

// clientOrderAction serves one of the client's own order forms on the
// receipt: ending monthly billing where they ordered it (Consumer Contracts
// Act, section 28 a), or confirming a withdrawal (section 20 a).
func (b *browser) clientOrderAction(act func(context.Context, Principal, string, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		order, valid := parseClientOrderForm(r)
		if !valid {
			b.render(w, r, 400, "error", "", "")
			return
		}
		b.clientOrderResult(w, r, act(r.Context(), b.principal(r.Context()), r.PathValue("id"), order))
	}
}

// clientEndMonthlyBilling ends the order's monthly billing after the current
// month: the notice is at most the rest of that month, and nothing already
// billed changes. An end already recorded, by anyone, stands, so a retry
// succeeds. A card order's subscription is then asked to end when it would
// next renew.
func (s *Foundation) clientEndMonthlyBilling(ctx context.Context, p Principal, id, orderID string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginClientProposalWrite, true)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if _, err = tx.Exec(ctx, `INSERT INTO portal_order_ends(order_id,last_month,source,client_issuer,client_subject) VALUES($1,$2,'client',$3,$4) ON CONFLICT(order_id) DO NOTHING`, o.ID, currentMonth(time.Now()), p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	if err = s.commitOrder(ctx, p, tx, s.pathReady(o.Collection)); err != nil {
		return err
	}
	if o.Collection == "card" && s.scheduleCardEnd(ctx, o.ID) != nil {
		// The end is recorded, so the client's form is gone; the operator's
		// page offers to ask Stripe again until it confirms.
		log.Printf("portal: card subscription end not confirmed")
	}
	return nil
}

// orderWrite opens the request's locked write as begin authorizes it and
// reads its order, which must be the one the form named and, for an end of
// monthly billing, bill monthly.
func (s *Foundation) orderWrite(ctx context.Context, p Principal, id, orderID string, begin func(context.Context, Principal, string) (pgx.Tx, string, error), monthly bool) (pgx.Tx, projectOrder, error) {
	if !s.orderReady(ctx) {
		return nil, projectOrder{}, ErrDenied
	}
	tx, client, err := begin(ctx, p, id)
	if err != nil {
		return nil, projectOrder{}, err
	}
	o, err := readOrderTx(ctx, tx, id, client)
	if err == nil && (o.ID != orderID || (monthly && o.MonthlyMinor == 0)) {
		err = errProposalConflict
	}
	if err != nil {
		rollbackProposalWrite(ctx, tx)
		return nil, projectOrder{}, err
	}
	return tx, o, nil
}

// endMonthlyBilling records the order's last billed month once; the same month
// again is a retry and a different one a conflict. A card order's subscription
// is then asked to end when it would next renew.
func (s *Foundation) endMonthlyBilling(ctx context.Context, p Principal, id, orderID string, last time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginOperatorWrite, true)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	ready := s.pathReady(o.Collection)
	if o.End == nil && last.Before(currentMonth(time.Now())) {
		// Earlier months may already be invoiced or charged.
		return errProposalConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_order_ends(order_id,last_month,source,operator_issuer,operator_subject) VALUES($1,$2,'operator',$3,$4) ON CONFLICT(order_id) DO NOTHING`, o.ID, last, p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	var recorded time.Time
	if err = tx.QueryRow(ctx, `SELECT last_month FROM portal_order_ends WHERE order_id=$1`, o.ID).Scan(&recorded); err != nil {
		return errTriageStorage
	}
	if !recorded.Equal(last) {
		return errProposalConflict
	}
	if err = commitProposalWrite(ctx, p, tx, ready); err != nil {
		return err
	}
	if o.Collection == "card" {
		return s.scheduleCardEnd(ctx, o.ID)
	}
	return nil
}

// scheduleCardEnd asks Stripe to end the order's subscription at 00:00 UTC on
// the 1st after the last billed month, exactly when it would renew, and records
// Stripe's confirmation. It is safe to repeat, and does nothing for an order
// that is not ended or has no subscription yet.
func (s *Foundation) scheduleCardEnd(ctx context.Context, order string) error {
	if !s.cardBillingReady(ctx) {
		return ErrDenied
	}
	var subscription string
	var last time.Time
	err := s.pool.QueryRow(ctx, `SELECT c.subscription_id,e.last_month FROM portal_order_ends e JOIN portal_card_checkouts c ON c.order_id=e.order_id AND c.purpose='monthly' AND c.subscription_id IS NOT NULL
 WHERE e.order_id=$1 AND NOT EXISTS (SELECT 1 FROM portal_card_cancellations x WHERE x.subscription_id=c.subscription_id) ORDER BY c.attempt DESC LIMIT 1`, order).Scan(&subscription, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	api, at := s.cards.Load().Stripe, last.AddDate(0, 1, 0)
	var confirmed time.Time
	if at.After(time.Now()) {
		confirmed, err = api.CancelSubscriptionAt(ctx, subscription, at)
	} else {
		// The renewal after the last month has passed unconfirmed: end now.
		// A month it charged never funds the order and is refunded by hand.
		confirmed, err = api.CancelSubscriptionNow(ctx, subscription)
	}
	if err != nil {
		// The operator page keeps offering to ask again.
		log.Print("portal: card subscription end not confirmed")
		return nil
	}
	if _, err = s.pool.Exec(ctx, `INSERT INTO portal_card_cancellations(subscription_id,order_id,cancel_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, subscription, order, confirmed); err != nil {
		return errTriageStorage
	}
	return nil
}

// readOrderEnd adds the order's end, if any, to an order read in tx.
func readOrderEnd(ctx context.Context, tx pgx.Tx, o *projectOrder) error {
	var e orderEnd
	err := tx.QueryRow(ctx, `SELECT e.last_month,e.source,(SELECT max(x.cancel_at) FROM portal_card_cancellations x WHERE x.order_id=e.order_id) FROM portal_order_ends e WHERE e.order_id=$1`, o.ID).Scan(&e.LastMonth, &e.Source, &e.CancelAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	o.End = &e
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_card_checkouts WHERE order_id=$1 AND purpose='monthly' AND subscription_id IS NOT NULL)`, o.ID).Scan(&o.Subscribed); err != nil {
		return errTriageStorage
	}
	return nil
}
