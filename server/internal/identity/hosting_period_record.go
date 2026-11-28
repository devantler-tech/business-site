package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type hostingPeriodInput struct {
	id, acceptance string
	start, end     time.Time
}

func parseHostingPeriod(r *http.Request) (hostingPeriodInput, bool) {
	if err := r.ParseForm(); err != nil {
		return hostingPeriodInput{}, false
	}
	for field, values := range r.PostForm {
		switch field {
		case "period_id", "acceptance_id", "starts_at", "ends_at", "confirm", "csrf_token", "lang":
			if len(values) != 1 {
				return hostingPeriodInput{}, false
			}
		default:
			return hostingPeriodInput{}, false
		}
	}
	d := hostingPeriodInput{id: r.PostForm.Get("period_id"), acceptance: r.PostForm.Get("acceptance_id")}
	const layout = "2006-01-02T15:04"
	start, startErr := time.Parse(layout, r.PostForm.Get("starts_at"))
	end, endErr := time.Parse(layout, r.PostForm.Get("ends_at"))
	d.start, d.end = start, end
	return d, opaqueRequestID.MatchString(d.id) && opaqueRequestID.MatchString(d.acceptance) &&
		startErr == nil && endErr == nil && d.start.Before(d.end) && r.PostForm.Get("confirm") == "yes"
}

func (b *browser) submitHostingPeriod(w http.ResponseWriter, r *http.Request) {
	if !b.foundation.hostingRecordingReady(r.Context()) {
		http.NotFound(w, r)
		return
	}
	project := r.PathValue("id")
	if !opaqueRequestID.MatchString(project) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel, snapshot, current := b.captureWorkResponse(r)
	defer cancel()
	p := b.principal(r.Context())
	if !current || p.membershipIncarnation == "" || p.isClientPreview() {
		b.triageError(w, r, ErrDenied)
		return
	}
	client, role, err := b.foundation.membership(ctx, p)
	if err != nil || role != "operator" {
		b.triageError(w, r, ErrDenied)
		return
	}
	input, valid := parseHostingPeriod(r)
	if !valid {
		b.hostingPeriodError(w, r, http.StatusBadRequest)
		return
	}
	err = b.foundation.recordHostingPeriod(ctx, p, project, input, func(ctx context.Context, tx pgx.Tx) bool {
		return b.durableSnapshotAllowed(ctx, snapshot, client, "operator", tx)
	})
	if errors.Is(err, errProposalConflict) {
		b.hostingPeriodError(w, r, http.StatusConflict)
		return
	}
	b.proposalMutationResult(w, r, err, "")
}

func (s *Foundation) recordHostingPeriod(ctx context.Context, p Principal, project string, input hostingPeriodInput, check func(context.Context, pgx.Tx) bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errTriageStorage
	}
	defer rollbackProposalWrite(ctx, tx)
	var result string
	err = tx.QueryRow(ctx, `SELECT public.portal_record_hosting_period($1,$2,$3,$4::uuid,$5,$6,$7,$8)`,
		project, p.issuer, p.subject, p.membershipIncarnation, input.id, input.acceptance, input.start, input.end).Scan(&result)
	if err != nil {
		return errTriageStorage
	}
	switch result {
	case "recorded":
		return commitProposalWrite(ctx, p, tx, func(ctx context.Context) bool {
			return s.hostingRecordingReady(ctx) && check(ctx, tx)
		})
	case "conflict":
		return errProposalConflict
	case "missing":
		return pgx.ErrNoRows
	case "denied":
		return ErrDenied
	default:
		return errTriageStorage
	}
}

func (b *browser) hostingPeriodError(w http.ResponseWriter, r *http.Request, status int) {
	b.render(w, r, status, "hosting-period-error", "", "")
}
