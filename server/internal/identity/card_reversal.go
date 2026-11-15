package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/devantler-tech/business-site/server/internal/stripe"
	"github.com/jackc/pgx/v5"
)

// cardReversal is a refund or dispute matched to the funded charge it reverses.
type cardReversal struct {
	Order, Period, Reason, Reference string
}

// resolveReversal matches a charge.refunded or charge.dispute.created event to
// one of the portal's own settled card charges. The charge names only its
// PaymentIntent, so Stripe is asked which Checkout Session or invoice that
// paid, and only references the portal stored count. Anything else belongs to
// another integration and is ignored (nil). A payment on one of the portal's
// own sessions that is not funded yet fails, so Stripe retries the event.
func (s *Foundation) resolveReversal(ctx context.Context, event stripe.Event) (*cardReversal, error) {
	var reason, reference, intent string
	switch event.Type {
	case "charge.refunded":
		var c stripe.Charge
		if json.Unmarshal(event.Data.Object, &c) != nil || c.ID == "" {
			return nil, errors.New("card billing: malformed charge")
		}
		if c.AmountRefunded <= 0 {
			return nil, nil
		}
		reason, reference, intent = "refunded", c.ID, c.PaymentIntent
	case "charge.dispute.created":
		var d stripe.Dispute
		if json.Unmarshal(event.Data.Object, &d) != nil || d.ID == "" {
			return nil, errors.New("card billing: malformed dispute")
		}
		reason, reference, intent = "disputed", d.ID, d.PaymentIntent
	}
	if intent == "" {
		return nil, nil
	}
	refs, err := s.cards.Load().Stripe.PaymentReferences(ctx, intent)
	if err != nil {
		return nil, err
	}
	r := cardReversal{Reason: reason, Reference: reference}
	err = s.pool.QueryRow(ctx, `SELECT c.order_id,c.period FROM portal_order_charges c JOIN portal_orders o ON o.id=c.order_id
 WHERE o.collection='card' AND c.state='settled' AND c.provider_ref=ANY($1) ORDER BY c.period LIMIT 1`, refs).Scan(&r.Order, &r.Period)
	if errors.Is(err, pgx.ErrNoRows) {
		ours, err := s.ownsUnfundedPayment(ctx, refs)
		if err != nil {
			return nil, err
		}
		if ours {
			return nil, errors.New("card billing: reversed payment not funded yet")
		}
		return nil, nil
	}
	if err != nil {
		return nil, errTriageStorage
	}
	return &r, nil
}

// ownsUnfundedPayment reports whether references with no settled charge yet
// are still the portal's own: one of its one-time Checkout Sessions, or an
// invoice of one of its subscriptions, known or carrying the portal's order
// marker. Their funding event has not arrived yet, so the reversal waits.
func (s *Foundation) ownsUnfundedPayment(ctx context.Context, refs []string) (bool, error) {
	var ours bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_card_checkouts WHERE session_id=ANY($1) AND purpose='one-time')`, refs).Scan(&ours); err != nil {
		return false, errTriageStorage
	}
	for _, ref := range refs {
		if ours || !strings.HasPrefix(ref, "in_") {
			continue
		}
		invoice, err := s.cards.Load().Stripe.GetInvoice(ctx, ref)
		if err != nil {
			return false, err
		}
		if invoice.Parent.SubscriptionDetails.Metadata["portal_order"] != "" {
			ours = true
			continue
		}
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_card_checkouts WHERE subscription_id=$1)`, invoice.SubscriptionID()).Scan(&ours); err != nil {
			return false, errTriageStorage
		}
	}
	return ours, nil
}

// applyReversal records that a funded charge no longer funds its order. The
// charge keeps its settlement; the reversal is its own immutable record.
func applyReversal(ctx context.Context, tx pgx.Tx, r *cardReversal) error {
	if r == nil {
		return nil
	}
	// The charge lock serializes this with a work approval's funding check.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM portal_order_charges WHERE order_id=$1 AND period=$2 FOR UPDATE`, r.Order, r.Period); err != nil {
		return errTriageStorage
	}
	if _, err := tx.Exec(ctx, `INSERT INTO portal_charge_reversals(order_id,period,reason,reference) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, r.Order, r.Period, r.Reason, r.Reference); err != nil {
		return errTriageStorage
	}
	return nil
}

// applySubscriptionDeleted records the end of an order's monthly billing when
// Stripe ends its subscription, unless an operator already ended it. The last
// billed month is the one the subscription had paid in advance when it ended.
func applySubscriptionDeleted(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(raw, &sub); err != nil || sub.ID == "" {
		return errors.New("card billing: malformed subscription")
	}
	o, _, err := cardCheckoutBy(ctx, tx, "subscription_id", sub.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A portal subscription whose Checkout completion has not arrived yet
		// fails, so Stripe retries this event; anything else is not ours.
		if sub.Metadata["portal_order"] != "" {
			return errors.New("card billing: ended subscription not yet known")
		}
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	ended := time.Now().UTC()
	if sub.EndedAt > 0 {
		ended = time.Unix(sub.EndedAt, 0).UTC().Add(-time.Second)
	}
	last := time.Date(ended.Year(), ended.Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err = tx.Exec(ctx, `INSERT INTO portal_order_ends(order_id,last_month,source) VALUES($1,$2,'stripe') ON CONFLICT(order_id) DO NOTHING`, o.ID, last); err != nil {
		return errTriageStorage
	}
	return nil
}

// scheduleSubscriptionEnd ends a newly recorded subscription whose order was
// already ended or withdrawn, for example when its Checkout completed after
// the operator's decision or the client's withdrawal.
func (s *Foundation) scheduleSubscriptionEnd(ctx context.Context, subscription string) error {
	var order string
	err := s.pool.QueryRow(ctx, `SELECT order_id FROM portal_card_checkouts WHERE subscription_id=$1`, subscription).Scan(&order)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	if err = s.scheduleCardEnd(ctx, order); err != nil {
		return err
	}
	return s.endWithdrawnSubscription(ctx, order)
}
