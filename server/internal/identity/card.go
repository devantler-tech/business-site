package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/devantler-tech/business-site/server/internal/stripe"
	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

// StripeAPI is the part of the Stripe client the card path uses.
type StripeAPI interface {
	CreateCheckout(ctx context.Context, key string, checkout stripe.Checkout) (stripe.Session, error)
}

// CardBilling configures the card and MobilePay path (ADR 0030, #553).
type CardBilling struct {
	Stripe        StripeAPI
	WebhookSecret string
	Now           func() time.Time
}

// EnableCardBilling supplies the Stripe client. The false-default
// portal-card-billing gate still decides whether anything happens.
func (s *Foundation) EnableCardBilling(cfg CardBilling) error {
	if cfg.Stripe == nil || cfg.WebhookSecret == "" || cfg.Now == nil {
		return errors.New("card billing is not configured")
	}
	s.cards.Store(&cfg)
	return nil
}

// cardBillingReady is the false-default gate for card orders, their Checkout
// links and the Stripe webhook. It has no environment mapping.
func (s *Foundation) cardBillingReady(ctx context.Context) bool {
	if s.cards.Load() == nil || !s.acceptanceReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-card-billing", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx)
}

// orderCheckout is a card order's latest Checkout Session for one purpose:
// "one-time" (card or MobilePay, paid now) or "monthly" (a card subscription).
type orderCheckout struct {
	Purpose, URL, State string
}

// Open reports a Checkout the client can still complete.
func (c orderCheckout) Open() bool { return c.State == "open" }

// Replaceable reports a Checkout Stripe has said can no longer be completed:
// it expired or its asynchronous payment failed. Stripe never expires a
// completed session, so a replacement never runs beside a payable attempt.
func (c orderCheckout) Replaceable() bool { return c.State == "closed" }

// cardPurposes are the Checkout Sessions a card order needs, in display order.
func cardPurposes(oneTime, monthly int64) []string {
	var purposes []string
	if oneTime > 0 {
		purposes = append(purposes, "one-time")
	}
	if monthly > 0 {
		purposes = append(purposes, "monthly")
	}
	return purposes
}

// latestCheckout returns the newest session for one purpose. Only Stripe's
// own events close a session; its local expiry time is not enough, because a
// completion just before it may still be on its way.
func (s *Foundation) latestCheckout(ctx context.Context, order, purpose string) (orderCheckout, int, error) {
	c := orderCheckout{Purpose: purpose}
	var attempt int
	err := s.pool.QueryRow(ctx, `SELECT url,state,attempt FROM portal_card_checkouts WHERE order_id=$1 AND purpose=$2 ORDER BY attempt DESC LIMIT 1`, order, purpose).Scan(&c.URL, &c.State, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderCheckout{Purpose: purpose, State: "closed"}, 0, nil
	}
	if err != nil {
		return orderCheckout{}, 0, errTriageStorage
	}
	return c, attempt, nil
}

