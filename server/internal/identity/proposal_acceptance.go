package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

type proposalReview struct {
	Key, Hash string
	Event     int
	Expires   time.Time
	Proposal  projectProposal
}

type proposalAcceptance struct {
	ID       string
	Event    int
	Recorded time.Time
	Current  bool
	Proposal projectProposal
}

// acceptanceReady requires the independent consent gate and every sharing prerequisite.
func (s *Foundation) acceptanceReady(ctx context.Context) bool {
	if !s.sharingReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-proposal-acceptance", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && s.ready(ctx)
}

// proposalContentHash binds the typed, normalized stored content, not browser
// assertions or ambiguous delimiter concatenation. It is not a legal signature.
func proposalContentHash(d projectProposal) (string, error) {
	content := struct {
		Scope, Deliverables, Currency, OneTimeTerms, MonthlyTerms string
		OneTimeMinor, MonthlyMinor                                int64
	}{d.Scope, d.Deliverables, d.Currency, d.OneTimeTerms, d.MonthlyTerms, d.OneTimeMinor, d.MonthlyMinor}
	data, err := json.Marshal(content)
	if err != nil {
		return "", errTriageStorage
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// beginClientProposalWrite locks the request before current client membership,
// independently of the operator-only writer. Rebinding while waiting denies.
func (s *Foundation) beginClientProposalWrite(ctx context.Context, p Principal, id string) (pgx.Tx, string, error) {
	if !s.acceptanceReady(ctx) {
		return nil, "", ErrDenied
	}
	bound, err := s.requestClient(ctx, p)
	if err != nil {
		return nil, "", ErrDenied
	}
	if !opaqueRequestID.MatchString(id) {
		return nil, "", pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", errTriageStorage
	}
	handedOff := false
	defer func() {
		if !handedOff {
			rollbackProposalWrite(ctx, tx)
		}
	}()
	if err = lockProposalRequest(ctx, tx, id); err != nil {
		return nil, "", err
	}
	var client string
	if err = tx.QueryRow(ctx, `SELECT client_id FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked FOR SHARE`, p.issuer, p.subject).Scan(&client); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrDenied
		}
		return nil, "", errTriageStorage
	}
	if client != bound || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return nil, "", ErrDenied
	}
	var owned string
	if err = tx.QueryRow(ctx, `SELECT id FROM portal_requests WHERE id=$1 AND client_id=$2`, id, client).Scan(&owned); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", pgx.ErrNoRows
		}
		return nil, "", errTriageStorage
	}
	handedOff = true
	return tx, client, nil
}

