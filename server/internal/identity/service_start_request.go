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

// beginClientServiceRequest fences the client request and holds the preview
// owner's authority through commit. Real service consumers acquire their
// complete, sorted membership set later in the same transaction.
func (s *Foundation) beginClientServiceRequest(ctx context.Context, p Principal, id string, ready func(context.Context) bool) (pgx.Tx, string, workMember, error) {
	if !ready(ctx) || p.membershipIncarnation == "" {
		return nil, "", workMember{}, ErrDenied
	}
	bound, err := s.requestClient(ctx, p)
	if err != nil {
		return nil, "", workMember{}, ErrDenied
	}
	return s.beginLockedProposalRequest(ctx, id, func(tx pgx.Tx) (string, workMember, error) {
		viewer, err := resolveClientServiceViewer(ctx, tx, p)
		if err != nil {
			return "", workMember{}, err
		}
		if viewer.Client != bound {
			return "", workMember{}, ErrDenied
		}
		if err := s.checkPreviewWrite(ctx, tx, p, bound); err != nil {
			return "", workMember{}, err
		}
		var request string
		if err := tx.QueryRow(ctx, `SELECT id FROM portal_requests WHERE id=$1 AND client_id=$2`, id, bound).Scan(&request); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", workMember{}, err
			}
			return "", workMember{}, errTriageStorage
		}
		if ctx.Err() != nil || !time.Now().Before(p.expires) {
			return "", workMember{}, ErrDenied
		}
		return bound, viewer, nil
	})
}
