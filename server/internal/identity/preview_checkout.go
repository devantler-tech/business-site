package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type previewPurchase struct {
	Acceptance                 string
	OneTimeMinor, MonthlyMinor int64
	Recorded                   time.Time
}

func (d previewPurchase) OneTimeDKK() int64    { return d.OneTimeMinor / 100 }
func (d previewPurchase) MonthlyDKK() int64    { return d.MonthlyMinor / 100 }
func (d previewPurchase) DiscountPercent() int { return 100 }
func (d previewPurchase) DueDKK() int64        { return 0 }
func (d previewPurchase) Simulated() bool      { return !d.Recorded.IsZero() }

func previewQuote(acceptance string, proposal projectProposal) *previewPurchase {
	return &previewPurchase{Acceptance: acceptance, OneTimeMinor: proposal.OneTimeMinor, MonthlyMinor: proposal.MonthlyMinor}
}

func currentPreviewPurchase(acceptances []proposalAcceptance) *previewPurchase {
	for _, a := range acceptances {
		if a.Current {
			return previewQuote(a.ID, a.Proposal)
		}
	}
	return nil
}

// recordPreviewPurchase uses the existing request and current actor locks. A
// retry for the same accepted proposal returns the receipt with no new effects.
// The point of sale never creates an actual order or reserves a public offer.
func (s *Foundation) recordPreviewPurchase(ctx context.Context, p Principal, id, acceptance string) error {
	if !s.operatorOnly || !p.isClientPreview() {
		return ErrDenied
	}
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	if client != previewWorkspace {
		return ErrDenied
	}
	accepted, _, err := proposalAcceptanceHistory(ctx, tx, id, client)
	if err != nil {
		return err
	}
	price := currentPreviewPurchase(accepted)
	if price == nil || price.Acceptance != acceptance {
		return errProposalConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_preview_receipts
 (acceptance_id,request_id,client_id,one_time_minor,monthly_minor)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(acceptance_id) DO NOTHING`,
		acceptance, id, client, price.OneTimeMinor, price.MonthlyMinor); err != nil {
		return errTriageStorage
	}
	return s.commitClientProposal(ctx, p, tx, time.Time{})
}

func (s *Foundation) readPreviewPurchase(ctx context.Context, p Principal, id string, accepted []proposalAcceptance) (*previewPurchase, error) {
	if !s.operatorOnly || !p.isClientPreview() {
		return nil, ErrDenied
	}
	quote := currentPreviewPurchase(accepted)
	if quote == nil {
		return nil, nil
	}
	err := s.pool.QueryRow(ctx, `SELECT one_time_minor,monthly_minor,recorded_at FROM portal_preview_receipts
 WHERE acceptance_id=$1 AND request_id=$2 AND client_id=$3`,
		quote.Acceptance, id, previewWorkspace).Scan(&quote.OneTimeMinor, &quote.MonthlyMinor, &quote.Recorded)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, errTriageStorage
	}
	if client, role, err := s.membership(ctx, p); err != nil || role != "client" || client != previewWorkspace {
		return nil, ErrDenied
	}
	return quote, nil
}

func previewPurchaseForm(r *http.Request) (string, bool) {
	if r.ParseForm() != nil {
		return "", false
	}
	for key, values := range r.Form {
		if len(values) != 1 || (key != "acceptance" && key != "csrf_token" && key != "lang") {
			return "", false
		}
	}
	acceptance := r.PostForm.Get("acceptance")
	return acceptance, opaqueRequestID.MatchString(acceptance)
}

func (b *browser) submitPreviewPurchase(w http.ResponseWriter, r *http.Request) {
	p := b.principal(r.Context())
	if !p.isClientPreview() {
		b.clientOrderResult(w, r, ErrDenied)
		return
	}
	if !b.foundation.acceptanceReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	acceptance, valid := previewPurchaseForm(r)
	if !valid {
		b.render(w, r, http.StatusBadRequest, "error", "", "")
		return
	}
	b.clientOrderResult(w, r, b.foundation.recordPreviewPurchase(r.Context(), p, r.PathValue("id"), acceptance))
}