// currentProposalReview returns no unshared contents or draft number on conflict.
func currentProposalReview(ctx context.Context, tx pgx.Tx, id, client string) (proposalReview, error) {
	var review proposalReview
	var latest, selected int
	var action string
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT max(version) FROM portal_proposals WHERE request_id=$1 AND client_id=$2),0),COALESCE((SELECT event FROM portal_proposal_visibility WHERE request_id=$1 AND client_id=$2 ORDER BY event DESC LIMIT 1),0),COALESCE((SELECT proposal_version FROM portal_proposal_visibility WHERE request_id=$1 AND client_id=$2 ORDER BY event DESC LIMIT 1),0),COALESCE((SELECT action FROM portal_proposal_visibility WHERE request_id=$1 AND client_id=$2 ORDER BY event DESC LIMIT 1),'')`, id, client).Scan(&latest, &review.Event, &selected, &action); err != nil {
		return proposalReview{}, errTriageStorage
	}
	if latest == 0 || latest != selected || action != "share" {
		return proposalReview{}, errProposalConflict
	}
	d := &review.Proposal
	if err := tx.QueryRow(ctx, `SELECT version,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms,prepared_at FROM portal_proposals WHERE request_id=$1 AND client_id=$2 AND version=$3`, id, client, selected).Scan(&d.Version, &d.Scope, &d.Deliverables, &d.Currency, &d.OneTimeMinor, &d.MonthlyMinor, &d.OneTimeTerms, &d.MonthlyTerms, &d.Prepared); err != nil {
		return proposalReview{}, errTriageStorage
	}
	hash, err := proposalContentHash(*d)
	if err != nil {
		return proposalReview{}, err
	}
	review.Hash = hash
	return review, nil
}

func (s *Foundation) commitClientProposal(ctx context.Context, p Principal, tx pgx.Tx, deadline time.Time) error {
	if !s.acceptanceReady(ctx) || ctx.Err() != nil || !time.Now().Before(p.expires) || (!deadline.IsZero() && !time.Now().Before(deadline)) {
		return ErrDenied
	}
	if err := tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}

// prepareProposalReview mutates only ephemeral review state through an explicit
// POST. A live matching context retains its original, non-extendable deadline.
func (s *Foundation) prepareProposalReview(ctx context.Context, p Principal, id string) (proposalReview, error) {
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return proposalReview{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	review, err := currentProposalReview(ctx, tx, id, client)
	if err != nil {
		return proposalReview{}, err
	}
	var accepted bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_proposal_acceptances WHERE request_id=$1 AND client_id=$2 AND proposal_version=$3)`, id, client, review.Proposal.Version).Scan(&accepted); err != nil {
		return proposalReview{}, errTriageStorage
	}
	if accepted {
		return proposalReview{}, errProposalConflict
	}
	var saved proposalReview
	err = tx.QueryRow(ctx, `SELECT context_key,content_hash,visibility_event,proposal_version,expires_at FROM portal_proposal_reviews WHERE request_id=$1 AND client_id=$2 AND client_issuer=$3 AND client_subject=$4`, id, client, p.issuer, p.subject).Scan(&saved.Key, &saved.Hash, &saved.Event, &saved.Proposal.Version, &saved.Expires)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return proposalReview{}, errTriageStorage
	}
	if err == nil && saved.Hash == review.Hash && saved.Event == review.Event && saved.Proposal.Version == review.Proposal.Version && time.Now().Before(saved.Expires) {
		review.Key, review.Expires = saved.Key, saved.Expires
	} else {
		review.Key, review.Expires = secret(), time.Now().Add(10*time.Minute)
		if p.expires.Before(review.Expires) {
			review.Expires = p.expires
		}
		// PostgreSQL stores microseconds. Return its actual deadline so a reuse
		// reads precisely the same value, without rounding the deadline upward.
		if err = tx.QueryRow(ctx, `INSERT INTO portal_proposal_reviews(request_id,client_id,client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(request_id,client_id,client_issuer,client_subject) DO UPDATE SET context_key=EXCLUDED.context_key,proposal_version=EXCLUDED.proposal_version,visibility_event=EXCLUDED.visibility_event,content_hash=EXCLUDED.content_hash,expires_at=EXCLUDED.expires_at RETURNING expires_at`, id, client, p.issuer, p.subject, review.Key, review.Proposal.Version, review.Event, review.Hash, review.Expires).Scan(&review.Expires); err != nil {
			return proposalReview{}, errTriageStorage
		}
	}
	if err = s.commitClientProposal(ctx, p, tx, review.Expires); err != nil {
		return proposalReview{}, err
	}
	return review, nil
}

