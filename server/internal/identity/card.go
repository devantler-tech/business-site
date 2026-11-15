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

// orderCheckout is the card order's current Stripe Checkout link.
type orderCheckout struct {
	URL     string
	Expires time.Time
	Done    bool
}

// readCheckout returns a card order's stored Checkout link without creating one.
func (s *Foundation) readCheckout(ctx context.Context, order string) (*orderCheckout, error) {
	var c orderCheckout
	err := s.pool.QueryRow(ctx, `SELECT url,expires_at,completed FROM portal_card_checkouts WHERE order_id=$1`, order).Scan(&c.URL, &c.Expires, &c.Done)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errTriageStorage
	}
	if !c.Done && !s.cards.Load().Now().Before(c.Expires.Add(-5*time.Minute)) {
		return nil, nil
	}
	return &c, nil
}

// cardCheckout returns an open Checkout Session for a card order, creating a
// new one when none exists or the last one expired. A browser return is never
// proof of payment; only the signed webhook funds the order.
func (s *Foundation) cardCheckout(ctx context.Context, order, origin, lang string) (orderCheckout, error) {
	if !s.cardBillingReady(ctx) {
		return orderCheckout{}, ErrDenied
	}
	if open, err := s.readCheckout(ctx, order); err != nil || open != nil {
		if open == nil {
			return orderCheckout{}, err
		}
		return *open, nil
	}
	var o projectOrder
	var requestID, scope, email string
	var attempt int
	err := s.pool.QueryRow(ctx, `SELECT o.id,o.request_id,o.one_time_minor,o.monthly_minor,o.monthly_from,p.scope,coalesce(c.email,''),coalesce((SELECT k.attempt FROM portal_card_checkouts k WHERE k.order_id=o.id),0)
 FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id JOIN portal_proposals p ON p.request_id=a.request_id AND p.version=a.proposal_version
 LEFT JOIN portal_contacts c ON c.client_id=o.client_id WHERE o.id=$1 AND o.collection='card'`, order).
		Scan(&o.ID, &requestID, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom, &scope, &email, &attempt)
	if err != nil {
		return orderCheckout{}, errTriageStorage
	}
	if lang != "da" {
		lang = "en"
	}
	back := origin + "/portal/requests/" + requestID + "?lang=" + lang
	checkout := stripe.Checkout{Mode: "payment", ClientReference: o.ID, CustomerEmail: email, Locale: lang, SuccessURL: back, CancelURL: back, Metadata: map[string]string{"portal_order": o.ID}}
	name := invoiceLine(scope)
	if o.OneTimeMinor > 0 {
		checkout.Lines = append(checkout.Lines, stripe.Line{Name: name, AmountMinor: o.OneTimeMinor})
	}
	if o.MonthlyMinor > 0 {
		// Monthly card payments start on the order's first billed month, in
		// advance on the 1st; nothing is charged for the days before it.
		checkout.Mode = "subscription"
		checkout.BillingCycleAnchor = time.Date(o.MonthlyFrom.Year(), o.MonthlyFrom.Month(), 1, 0, 0, 0, 0, copenhagen)
		checkout.Lines = append(checkout.Lines, stripe.Line{Name: name, AmountMinor: o.MonthlyMinor, Monthly: true})
	}
	attempt++
	created, err := s.cards.Load().Stripe.CreateCheckout(ctx, "portal-"+o.ID+"-checkout-"+strconv.Itoa(attempt), checkout)
	if err != nil {
		return orderCheckout{}, err
	}
	c := orderCheckout{URL: created.URL, Expires: time.Unix(created.ExpiresAt, 0)}
	if _, err = s.pool.Exec(ctx, `INSERT INTO portal_card_checkouts(order_id,session_id,url,expires_at,attempt) VALUES($1,$2,$3,$4,$5) ON CONFLICT(order_id) DO UPDATE SET session_id=excluded.session_id,url=excluded.url,expires_at=excluded.expires_at,attempt=excluded.attempt,updated_at=clock_timestamp() WHERE NOT portal_card_checkouts.completed`,
		o.ID, created.ID, created.URL, c.Expires, attempt); err != nil {
		return orderCheckout{}, errTriageStorage
	}
	return c, nil
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
		log.Printf("portal: card billing event not processed: %s", event.Type)
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
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		err = applyCheckoutPaid(ctx, tx, event.Data.Object)
	case "checkout.session.expired", "checkout.session.async_payment_failed":
		err = applyCheckoutClosed(ctx, tx, event.Data.Object)
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

func checkoutOrder(ctx context.Context, tx pgx.Tx, session string) (cardOrder, error) {
	var o cardOrder
	err := tx.QueryRow(ctx, `SELECT o.id,o.one_time_minor,o.monthly_minor FROM portal_card_checkouts c JOIN portal_orders o ON o.id=c.order_id WHERE c.session_id=$1 AND o.collection='card' FOR UPDATE OF c`, session).Scan(&o.ID, &o.OneTimeMinor, &o.MonthlyMinor)
	return o, err
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

func applyCheckoutPaid(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var session stripe.Session
	if err := json.Unmarshal(raw, &session); err != nil || session.ID == "" {
		return errors.New("card billing: malformed session")
	}
	o, err := checkoutOrder(ctx, tx, session.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not a portal session (another integration on the same account).
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	if _, err = tx.Exec(ctx, `UPDATE portal_card_checkouts SET completed=true,subscription_id=nullif($2,''),updated_at=clock_timestamp() WHERE session_id=$1`, session.ID, session.Subscription); err != nil {
		return errTriageStorage
	}
	// A subscription's first payment is funded by its own invoice.paid event.
	if session.Mode != "payment" || session.PaymentStatus != "paid" {
		return nil
	}
	return settleCardCharge(ctx, tx, o.ID, "acceptance", session.ID, session.AmountTotal, o.OneTimeMinor, session.Currency)
}

func applyCheckoutClosed(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var session stripe.Session
	if err := json.Unmarshal(raw, &session); err != nil || session.ID == "" {
		return errors.New("card billing: malformed session")
	}
	// Expire the link so the client is offered a fresh Checkout next time.
	if _, err := tx.Exec(ctx, `UPDATE portal_card_checkouts SET expires_at=least(expires_at,clock_timestamp()),updated_at=clock_timestamp() WHERE session_id=$1 AND NOT completed`, session.ID); err != nil {
		return errTriageStorage
	}
	return nil
}

func applySubscriptionInvoicePaid(ctx context.Context, tx pgx.Tx, raw json.RawMessage) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(raw, &invoice); err != nil || invoice.ID == "" {
		return errors.New("card billing: malformed invoice")
	}
	subscription := invoice.SubscriptionID()
	if subscription == "" {
		return nil
	}
	var o cardOrder
	err := tx.QueryRow(ctx, `SELECT o.id,o.one_time_minor,o.monthly_minor FROM portal_card_checkouts c JOIN portal_orders o ON o.id=c.order_id WHERE c.subscription_id=$1 AND o.collection='card'`, subscription).Scan(&o.ID, &o.OneTimeMinor, &o.MonthlyMinor)
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
	switch invoice.BillingReason {
	case "subscription_create":
		if o.OneTimeMinor == 0 {
			return nil
		}
		return settleCardCharge(ctx, tx, o.ID, "acceptance", invoice.ID, invoice.AmountPaid, o.OneTimeMinor, invoice.Currency)
	case "subscription_cycle":
		period := monthPeriod(time.Unix(invoice.Created, 0).In(copenhagen))
		return settleCardCharge(ctx, tx, o.ID, period, invoice.ID, invoice.AmountPaid, o.MonthlyMinor, invoice.Currency)
	}
	return nil
}