// readCheckouts returns a card order's latest session for each purpose it needs.
func (s *Foundation) readCheckouts(ctx context.Context, o projectOrder) ([]orderCheckout, error) {
	var result []orderCheckout
	for _, purpose := range cardPurposes(o.OneTimeMinor, o.MonthlyMinor) {
		c, _, err := s.latestCheckout(ctx, o.ID, purpose)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// firstAnchor is the subscription's first charge: 00:00 UTC on the 1st of the
// order's first billed month, or of the next month once that has begun. Stripe
// renews on the anchor's UTC day, so a UTC midnight keeps every renewal on the
// 1st; in Copenhagen that is 01:00 or 02:00 on the same day.
func firstAnchor(monthlyFrom, now time.Time) time.Time {
	anchor := time.Date(monthlyFrom.Year(), monthlyFrom.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !anchor.After(now) {
		anchor = anchor.AddDate(0, 1, 0)
	}
	return anchor
}

// ensureCheckouts creates a Checkout Session for every purpose whose latest
// session is closed or missing. Sessions that are open, processing or paid are
// kept. A browser return is never proof of payment; only the signed webhook
// funds the order.
func (s *Foundation) ensureCheckouts(ctx context.Context, order, origin, lang string) error {
	if !s.cardBillingReady(ctx) {
		return ErrDenied
	}
	var o projectOrder
	var requestID, scope, email string
	err := s.pool.QueryRow(ctx, `SELECT o.id,o.request_id,o.one_time_minor,o.monthly_minor,o.monthly_from,p.scope,coalesce(c.email,'')
 FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id JOIN portal_proposals p ON p.request_id=a.request_id AND p.version=a.proposal_version
 LEFT JOIN portal_contacts c ON c.client_id=o.client_id WHERE o.id=$1 AND o.collection='card'`, order).
		Scan(&o.ID, &requestID, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom, &scope, &email)
	if err != nil {
		return errTriageStorage
	}
	if lang != "da" {
		lang = "en"
	}
	back := origin + "/portal/requests/" + requestID + "?lang=" + lang
	name := invoiceLine(scope)
	cfg := s.cards.Load()
	for _, purpose := range cardPurposes(o.OneTimeMinor, o.MonthlyMinor) {
		latest, attempt, err := s.latestCheckout(ctx, o.ID, purpose)
		if err != nil {
			return err
		}
		if !latest.Replaceable() {
			continue
		}
		checkout := stripe.Checkout{Mode: "payment", ClientReference: o.ID, CustomerEmail: email, Locale: lang, SuccessURL: back, CancelURL: back,
			Metadata: map[string]string{"portal_order": o.ID}, Lines: []stripe.Line{{Name: name, AmountMinor: o.OneTimeMinor}}}
		if purpose == "monthly" {
			// Card only by Stripe's own rules: MobilePay has no recurring use.
			// Nothing is charged before the anchor; it then charges each month.
			checkout.Mode = "subscription"
			checkout.BillingCycleAnchor = firstAnchor(o.MonthlyFrom, cfg.Now())
			checkout.Lines = []stripe.Line{{Name: name, AmountMinor: o.MonthlyMinor, Monthly: true}}
		}
		attempt++
		created, err := cfg.Stripe.CreateCheckout(ctx, "portal-"+o.ID+"-"+purpose+"-checkout-"+strconv.Itoa(attempt), checkout)
		if err != nil {
			return err
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO portal_card_checkouts(session_id,order_id,purpose,attempt,url,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(session_id) DO NOTHING`,
			created.ID, o.ID, purpose, attempt, created.URL, time.Unix(created.ExpiresAt, 0)); err != nil {
			return errTriageStorage
		}
	}
	return nil
}

// stripeWebhook funds card orders from signature-verified Stripe events only.
// Each event is processed once; a failed step returns 500 so Stripe retries.
func (b *browser) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	s := b.foundation
	if r.Method != http.MethodPost || !s.cardBillingReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 256<<10+1))
	if err != nil || len(payload) > 256<<10 {
		http.Error(w, "Invalid event", http.StatusBadRequest)
		return
	}
	cards := s.cards.Load()
	if stripe.VerifySignature(payload, r.Header.Get("Stripe-Signature"), cards.WebhookSecret, cards.Now()) != nil {
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}
	var event stripe.Event
	if err := json.Unmarshal(payload, &event); err != nil || event.ID == "" || event.Type == "" {
		http.Error(w, "Invalid event", http.StatusBadRequest)
		return
	}
	if err := s.handleStripeEvent(r.Context(), event); err != nil {
		// Stripe retries the event and shows it as failed in its Dashboard.
		log.Print("portal: a card billing event was not processed")
		http.Error(w, "Event not processed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleStripeEvent records the event and applies it in one transaction, so a
// retried event is either fully applied once or not at all.
func (s *Foundation) handleStripeEvent(ctx context.Context, event stripe.Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errTriageStorage
	}
	defer rollbackProposalWrite(ctx, tx)
	tag, err := tx.Exec(ctx, `INSERT INTO portal_stripe_events(id,type) VALUES($1,$2) ON CONFLICT(id) DO NOTHING`, event.ID, event.Type)
	if err != nil {
		return errTriageStorage
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded", "checkout.session.expired", "checkout.session.async_payment_failed":
		var session stripe.Session
		if json.Unmarshal(event.Data.Object, &session) != nil || session.ID == "" {
			return errors.New("card billing: malformed session")
		}
		err = applyCheckoutEvent(ctx, tx, event.Type, session)
	case "invoice.paid":
		err = applySubscriptionInvoicePaid(ctx, tx, event.Data.Object)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}

type cardOrder struct {
	ID                         string
	OneTimeMinor, MonthlyMinor int64
}

// cardCheckoutBy finds the card order and purpose behind one of the portal's
// own sessions, by session or subscription ID, locking that session's row.
// column is one of two fixed names.
func cardCheckoutBy(ctx context.Context, tx pgx.Tx, column, value string) (cardOrder, string, error) {
	var o cardOrder
	var purpose string
	if column != "session_id" && column != "subscription_id" {
		return o, "", errTriageStorage
	}
	err := tx.QueryRow(ctx, `SELECT o.id,o.one_time_minor,o.monthly_minor,c.purpose FROM portal_card_checkouts c JOIN portal_orders o ON o.id=c.order_id WHERE c.`+column+`=$1 AND o.collection='card' FOR UPDATE OF c`, value).Scan(&o.ID, &o.OneTimeMinor, &o.MonthlyMinor, &purpose)
	return o, purpose, err
}

// settleCardCharge records a funded period for a card order. The amount must
// equal the order's amount for that period exactly, in DKK.
func settleCardCharge(ctx context.Context, tx pgx.Tx, order, period, reference string, paid, want int64, currency string) error {
	if currency != "dkk" || paid != want || want <= 0 {
		log.Printf("portal: card billing amount mismatch for period %s", period)
		return nil
	}
	net, vat, ok := splitVAT(want)
	if !ok {
		return errors.New("card billing: amount has no exact net")
	}
	_, err := tx.Exec(ctx, `INSERT INTO portal_order_charges(order_id,period,identity,gross_minor,net_minor,vat_minor,state,provider_ref,settled_at) VALUES($1,$2,$3,$4,$5,$6,'settled',$7,clock_timestamp())
ON CONFLICT(order_id,period) DO UPDATE SET state='settled',provider_ref=excluded.provider_ref,settled_at=coalesce(portal_order_charges.settled_at,excluded.settled_at),updated_at=clock_timestamp() WHERE portal_order_charges.state<>'settled'`,
		order, period, "portal-"+order+"-"+period, want, net, vat, reference)
	if err != nil {
		return errTriageStorage
	}
	return nil
}

// applyCheckoutEvent moves one of the portal's sessions through its states.
// Sessions are never forgotten when replaced, so a delayed event still finds
// its own order; a session the portal did not create is ignored.
func applyCheckoutEvent(ctx context.Context, tx pgx.Tx, kind string, session stripe.Session) error {
	o, purpose, err := cardCheckoutBy(ctx, tx, "session_id", session.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	setState := func(state, subscription string, from ...string) error {
		if _, err := tx.Exec(ctx, `UPDATE portal_card_checkouts SET state=$2,subscription_id=coalesce(nullif($3,''),subscription_id),updated_at=clock_timestamp() WHERE session_id=$1 AND state=ANY($4)`, session.ID, state, subscription, from); err != nil {
			return errTriageStorage
		}
		return nil
	}
	switch kind {
	case "checkout.session.expired", "checkout.session.async_payment_failed":
		// Retire the attempt so the client can start a new one.
		return setState("closed", "", "open", "processing")
	case "checkout.session.completed":
		if purpose == "monthly" {
			// The subscription's months are funded by their own invoices.
			return setState("paid", session.Subscription, "open", "processing", "closed")
		}
		if session.PaymentStatus != "paid" {
			// An asynchronous method has not settled yet. A failure that
			// arrived first stays final, so the client can start again.
			return setState("processing", "", "open")
		}
	}
	if purpose != "one-time" || session.Mode != "payment" || session.PaymentStatus != "paid" {
		return nil
	}
	if err := setState("paid", "", "open", "processing", "closed"); err != nil {
		return err
	}
	return settleCardCharge(ctx, tx, o.ID, "acceptance", session.ID, session.AmountTotal, o.OneTimeMinor, session.Currency)
}

// applySubscriptionInvoicePaid funds the month a subscription renewal paid for.
func applySubscriptionInvoicePaid(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(raw, &invoice); err != nil || invoice.ID == "" {
		return errors.New("card billing: malformed invoice")
	}
	subscription := invoice.SubscriptionID()
	if subscription == "" || invoice.BillingReason != "subscription_cycle" {
		return nil
	}
	o, _, err := cardCheckoutBy(ctx, tx, "subscription_id", subscription)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not ours unless it carries the portal's order marker. Events are
		// unordered: a portal subscription whose Checkout completion has not
		// arrived yet fails, so Stripe retries this event later.
		if invoice.Parent.SubscriptionDetails.Metadata["portal_order"] == "" {
			return nil
		}
		return errors.New("card billing: subscription not yet known")
	}
	if err != nil {
		return errTriageStorage
	}
	period := monthPeriod(time.Unix(invoice.Created, 0).In(copenhagen))
	return settleCardCharge(ctx, tx, o.ID, period, invoice.ID, invoice.AmountPaid, o.MonthlyMinor, invoice.Currency)
}
