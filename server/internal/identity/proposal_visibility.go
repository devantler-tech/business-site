package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

type visibilityDecision struct {
	ExpectedEvent, Proposal, ExpectedProposal int
	Action, Key                               string
}

type proposalVisibility struct {
	Event, Active int
	Shared        map[int]bool
}

func (s *Foundation) sharingReady(ctx context.Context) bool {
	if !s.proposalsReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-proposal-sharing", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

func (s *Foundation) sharingOperator(ctx context.Context, p Principal) error {
	if !s.sharingReady(ctx) {
		return ErrDenied
	}
	return s.triageOperator(ctx, p)
}

func parseVisibility(r *http.Request) (visibilityDecision, bool) {
	if err := r.ParseForm(); err != nil {
		return visibilityDecision{}, false
	}
	for name, values := range r.PostForm {
		if len(values) != 1 {
			return visibilityDecision{}, false
		}
		switch name {
		case "submission_key", "expected_event", "expected_proposal", "proposal_version", "action", "csrf_token", "lang":
		default:
			return visibilityDecision{}, false
		}
	}
	event, eok := proposalNumber(r.PostForm.Get("expected_event"), 99)
	latest, lok := proposalNumber(r.PostForm.Get("expected_proposal"), 50)
	version, vok := proposalNumber(r.PostForm.Get("proposal_version"), 50)
	d := visibilityDecision{ExpectedEvent: event, ExpectedProposal: latest, Proposal: version, Action: r.PostForm.Get("action"), Key: r.PostForm.Get("submission_key")}
	return d, eok && lok && vok && version > 0 && latest >= version && opaqueRequestID.MatchString(d.Key) && (d.Action == "share" || d.Action == "withdraw")
}

// visibilityState reads the complete bounded event history. A withdrawal clears
// the current selection; it never falls back to an older shared draft.
func visibilityState(ctx context.Context, tx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, id string, p Principal) (proposalVisibility, error) {
	rows, err := tx.Query(ctx, `SELECT event,proposal_version,action FROM portal_proposal_visibility WHERE request_id=$1 AND EXISTS(SELECT 1 FROM portal_memberships m WHERE m.issuer=$2 AND m.subject=$3 AND m.role='operator' AND NOT m.revoked) ORDER BY event LIMIT 100`, id, p.issuer, p.subject)
	if err != nil {
		return proposalVisibility{}, errTriageStorage
	}
	defer rows.Close()
	v := proposalVisibility{Shared: make(map[int]bool)}
	for rows.Next() {
		var version int
		var action string
		if err := rows.Scan(&v.Event, &version, &action); err != nil {
			return proposalVisibility{}, errTriageStorage
		}
		v.Active = 0
		if action == "share" {
			v.Shared[version] = true
			v.Active = version
		}
	}
	if rows.Err() != nil {
		return proposalVisibility{}, errTriageStorage
	}
	return v, nil
}

func (s *Foundation) readVisibility(ctx context.Context, p Principal, id string) (proposalVisibility, error) {
	if err := s.sharingOperator(ctx, p); err != nil {
		return proposalVisibility{}, err
	}
	v, err := visibilityState(ctx, s.pool, id, p)
	if err != nil {
		return proposalVisibility{}, err
	}
	if err := s.sharingOperator(ctx, p); err != nil {
		return proposalVisibility{}, err
	}
	if !s.sharingReady(ctx) || !time.Now().Before(p.expires) {
		return proposalVisibility{}, ErrDenied
	}
	return v, nil
}

// persistVisibility shares the revision writer's per-request lock. Current
// membership is locked through commit; retries are bound to content and actor.
func (s *Foundation) persistVisibility(ctx context.Context, p Principal, id string, d visibilityDecision) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.sharingOperator(ctx, p); err != nil {
		return err
	}
	tx, client, err := s.beginProposalWrite(ctx, p, id)
	if err != nil {
		return err
	}
	defer rollbackProposalWrite(ctx, tx)
	var saved visibilityDecision
	var issuer, subject string
	err = tx.QueryRow(ctx, `SELECT expected_event,expected_proposal,proposal_version,action,operator_issuer,operator_subject FROM portal_proposal_visibility WHERE request_id=$1 AND submission_key=$2`, id, d.Key).Scan(&saved.ExpectedEvent, &saved.ExpectedProposal, &saved.Proposal, &saved.Action, &issuer, &subject)
	if err == nil {
		if saved.ExpectedEvent != d.ExpectedEvent || saved.ExpectedProposal != d.ExpectedProposal || saved.Proposal != d.Proposal || saved.Action != d.Action || issuer != p.issuer || subject != p.subject {
			return errProposalConflict
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return errTriageStorage
		}
		state, err := visibilityState(ctx, tx, id, p)
		if err != nil {
			return err
		}
		var latest int
		if err = tx.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM portal_proposals WHERE request_id=$1 AND client_id=$2`, id, client).Scan(&latest); err != nil {
			return errTriageStorage
		}
		if state.Event != d.ExpectedEvent || latest != d.ExpectedProposal || d.Proposal < 1 || d.Proposal > latest || state.Event >= 100 || (d.Action == "share" && state.Shared[d.Proposal]) || (d.Action == "withdraw" && state.Active != d.Proposal) {
			return errProposalConflict
		}
		if _, err = tx.Exec(ctx, `INSERT INTO portal_proposal_visibility(request_id,client_id,event,expected_event,proposal_version,expected_proposal,action,submission_key,operator_issuer,operator_subject) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, client, state.Event+1, d.ExpectedEvent, d.Proposal, d.ExpectedProposal, d.Action, d.Key, p.issuer, p.subject); err != nil {
			return errTriageStorage
		}
	}
	return commitProposalWrite(ctx, p, tx, s.sharingReady)
}

