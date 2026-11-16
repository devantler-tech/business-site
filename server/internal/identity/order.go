package identity

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

// copenhagen dates billing periods and invoices in the business's own time zone.
var copenhagen = mustLocation("Europe/Copenhagen")

func mustLocation(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		panic("identity: time zone unavailable: " + name)
	}
	return location
}

// projectOrder is an accepted proposal with its one collection path (ADR 0030).
type projectOrder struct {
	ID, Acceptance, Collection string
	Version                    int
	OneTimeMinor, MonthlyMinor int64
	MonthlyFrom, Placed        time.Time
	Charges                    []orderCharge
	End                        *orderEnd
	Subscribed                 bool       // a card order's monthly subscription exists
	Withdrawn                  *time.Time // when the client withdrew from the order
	EarlyStart                 *time.Time // the client's request to start within the withdrawal period
	Completed                  *time.Time // the operator's record that the work is completely finished
	Owed, Paid                 *int64     // a withdrawal's amount owed for work done, and what was paid
	Mail                       []orderMailState
	WithdrawalEnd              *time.Time   // when Stripe ends a withdrawn order's subscription
	Launch                     *launchPlace // the launch place whose discount the one-time price includes
	Addons                     []orderAddon // the standard add-ons the prices include
}

// BaseOneTimeDKK and BaseMonthlyDKK are the order's prices without its add-ons.
func (o projectOrder) BaseOneTimeDKK() int64 {
	one, _ := addonTotals(o.Addons)
	return (o.OneTimeMinor - one) / 100
}
func (o projectOrder) BaseMonthlyDKK() int64 {
	_, monthly := addonTotals(o.Addons)
	return (o.MonthlyMinor - monthly) / 100
}

func (o projectOrder) OneTimeDKK() int64 { return o.OneTimeMinor / 100 }
func (o projectOrder) MonthlyDKK() int64 { return o.MonthlyMinor / 100 }

// MonthlyStart is the first billed month, as YYYY-MM.
func (o projectOrder) MonthlyStart() string { return o.MonthlyFrom.Format("2006-01") }

// orderCharge is one billing period's funding record.
type orderCharge struct {
	Period, State, Reference, Identity string
	Number                             *int
	GrossMinor                         int64
	Sent                               bool
	Reversal                           string // refunded or disputed: no longer funds the order
}

func (c orderCharge) GrossDKK() int64 { return c.GrossMinor / 100 }

// invoiceBillingReady is the false-default gate for the invoice collection path:
// order placement, order display and the Numina sync. It has no environment mapping.
func (s *Foundation) invoiceBillingReady(ctx context.Context) bool {
	if !s.acceptanceReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-invoice-billing", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx)
}

// orderReady opens ordering and order display when either collection path is on.
func (s *Foundation) orderReady(ctx context.Context) bool {
	return s.invoiceBillingReady(ctx) || s.cardBillingReady(ctx)
}

// pathReady binds collectionReady to one path for a final checkpoint.
func (s *Foundation) pathReady(collection string) func(context.Context) bool {
	return func(ctx context.Context) bool { return s.collectionReady(ctx, collection) }
}

// collectionReady is the gate for one collection path.
func (s *Foundation) collectionReady(ctx context.Context, collection string) bool {
	switch collection {
	case "invoice":
		return s.invoiceBillingReady(ctx)
	case "card":
		return s.cardBillingReady(ctx)
	}
	return false
}

