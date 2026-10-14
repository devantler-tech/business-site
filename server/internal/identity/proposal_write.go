package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// beginProposalWrite joins both proposal writers to the same request lock and
// current operator membership. Callers retain their independent SDK gates.
func (s *Foundation) beginProposalWrite(ctx context.Context, p Principal, id string) (pgx.Tx, string, error) {
	if !opaqueRequestID.MatchString(id) {
		return nil, "", pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", errTriageStorage
	}
	// Retain rollback responsibility until the caller receives the transaction,
	// including if a driver panics while the request/client join is being read.
	handedOff := false
	defer func() {
		if !handedOff {
			rollbackProposalWrite(ctx, tx)
		}
	}()
	client, err := lockProposalClient(ctx, tx, p, id)
	if err != nil {
		return nil, "", err
	}
	handedOff = true
	return tx, client, nil
}

// lockProposalClient holds the operator membership through the transaction and
// derives client scope from the immutable request, never from browser input.
func lockProposalClient(ctx context.Context, tx pgx.Tx, p Principal, id string) (string, error) {
	if err := lockProposalRequest(ctx, tx, id); err != nil {
		return "", err
	}
	var role, client string
	if err := tx.QueryRow(ctx, `SELECT role FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='operator' AND NOT revoked FOR SHARE`, p.issuer, p.subject).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrDenied
		}
		return "", errTriageStorage
	}
	if err := tx.QueryRow(ctx, `SELECT client_id FROM portal_requests WHERE id=$1`, id).Scan(&client); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		return "", errTriageStorage
	}
	return client, nil
}

// lockProposalRequest serializes draft, visibility and client-consent decisions.
// It grants no role or record access; each caller separately locks its membership.
func lockProposalRequest(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820518,hashtext($1))`, id); err != nil {
		return errTriageStorage
	}
	return nil
}

// rollbackProposalWrite releases unfinished work. Commit determines success;
// pgx discards failed rollback connections, and committed transactions need none.
func rollbackProposalWrite(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

// commitProposalWrite reevaluates the caller's SDK gate and expiry immediately
// before committing, preserving refusal after an in-flight authority change.
func commitProposalWrite(ctx context.Context, p Principal, tx pgx.Tx, ready func(context.Context) bool) error {
	if !ready(ctx) || !time.Now().Before(p.expires) {
		return ErrDenied
	}
	if err := tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}

// proposalMutationResult preserves the shared conflict, missing-record and
// unavailable responses while each form keeps its own conflict presentation.
func (b *browser) proposalMutationResult(w http.ResponseWriter, r *http.Request, err error, conflict string) {
	if errors.Is(err, errProposalConflict) {
		b.render(w, r, 409, conflict, "", "")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	http.Redirect(w, r, "/portal/operator/requests/"+r.PathValue("id")+"?lang="+language(r), http.StatusSeeOther)
}
