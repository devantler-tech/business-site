package identity

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type proposalResponseSnapshotKey struct{}

type proposalResponseCapture struct {
	snapshot durableResponseSnapshot
	ok       bool
}

func proposalDecisionRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && ((strings.HasPrefix(r.URL.Path, "/portal/operator/requests/") && strings.HasSuffix(r.URL.Path, "/proposal-visibility")) || (strings.HasPrefix(r.URL.Path, "/portal/requests/") && strings.HasSuffix(r.URL.Path, "/proposal-acceptance")))
}

// captureProposalDecisionSession runs inside the maintained session middleware
// and before every common SDK gate for these two human POST routes. It retains
// failed captures too, so a later session can never become a fresh baseline.
func (b *browser) captureProposalDecisionSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel, snapshot, ok := b.captureWorkResponse(r)
		defer cancel()
		ctx = context.WithValue(ctx, proposalResponseSnapshotKey{}, proposalResponseCapture{snapshot: snapshot, ok: ok})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// captureProposalResponse retains the original maintained-store bytes before
// any proposal SDK wait. Role is a server-selected handler constant.
func (b *browser) captureProposalResponse(r *http.Request, role string) (context.Context, context.CancelFunc, Principal, workResponseCheck, bool) {
	ctx, cancel := context.WithCancel(r.Context())
	capture, captured := r.Context().Value(proposalResponseSnapshotKey{}).(proposalResponseCapture)
	snapshot, ok := capture.snapshot, capture.ok
	if !captured {
		cancel()
		ctx, cancel, snapshot, ok = b.captureWorkResponse(r)
	}
	check := func(ctx context.Context, tx pgx.Tx, client string) error {
		if !b.durableSnapshotAllowed(ctx, snapshot, client, role, tx) {
			return ErrDenied
		}
		return nil
	}
	return ctx, cancel, snapshot.original.principal, check, ok
}

// finishProposalResponse uses the shared bounded transaction commit. An exact
// review deadline also bounds the SDK and durable-session queries themselves.
func (s *Foundation) finishProposalResponse(ctx context.Context, p Principal, tx pgx.Tx, ready func(context.Context) bool, deadline time.Time, client string, check workResponseCheck) error {
	if !deadline.IsZero() {
		bounded, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		ctx = bounded
	}
	return s.finishWorkResponse(ctx, p, tx, ready, deadline, client, check)
}

// heldProposalViewer derives the original actor's workspace independently of
// the customer's request scope. It takes no lock; the granting caller locks its
// complete canonical identity set before using the result.
func heldProposalViewer(ctx context.Context, tx pgx.Tx, p Principal, role string) (workMember, error) {
	if p.membershipIncarnation == "" || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return workMember{}, ErrDenied
	}
	viewer := workMember{Issuer: p.issuer, Subject: p.subject, Role: role, Incarnation: p.membershipIncarnation}
	err := tx.QueryRow(ctx, `SELECT m.client_id FROM portal_memberships m WHERE m.issuer=$1 AND m.subject=$2 AND m.role=$3 AND NOT m.revoked AND m.incarnation::text=$4
 AND EXISTS(SELECT 1 FROM portal_membership_transitions h WHERE h.incarnation=m.incarnation AND h.issuer=m.issuer AND h.subject=m.subject AND h.client_id=m.client_id AND h.role=m.role AND NOT h.revoked AND h.kind IN ('baseline','insert','update'))`, p.issuer, p.subject, role, p.membershipIncarnation).Scan(&viewer.Client)
	if ctx.Err() != nil || !time.Now().Before(p.expires) {
		return workMember{}, ErrDenied
	}
	if err != nil {
		return workMember{}, proposalMemberError(err)
	}
	return viewer, nil
}
