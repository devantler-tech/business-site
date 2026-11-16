package identity

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// endOrderAddon ends one monthly add-on of the client's order after the
// current month, where they ordered it (Consumer Contracts Act, section 28 a).
// The add-on is billed through that month and no longer after. A card order's
// subscription first loses the add-on's item, without proration, so the
// end is recorded only once Stripe no longer charges it; a retry converges.
// The same add-on again is a retry.
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
		return s.commitOrder(ctx, p, tx, s.pathReady(o.Collection))
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_addon_ends(order_id,addon_id,last_month,client_issuer,client_subject) VALUES($1,$2,$3,$4,$5)`, o.ID, addonID, currentMonth(time.Now()), p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	if o.Collection == "card" {
		var subscription string
		err = tx.QueryRow(ctx, `SELECT subscription_id FROM portal_card_checkouts WHERE order_id=$1 AND purpose='monthly' AND subscription_id IS NOT NULL`, o.ID).Scan(&subscription)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// No subscription yet: its checkout leaves the ended add-on out.
		case err != nil:
			return errTriageStorage
		default:
			cfg := s.cards.Load()
			if cfg == nil || cfg.Stripe.RemoveSubscriptionAddon(ctx, subscription, addonID) != nil {
				return errors.New("card billing: the add-on was not removed from the subscription")
			}
		}
	}
	return s.commitOrder(ctx, p, tx, s.pathReady(o.Collection))
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