// acceptProposal records one exact decision. Original actor-bound retries return
// history after current access checks, never authorize a replacement proposal.
func (s *Foundation) acceptProposal(ctx context.Context, p Principal, id, key string) (proposalAcceptance, error) {
	if !opaqueRequestID.MatchString(key) {
		return proposalAcceptance{}, errProposalConflict
	}
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return proposalAcceptance{}, err
	}
	defer rollbackProposalWrite(ctx, tx)
	var receipt proposalAcceptance
	var issuer, subject string
	err = tx.QueryRow(ctx, `SELECT id,proposal_version,recorded_at,client_issuer,client_subject FROM portal_proposal_acceptances WHERE request_id=$1 AND client_id=$2 AND context_key=$3`, id, client, key).Scan(&receipt.ID, &receipt.Proposal.Version, &receipt.Recorded, &issuer, &subject)
	if err == nil {
		if issuer != p.issuer || subject != p.subject {
			return proposalAcceptance{}, errProposalConflict
		}
		if err = s.commitClientProposal(ctx, p, tx, time.Time{}); err != nil {
			return proposalAcceptance{}, err
		}
		return receipt, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return proposalAcceptance{}, errTriageStorage
	}
	var saved proposalReview
	err = tx.QueryRow(ctx, `SELECT content_hash,visibility_event,proposal_version,expires_at FROM portal_proposal_reviews WHERE request_id=$1 AND client_id=$2 AND client_issuer=$3 AND client_subject=$4 AND context_key=$5`, id, client, p.issuer, p.subject, key).Scan(&saved.Hash, &saved.Event, &saved.Proposal.Version, &saved.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return proposalAcceptance{}, errProposalConflict
	}
	if err != nil {
		return proposalAcceptance{}, errTriageStorage
	}
	review, err := currentProposalReview(ctx, tx, id, client)
	if err != nil {
		return proposalAcceptance{}, err
	}
	if !time.Now().Before(saved.Expires) || saved.Hash != review.Hash || saved.Event != review.Event || saved.Proposal.Version != review.Proposal.Version {
		return proposalAcceptance{}, errProposalConflict
	}
	err = tx.QueryRow(ctx, `INSERT INTO portal_proposal_acceptances(id,request_id,client_id,client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(request_id,client_id,proposal_version) DO NOTHING RETURNING id,recorded_at`, secret(), id, client, p.issuer, p.subject, key, review.Proposal.Version, review.Event, review.Hash).Scan(&receipt.ID, &receipt.Recorded)
	if errors.Is(err, pgx.ErrNoRows) {
		return proposalAcceptance{}, errProposalConflict
	}
	if err != nil {
		return proposalAcceptance{}, errTriageStorage
	}
	if err = s.commitClientProposal(ctx, p, tx, saved.Expires); err != nil {
		return proposalAcceptance{}, err
	}
	receipt.Proposal, receipt.Current = review.Proposal, true
	return receipt, nil
}

// readProposalAcceptances exposes the client's bounded immutable receipts, never
// actor/retry/context metadata or unpublished versions. GET writes no session.
func (s *Foundation) readProposalAcceptances(ctx context.Context, p Principal, id string) ([]proposalAcceptance, bool, error) {
	tx, client, err := s.beginClientProposalWrite(ctx, p, id)
	if err != nil {
		return nil, false, err
	}
	defer rollbackProposalWrite(ctx, tx)
	result, canAccept, err := proposalAcceptanceHistory(ctx, tx, id, client)
	if err != nil {
		return nil, false, err
	}
	if err = s.commitClientProposal(ctx, p, tx, time.Time{}); err != nil {
		return nil, false, err
	}
	return result, canAccept, nil
}

