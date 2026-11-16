package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// fairUseLock serializes one client's new requests, so two at once cannot both
// pass the fair-use check. It is the first key of a two-key advisory lock whose
// second key is the client.
const fairUseLock = 1869820523

// errRequestInProgress refuses a hosting client's second request in progress.
var errRequestInProgress = errors.New("a request is already in progress")

// billsMonthly is the condition that order o of client $1 still bills monthly: it has a
// monthly price, no withdrawal, and no end before this month.
const billsMonthly = `o.client_id=$1 AND o.monthly_minor>0
 AND NOT EXISTS (SELECT 1 FROM portal_order_withdrawals w WHERE w.order_id=o.id)
 AND NOT EXISTS (SELECT 1 FROM portal_order_ends e WHERE e.order_id=o.id AND e.last_month<date_trunc('month',now() AT TIME ZONE 'Europe/Copenhagen')::date)`

// featureState is how the operator's page shows a client's feature requests:
// the client's priority level and whether this request is finished.
type featureState struct {
	Level   string // standard, priority or express
	Hosting bool
	Closed  *time.Time
}

// featureLevel is the client's feature-request priority: the highest tier
// among the monthly add-ons of their orders that still bill, else standard.
func featureLevel(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, client string) (string, error) {
	rows, err := q.Query(ctx, `SELECT a.addon_id FROM portal_order_addons a JOIN portal_orders o ON o.id=a.order_id WHERE a.kind='monthly' AND `+billsMonthly, client)
	if err != nil {
		return "", errTriageStorage
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", errTriageStorage
	}
	level := "standard"
	for _, id := range ids {
		if a, ok := findAddon(id); ok && (a.Tier == "express" || a.Tier == "priority" && level == "standard") {
			level = a.Tier
		}
	}
	return level, nil
}

// requestBusy reports, under the client's fair-use lock, whether a client with
// hosting already has a request in progress: one the operator has not marked
// finished, other than the retry of this submission.
func requestBusy(ctx context.Context, tx pgx.Tx, client, key string) (bool, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int,hashtext($2))`, fairUseLock, client); err != nil {
		return false, errTriageStorage
	}
	var busy bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_orders o WHERE `+billsMonthly+`)
 AND EXISTS (SELECT 1 FROM portal_requests q WHERE q.client_id=$1 AND q.submission_key<>$2
   AND NOT EXISTS (SELECT 1 FROM portal_request_closures c WHERE c.request_id=q.id))`, client, key).Scan(&busy)
	if err != nil {
		return false, errTriageStorage
	}
	return busy, nil
}

// readFeature reads, in the operator's request transaction, the client's
// feature-request level and whether this request is finished.
func readFeature(ctx context.Context, tx pgx.Tx, client, id string) (*featureState, error) {
	f := &featureState{}
	var err error
	if f.Level, err = featureLevel(ctx, tx, client); err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_orders o WHERE `+billsMonthly+`),(SELECT closed_at FROM portal_request_closures WHERE request_id=$2)`, client, id).Scan(&f.Hosting, &f.Closed); err != nil {
		return nil, errTriageStorage
	}
	return f, nil
}

// closeRequest records once that the operator finished the request or that it
// will not go ahead, so a hosting client can send the next one.
func (s *Foundation) closeRequest(ctx context.Context, p Principal, id string) error {
	if !s.orderReady(ctx) {
		return ErrDenied
	}
	tx, client, err := s.beginOperatorWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if _, err = tx.Exec(ctx, `INSERT INTO portal_request_closures(request_id,client_id,operator_issuer,operator_subject) VALUES($1,$2,$3,$4) ON CONFLICT(request_id) DO NOTHING`, id, client, p.issuer, p.subject); err != nil {
		return errTriageStorage
	}
	return commitProposalWrite(ctx, p, tx, s.orderReady)
}

// finishRequest is the operator's form to mark a request finished.
func (b *browser) finishRequest(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		b.render(w, r, 400, "error", "", "")
		return
	}
	for name, values := range r.PostForm {
		if len(values) != 1 || name != "csrf_token" && name != "lang" {
			b.render(w, r, 400, "error", "", "")
			return
		}
	}
	b.proposalMutationResult(w, r, b.foundation.closeRequest(r.Context(), b.principal(r.Context()), r.PathValue("id")), "error")
}