// firstOfNextMonth is where monthly billing starts for an order placed at t.
func firstOfNextMonth(t time.Time) time.Time {
	local := t.In(copenhagen)
	return time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// placeOrder binds the client's current acceptance to one collection path. An
// identical retry returns the existing order; a different path conflicts.
func (s *Foundation) placeOrder(ctx context.Context, p Principal, id, acceptance, collection string, earlyStart, launch bool, addonValues []string, mc mailContext) (projectOrder, error) {
	if !s.collectionReady(ctx, collection) {
		return projectOrder{}, ErrDenied
	}
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return projectOrder{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	history, _, err := proposalAcceptanceHistory(ctx, tx, id, client)
	if err != nil {
		return projectOrder{}, err
	}
	var current *proposalAcceptance
	for i := range history {
		if history[i].ID == acceptance && history[i].Current {
			current = &history[i]
		}
	}
	// Add-ons must exist for the request's offering; monthly ones need hosting.
	existing, err := readOrderTx(ctx, tx, id, client)
	switch {
	case err == nil:
		// A retry names the order's own add-ons; it keeps their stored prices,
		// whatever the catalogue says now.
		if existing.Acceptance != acceptance || existing.Collection != collection || (existing.EarlyStart != nil) != earlyStart || (existing.Launch != nil) != launch || !sameAddons(existing.Addons, addonValues) {
			return projectOrder{}, errProposalConflict
		}
		return existing, s.commitOrder(ctx, p, tx, s.pathReady(collection))
	case !errors.Is(err, pgx.ErrNoRows):
		return projectOrder{}, err
	}
	if current == nil || (current.Proposal.OneTimeMinor == 0 && current.Proposal.MonthlyMinor == 0) {
		return projectOrder{}, errProposalConflict
	}
	// A new order's add-ons must exist for the request's offering; monthly ones
	// need hosting.
	var product string
	if err = tx.QueryRow(ctx, `SELECT product FROM portal_requests WHERE id=$1 AND client_id=$2`, id, client).Scan(&product); err != nil {
		return projectOrder{}, errTriageStorage
	}
	addons, valid := parseAddons(addonValues, product, current.Proposal.MonthlyMinor > 0)
	if !valid {
		return projectOrder{}, errProposalConflict
	}
	// The order takes the client's launch place, if one is open, at the price
	// the client was shown; if the last place went in between, the client
	// sees the new price before ordering again.
	place, err := lockLaunchPlace(ctx, tx, client, current.Proposal.OneTimeMinor, current.Proposal.MonthlyMinor)
	if err != nil {
		return projectOrder{}, err
	}
	if (place != nil) != launch {
		return projectOrder{}, errLaunchChanged
	}
	// The launch discount takes off only the proposal's own one-time price.
	oneTime, monthly := addonTotals(addons)
	oneTime += current.Proposal.OneTimeMinor
	monthly += current.Proposal.MonthlyMinor
	if place != nil {
		oneTime -= place.DiscountMinor
	}
	// One timestamp dates the order and its first billed month.
	placed := time.Now()
	// The client's express request to start within the withdrawal period is
	// dated with the order itself.
	var early *time.Time
	if earlyStart {
		early = &placed
	}
	orderID := secret()
	if _, err = tx.Exec(ctx, `INSERT INTO portal_orders(id,request_id,client_id,acceptance_id,client_issuer,client_subject,collection,one_time_minor,monthly_minor,monthly_from,placed_at,early_start_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		orderID, id, client, current.ID, p.issuer, p.subject, collection, oneTime, monthly, firstOfNextMonth(placed), placed, early); err != nil {
		return projectOrder{}, errTriageStorage
	}
	for _, a := range addons {
		if _, err = tx.Exec(ctx, `INSERT INTO portal_order_addons(order_id,addon_id,kind,quantity,unit_minor) VALUES($1,$2,$3,$4,$5)`, orderID, a.ID, a.Kind, a.Quantity, a.UnitMinor); err != nil {
			return projectOrder{}, errTriageStorage
		}
	}
	if place != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO portal_launch_places(place,client_id,order_id,percent,list_minor,discount_minor) VALUES($1,$2,$3,$4,$5,$6)`, place.Place, client, orderID, place.Percent, place.ListMinor, place.DiscountMinor); err != nil {
			return projectOrder{}, errTriageStorage
		}
	}
	// The order's confirmation on a durable medium (section 13), queued with it.
	when := projectOrder{Placed: placed, OneTimeMinor: oneTime, MonthlyMinor: monthly, Addons: addons}
	if err = s.queueOrderMail(ctx, tx, client, "order-confirmation", mc, orderMail{OrderID: orderID, Scope: current.Proposal.Scope, Deliverables: current.Proposal.Deliverables,
		OneTimeTerms: current.Proposal.OneTimeTerms, MonthlyTerms: current.Proposal.MonthlyTerms, Collection: collection,
		OneTimeDKK: oneTime / 100, BaseOneTimeDKK: when.BaseOneTimeDKK(), Launch: place, MonthlyDKK: monthly / 100, BaseMonthlyDKK: when.BaseMonthlyDKK(), Addons: addons, MonthlyStart: firstOfNextMonth(placed).Format("2006-01"),
		WithdrawUntil: when.WithdrawUntil(), WorkStartsFrom: when.WorkStartsFrom(), Placed: placed.UTC().Format("2006-01-02 15:04"), EarlyStart: earlyStart}); err != nil {
		return projectOrder{}, err
	}
	saved, err := readOrderTx(ctx, tx, id, client)
	if err != nil {
		return projectOrder{}, errTriageStorage
	}
	return saved, s.commitOrder(ctx, p, tx, s.pathReady(collection))
}

// readOrderTx reads the request's order and its charges inside a scoped,
// request-locked transaction. It grants no authority itself.
func readOrderTx(ctx context.Context, tx pgx.Tx, id, client string) (projectOrder, error) {
	var o projectOrder
	var place *int
	var l launchPlace
	err := tx.QueryRow(ctx, `SELECT o.id,o.acceptance_id,o.collection,a.proposal_version,o.one_time_minor,o.monthly_minor,o.monthly_from,o.placed_at,o.early_start_at,(SELECT x.completed_at FROM portal_order_completions x WHERE x.order_id=o.id),l.place,coalesce(l.percent,0),coalesce(l.list_minor,0),coalesce(l.discount_minor,0) FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id LEFT JOIN portal_launch_places l ON l.order_id=o.id WHERE o.request_id=$1 AND o.client_id=$2`, id, client).
		Scan(&o.ID, &o.Acceptance, &o.Collection, &o.Version, &o.OneTimeMinor, &o.MonthlyMinor, &o.MonthlyFrom, &o.Placed, &o.EarlyStart, &o.Completed, &place, &l.Percent, &l.ListMinor, &l.DiscountMinor)
	if place != nil {
		l.Place = *place
		o.Launch = &l
	}
	if err == nil {
		o.Addons, err = readOrderAddons(ctx, tx, o.ID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return projectOrder{}, err
	}
	if err != nil {
		return projectOrder{}, errTriageStorage
	}
	rows, err := tx.Query(ctx, `SELECT c.period,c.state,coalesce(c.provider_ref,''),c.identity,c.provider_number,c.gross_minor,c.sent,coalesce((SELECT min(r.reason) FROM portal_charge_reversals r WHERE r.order_id=c.order_id AND r.period=c.period),'') FROM portal_order_charges c WHERE c.order_id=$1 ORDER BY c.period='acceptance' DESC,c.period DESC LIMIT 120`, o.ID)
	if err != nil {
		return projectOrder{}, errTriageStorage
	}
	defer rows.Close()
	for rows.Next() {
		var c orderCharge
		if err = rows.Scan(&c.Period, &c.State, &c.Reference, &c.Identity, &c.Number, &c.GrossMinor, &c.Sent, &c.Reversal); err != nil {
			return projectOrder{}, errTriageStorage
		}
		o.Charges = append(o.Charges, c)
	}
	if rows.Err() != nil {
		return projectOrder{}, errTriageStorage
	}
	rows.Close()
	if err = readOrderEnd(ctx, tx, &o); err != nil {
		return projectOrder{}, err
	}
	if err = readWithdrawal(ctx, tx, &o); err != nil {
		return projectOrder{}, err
	}
	if err = readOrderMail(ctx, tx, &o); err != nil {
		return projectOrder{}, err
	}
	return o, nil
}

// readClientOrder returns the client's own order, or nil when none is placed.
func (s *Foundation) readClientOrder(ctx context.Context, p Principal, id string) (*projectOrder, error) {
	if !s.orderReady(ctx) {
		return nil, ErrDenied
	}
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return readOrderFor(ctx, tx, id, client, func() error { return s.commitOrder(ctx, p, tx, s.orderReady) })
}

// readOperatorOrder returns a request's order for a current operator.
func (s *Foundation) readOperatorOrder(ctx context.Context, p Principal, id string) (*projectOrder, error) {
	if !s.orderReady(ctx) || s.proposalOperator(ctx, p) != nil {
		return nil, ErrDenied
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return nil, err
	}
	return readOrderFor(ctx, tx, id, client, func() error { return commitProposalWrite(ctx, p, tx, s.orderReady) })
}

// commitOrder is the final checkpoint for order writes and reads: the billing
// gate, not only the acceptance gates, must still be open, along with the
// principal's expiry and the request's cancellation.
func (s *Foundation) commitOrder(ctx context.Context, p Principal, tx pgx.Tx, ready func(context.Context) bool) error {
	if !ready(ctx) {
		return ErrDenied
	}
	return s.commitClientProposal(ctx, p, tx, time.Time{})
}

// readOrderFor reads the order in an already scoped, request-locked transaction
// and finishes through the caller's own final authorization checkpoint.
func readOrderFor(ctx context.Context, tx pgx.Tx, id, client string, commit func() error) (*projectOrder, error) {
	defer rollbackProposalWrite(ctx, tx)
	o, err := readOrderTx(ctx, tx, id, client)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, commit()
	}
	if err != nil {
		return nil, err
	}
	return &o, commit()
}

// parseOrderForm admits only the acceptance, the chosen path and form plumbing.
func parseOrderForm(r *http.Request) (acceptance, collection string, earlyStart, launch, valid bool) {
	if err := r.ParseForm(); err != nil {
		return "", "", false, false, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 && (name != "addon" || len(values) > len(offerings.Addons)) {
			return "", "", false, false, false
		}
		switch name {
		case "csrf_token", "lang", "acceptance", "collection", "addon":
		case "early_start", "launch":
			// The optional request, never ticked in advance, to start within the
			// withdrawal period, and the launch discount the client was shown.
			if values[0] != "yes" {
				return "", "", false, false, false
			}
		default:
			return "", "", false, false, false
		}
	}
	acceptance, collection = r.PostForm.Get("acceptance"), r.PostForm.Get("collection")
	return acceptance, collection, r.PostForm.Has("early_start"), r.PostForm.Has("launch"), opaqueRequestID.MatchString(acceptance) && collection != ""
}

// submitOrder places the client's order and returns to the receipt.
func (b *browser) submitOrder(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.orderReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	acceptance, collection, earlyStart, launch, valid := parseOrderForm(r)
	if !valid {
		b.render(w, r, 400, "error", "", "")
		return
	}
	order, err := b.foundation.placeOrder(r.Context(), b.principal(r.Context()), r.PathValue("id"), acceptance, collection, earlyStart, launch, r.PostForm["addon"], mailContext{Lang: language(r), Portal: b.origin + b.base()})
	if err == nil && order.Collection == "card" {
		// The receipt offers to start the payment again if this fails.
		if checkoutErr := b.foundation.ensureCheckouts(r.Context(), order.ID, b.origin+b.base(), language(r)); checkoutErr != nil {
			log.Printf("portal: card checkout not created")
		}
	}
	b.clientOrderResult(w, r, err)
}

// clientOrderResult answers a client's order form and returns to the receipt.
func (b *browser) clientOrderResult(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errLaunchChanged):
		// The receipt shows the order form again at the new price.
		http.Redirect(w, r, "/portal/requests/"+r.PathValue("id")+"?lang="+language(r)+"&launch=ended", http.StatusSeeOther)
	case errors.Is(err, errProposalConflict):
		b.render(w, r, 409, "error", "", "")
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
	case errors.Is(err, ErrDenied):
		b.render(w, r, 403, "error", "", "")
	case err != nil:
		b.render(w, r, 503, "unavailable", "", "")
	default:
		http.Redirect(w, r, "/portal/requests/"+r.PathValue("id")+"?lang="+language(r), http.StatusSeeOther)
	}
}