// proposalAcceptanceHistory reads canonical receipts inside an already scoped,
// request-locked transaction. It grants neither client nor operator authority.
func proposalAcceptanceHistory(ctx context.Context, tx pgx.Tx, id, client string) ([]proposalAcceptance, bool, error) {
	current, stateErr := currentProposalReview(ctx, tx, id, client)
	if stateErr != nil && !errors.Is(stateErr, errProposalConflict) {
		return nil, false, stateErr
	}
	rows, err := tx.Query(ctx, `SELECT a.id,a.proposal_version,a.visibility_event,a.recorded_at,p.scope,p.deliverables,p.currency,p.one_time_minor,p.monthly_minor,p.one_time_terms,p.monthly_terms FROM portal_proposal_acceptances a JOIN portal_proposals p ON p.request_id=a.request_id AND p.client_id=a.client_id AND p.version=a.proposal_version WHERE a.request_id=$1 AND a.client_id=$2 ORDER BY a.proposal_version DESC LIMIT 50`, id, client)
	if err != nil {
		return nil, false, errTriageStorage
	}
	var result []proposalAcceptance
	acceptedCurrent := false
	for rows.Next() {
		var d proposalAcceptance
		q := &d.Proposal
		if err = rows.Scan(&d.ID, &q.Version, &d.Event, &d.Recorded, &q.Scope, &q.Deliverables, &q.Currency, &q.OneTimeMinor, &q.MonthlyMinor, &q.OneTimeTerms, &q.MonthlyTerms); err != nil {
			rows.Close()
			return nil, false, errTriageStorage
		}
		d.Current = stateErr == nil && d.Event == current.Event && q.Version == current.Proposal.Version
		acceptedCurrent = acceptedCurrent || d.Current
		result = append(result, d)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, false, errTriageStorage
	}
	return result, stateErr == nil && !acceptedCurrent, nil
}

// parseAcceptanceForm admits only native confirmation fields, never scope or prices.
func parseAcceptanceForm(r *http.Request, confirmation bool) (string, bool) {
	if err := r.ParseForm(); err != nil {
		return "", false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 || (name != "csrf_token" && name != "lang" && (!confirmation || (name != "context_key" && name != "confirm"))) {
			return "", false
		}
	}
	key := r.PostForm.Get("context_key")
	return key, !confirmation || (opaqueRequestID.MatchString(key) && r.PostForm.Get("confirm") == "yes")
}

// reviewProposal prepares a POST-only context and reauthorizes its private display.
func (b *browser) reviewProposal(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.acceptanceReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	if _, valid := parseAcceptanceForm(r, false); !valid {
		b.acceptanceError(w, r, 400)
		return
	}
	review, err := b.foundation.prepareProposalReview(r.Context(), b.principal(r.Context()), r.PathValue("id"))
	if err != nil {
		b.acceptanceResult(w, r, err)
		return
	}
	// Display SDK waits must precede the final current-membership SQL checkpoint.
	// A new gate evaluation after that read could release a revoked client's data.
	if !b.foundation.acceptanceReady(r.Context()) {
		b.triageError(w, r, ErrDenied)
		return
	}
	rows, err := b.foundation.readRequests(r.Context(), b.principal(r.Context()), r.PathValue("id"))
	if err != nil || len(rows) != 1 {
		b.triageError(w, r, ErrDenied)
		return
	}
	page := b.requestPage(r, "review", rows[0], nil, "")
	page.AcceptanceEnabled = true
	page.Review, page.SharedProposal = &review, &review.Proposal
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = browserPage.ExecuteTemplate(w, "requests.html", page)
}

// confirmProposal records an explicit server-bound decision, not a payment or order.
func (b *browser) confirmProposal(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.acceptanceReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	key, valid := parseAcceptanceForm(r, true)
	if !valid {
		b.acceptanceError(w, r, 400)
		return
	}
	_, err := b.foundation.acceptProposal(r.Context(), b.principal(r.Context()), r.PathValue("id"), key)
	b.acceptanceResult(w, r, err)
}

// acceptanceResult keeps conflict, missing-record and storage failures distinct.
func (b *browser) acceptanceResult(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errProposalConflict):
		b.acceptanceError(w, r, 409)
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
	case err != nil:
		b.triageError(w, r, err)
	default:
		http.Redirect(w, r, "/portal/requests/"+r.PathValue("id")+"?lang="+language(r), http.StatusSeeOther)
	}
}

// acceptanceError exposes no unpublished proposal version or original retry key.
func (b *browser) acceptanceError(w http.ResponseWriter, r *http.Request, status int) {
	b.renderRequests(w, r, status, "acceptance-conflict", projectRequest{ID: r.PathValue("id")}, nil, "")
}
