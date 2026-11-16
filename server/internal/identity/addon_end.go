package identity

import (
	"context"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// endOrderAddon ends one monthly add-on of the client's order after the
// current month, where they ordered it (Consumer Contracts Act, section 28 a).
// The add-on is billed through that month and no longer after. A card order's
// subscription then loses the add-on's item, without proration: the end is
// recorded first, so a lost Stripe answer leaves it to be asked again, and
// Stripe's confirmation is recorded on its own. The same add-on again is a
// retry, and asks Stripe again too.
func (s *Foundation) endOrderAddon(ctx context.Context, p Principal, id, orderID, addonID string) error {
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginClientProposalWrite, true)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	var addon *orderAddon
	for i := range o.Addons {
		if o.Addons[i].ID == addonID && o.Addons[i].Monthly() {
			addon = &o.Addons[i]
		}
	}
	switch {
	case addon == nil || o.End != nil:
		return errProposalConflict
	case addon.LastMonth != nil:
		if err = s.commitOrder(ctx, p, tx, s.pathReady(o.Collection)); err != nil || o.Collection != "card" {
			return err
		}
		return s.scheduleAddonRemovals(ctx, o.ID)
	case o.Collection == "card" && !s.cardBillingReady(ctx):
		// Never change a subscription the card path's own gate holds closed.
		return ErrDenied
	}
	last := addonLastMonth(o.Collection, time.Now())
	if _, err = tx.Exec(ctx, `INSERT INTO portal_addon_ends(order_id,addon_id,last_month,client_issuer,client_subject) VALUES($1,$2,$3,$4,$5)`, o.ID, addonID, last, p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	if o.Collection == "card" {
		// Locking the order's monthly Checkouts orders this against a Checkout
		// completing: either its subscription is visible once this commits, or
		// its completion sees this end and removes the add-on itself.
		if _, err = tx.Exec(ctx, `SELECT 1 FROM portal_card_checkouts WHERE order_id=$1 AND purpose='monthly' FOR UPDATE`, o.ID); err != nil {
			return errTriageStorage
		}
	}
	if err = s.commitOrder(ctx, p, tx, s.pathReady(o.Collection)); err != nil || o.Collection != "card" {
		return err
	}
	return s.scheduleAddonRemovals(ctx, o.ID)
}

// addonLastMonth is the last billed month of a monthly add-on ended at now:
// Copenhagen's month for an invoice order; for a card order the UTC month,
// since Stripe renews at midnight UTC on the 1st. The receipt offers, the end
// records and the client's level ends with this same month.
func addonLastMonth(collection string, now time.Time) time.Time {
	if collection == "card" {
		now = now.UTC()
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return currentMonth(now)
}

// scheduleAddonRemovals asks Stripe to remove every ended add-on of the order
// that its subscription may still charge, and records each confirmation. A
// failure leaves the end recorded: the operator's page offers to ask again.
func (s *Foundation) scheduleAddonRemovals(ctx context.Context, order string) error {
	if !s.cardBillingReady(ctx) {
		return ErrDenied
	}
	rows, err := s.pool.Query(ctx, `SELECT c.subscription_id,e.addon_id FROM portal_addon_ends e JOIN portal_card_checkouts c ON c.order_id=e.order_id AND c.purpose='monthly' AND c.subscription_id IS NOT NULL
 WHERE e.order_id=$1 AND NOT EXISTS (SELECT 1 FROM portal_addon_removals r WHERE r.order_id=e.order_id AND r.addon_id=e.addon_id)`, order)
	if err != nil {
		return errTriageStorage
	}
	pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[struct{ Subscription, Addon string }])
	if err != nil {
		return errTriageStorage
	}
	for _, p := range pending {
		if s.cards.Load().Stripe.RemoveSubscriptionAddon(ctx, p.Subscription, p.Addon) != nil {
			log.Print("portal: card add-on removal not confirmed")
			continue
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO portal_addon_removals(order_id,addon_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, order, p.Addon); err != nil {
			return errTriageStorage
		}
	}
	return nil
}

// retryAddonRemovals is the operator's form that asks Stripe again.
func (b *browser) retryAddonRemovals(w http.ResponseWriter, r *http.Request) {
	if !onlyFields(r, "order") || !opaqueRequestID.MatchString(r.PostForm.Get("order")) {
		b.render(w, r, 400, "error", "", "")
		return
	}
	b.proposalMutationResult(w, r, b.foundation.operatorAddonRemovals(r.Context(), b.principal(r.Context()), r.PathValue("id"), r.PostForm.Get("order")), "error")
}

// operatorAddonRemovals asks Stripe again for a current operator.
func (s *Foundation) operatorAddonRemovals(ctx context.Context, p Principal, id, orderID string) error {
	tx, o, err := s.orderWrite(ctx, p, id, orderID, s.beginOperatorWrite, false)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if err = commitProposalWrite(ctx, p, tx, s.orderReady); err != nil {
		return err
	}
	return s.scheduleAddonRemovals(ctx, o.ID)
}

// endAddon serves the receipt's form that ends one monthly add-on.
func (b *browser) endAddon(w http.ResponseWriter, r *http.Request) {
	if !onlyFields(r, "order", "addon") {
		b.render(w, r, 400, "error", "", "")
		return
	}
	order, addon := r.PostForm.Get("order"), r.PostForm.Get("addon")
	if !opaqueRequestID.MatchString(order) || !addonID.MatchString(addon) {
		b.render(w, r, 400, "error", "", "")
		return
	}
	b.clientOrderResult(w, r, b.foundation.endOrderAddon(r.Context(), b.principal(r.Context()), r.PathValue("id"), order, addon))
}

// onlyFields parses a form that carries the CSRF token, the language and
// exactly one of each named field, and nothing else.
func onlyFields(r *http.Request, names ...string) bool {
	if r.ParseForm() != nil {
		return false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 || name != "csrf_token" && name != "lang" && !slices.Contains(names, name) {
			return false
		}
	}
	return true
}
