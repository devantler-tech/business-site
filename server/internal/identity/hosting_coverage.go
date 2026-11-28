package identity

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

type hostingCoverage struct {
	State            string
	Scope            *string
	ProposalVersion  *int
	StartsAt, EndsAt *time.Time
}

func (s *Foundation) hostingRecordingReady(ctx context.Context) bool {
	return s.acceptanceReady(ctx) && s.hostingCoverageReady(ctx)
}

func (s *Foundation) hostingCoverageReady(ctx context.Context) bool {
	if !s.triageReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-hosting-periods", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled
}

func (s *Foundation) readHostingCoverage(ctx context.Context, p Principal, project string) (*hostingCoverage, error) {
	if p.membershipIncarnation == "" || !s.hostingCoverageReady(ctx) {
		return nil, ErrDenied
	}
	var coverage hostingCoverage
	err := s.pool.QueryRow(ctx, `SELECT c.state,c.scope,c.proposal_version,c.starts_at,c.ends_at
 FROM portal_requests q CROSS JOIN LATERAL public.portal_operator_hosting_coverage(q.id,q.client_id,$2,$3,$4::uuid) c
 WHERE q.id=$1 AND EXISTS (SELECT 1 FROM portal_memberships m WHERE m.issuer=$2 AND m.subject=$3
 AND m.incarnation=$4 AND m.role='operator' AND NOT m.revoked)`,
		project, p.issuer, p.subject, p.membershipIncarnation).Scan(
		&coverage.State, &coverage.Scope, &coverage.ProposalVersion, &coverage.StartsAt, &coverage.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDenied
	}
	if err != nil {
		return nil, errTriageStorage
	}
	return &coverage, nil
}

func (b *browser) showHostingReview(w http.ResponseWriter, r *http.Request, project string, page triagePageData) {
	ctx, cancel, snapshot, current := b.captureWorkResponse(r)
	defer cancel()
	if !current {
		b.triageError(w, r, ErrDenied)
		return
	}
	operator := snapshot.original.principal
	client, role, err := b.foundation.membership(ctx, operator)
	if err != nil || role != "operator" {
		b.triageError(w, r, ErrDenied)
		return
	}
	page.Hosting, err = b.foundation.readHostingCoverage(ctx, operator, project)
	if err != nil {
		b.triageError(w, r, err)
		return
	}
	if b.foundation.hostingRecordingReady(ctx) {
		for _, acceptance := range page.Acceptances {
			if acceptance.Proposal.ContentFormat == 2 && acceptance.Proposal.Service.Kind == proposalServiceSoftware {
				page.HostingAgreements = append(page.HostingAgreements, acceptance)
			}
		}
		if len(page.HostingAgreements) > 0 {
			page.HostingPeriodID = secret()
		}
	}
	var html bytes.Buffer
	if err := b.templates().ExecuteTemplate(&html, "triage.html", page); err != nil {
		b.triageError(w, r, err)
		return
	}
	if (page.HostingPeriodID != "" && !b.foundation.acceptanceReady(ctx)) ||
		!b.foundation.hostingCoverageReady(ctx) || !b.durableSnapshotAllowed(ctx, snapshot, client, "operator", b.foundation.pool) {
		b.triageError(w, r, ErrDenied)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(html.Bytes())
}
