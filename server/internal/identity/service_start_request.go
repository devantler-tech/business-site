package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func proposalMemberError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	return errTriageStorage
}

// resolveClientServiceViewer derives a real original-epoch viewer without taking
// a membership lock. The caller first verifies its configured broker/SDK policy,
// then locks its complete canonical identity set before granting anything.
func resolveClientServiceViewer(ctx context.Context, tx pgx.Tx, p Principal) (workMember, error) {
	if p.issuer == "" || p.subject == "" || p.audience == "" || p.membershipIncarnation == "" || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return workMember{}, ErrDenied
	}
	return heldProposalViewer(ctx, tx, p, "client")
}

// beginClientServiceRequest fences the actual client request without acquiring
// an early membership lock. Software and appointment consumers subsequently
// acquire their complete, sorted real membership set under this transaction.
func (s *Foundation) beginClientServiceRequest(ctx context.Context, p Principal, id string, ready func(context.Context) bool) (pgx.Tx, string, workMember, error) {
	if !ready(ctx) || p.membershipIncarnation == "" {
		return nil, "", workMember{}, ErrDenied
	}
	bound, err := s.requestClient(ctx, p)
	if err != nil {
		return nil, "", workMember{}, ErrDenied
	}
	if !opaqueRequestID.MatchString(id) {
		return nil, "", workMember{}, pgx.ErrNoRows
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, "", workMember{}, errTriageStorage
	}
	handedOff := false
	defer func() {
		if !handedOff {
			rollbackProposalWrite(ctx, tx)
		}
	}()
	if err := lockProposalRequest(ctx, tx, id); err != nil {
		return nil, "", workMember{}, err
	}
	viewer, err := resolveClientServiceViewer(ctx, tx, p)
	if err != nil || viewer.Client != bound {
		if err != nil {
			return nil, "", workMember{}, err
		}
		return nil, "", workMember{}, ErrDenied
	}
	var request string
	if err := tx.QueryRow(ctx, `SELECT id FROM portal_requests WHERE id=$1 AND client_id=$2`, id, bound).Scan(&request); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", workMember{}, err
		}
		return nil, "", workMember{}, errTriageStorage
	}
	if ctx.Err() != nil || !time.Now().Before(p.expires) {
		return nil, "", workMember{}, ErrDenied
	}
	handedOff = true
	return tx, bound, viewer, nil
}