// readSharedProposal selects only the latest explicit decision for this client's
// request. It exposes neither draft history, internal retry keys nor attribution.
func (s *Foundation) readSharedProposal(ctx context.Context, p Principal, id string) (*projectProposal, error) {
	if !s.sharingReady(ctx) {
		return nil, ErrDenied
	}
	client, role, err := s.membership(ctx, p)
	if err != nil || role != "client" {
		if err != nil && !errors.Is(err, ErrDenied) {
			return nil, errTriageStorage
		}
		return nil, ErrDenied
	}
	var d projectProposal
	err = s.pool.QueryRow(ctx, `SELECT p.version,p.scope,p.deliverables,p.currency,p.one_time_minor,p.monthly_minor,p.one_time_terms,p.monthly_terms,p.prepared_at FROM portal_proposals p JOIN (SELECT proposal_version,action FROM portal_proposal_visibility WHERE request_id=$1 AND client_id=$2 ORDER BY event DESC LIMIT 1) v ON p.version=v.proposal_version AND v.action='share' WHERE p.request_id=$1 AND p.client_id=$2 AND EXISTS(SELECT 1 FROM portal_memberships m WHERE m.issuer=$3 AND m.subject=$4 AND m.client_id=$2 AND m.role='client' AND NOT m.revoked)`, id, client, p.issuer, p.subject).Scan(&d.Version, &d.Scope, &d.Deliverables, &d.Currency, &d.OneTimeMinor, &d.MonthlyMinor, &d.OneTimeTerms, &d.MonthlyTerms, &d.Prepared)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, errTriageStorage
	}
	current, currentRole, check := s.membership(ctx, p)
	if check != nil || currentRole != "client" || current != client || !s.sharingReady(ctx) || !time.Now().Before(p.expires) {
		if check != nil && !errors.Is(check, ErrDenied) {
			return nil, errTriageStorage
		}
		return nil, ErrDenied
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, nil
}

func (b *browser) submitVisibility(w http.ResponseWriter, r *http.Request) {
	p := b.principal(r.Context())
	if err := b.foundation.sharingOperator(r.Context(), p); err != nil {
		b.triageError(w, r, err)
		return
	}
	d, valid := parseVisibility(r)
	if !valid {
		b.render(w, r, 400, "sharing-invalid", "", "")
		return
	}
	err := b.foundation.persistVisibility(r.Context(), p, r.PathValue("id"), d)
	b.proposalMutationResult(w, r, err, "sharing-conflict")
}
