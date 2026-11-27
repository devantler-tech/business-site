package identity

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/open-feature/go-sdk/openfeature"
)

const introductionCancellationPath = "/portal/appointments/cancel"

func (s *Foundation) appointmentCancellationReady(ctx context.Context) bool {
	if !s.appointmentHistoryReady(ctx) {
		return false
	}
	enabled, err := s.flags.BooleanValue(ctx, "portal-appointment-cancellation", false, openfeature.NewEvaluationContext("", nil))
	return err == nil && enabled && ctx.Err() == nil
}

func (b *browser) cancelAppointmentIntroduction(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if !b.foundation.appointmentCancellationReady(ctx) {
		http.NotFound(w, r)
		return
	}
	client, role, ok := b.browserMember(w, r)
	if !ok {
		return
	}
	if role != "client" {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	reservation, revision, valid := parseAppointmentCancellation(r)
	if !valid {
		b.render(w, r, http.StatusBadRequest, "appointment-cancellation-error", "", "")
		return
	}
	snapshot, ok := b.captureDurableResponse(ctx, b.contactAuthority(ctx, client))
	if !ok {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	err := b.cancelAppointmentWithAuthority(ctx, snapshot, reservation, revision)
	switch {
	case err == nil:
		http.Redirect(w, r, b.base()+"/appointments?lang="+language(r), http.StatusSeeOther)
	case errors.Is(err, errProposalConflict):
		b.render(w, r, http.StatusConflict, "appointment-cancellation-error", "", "")
	case errors.Is(err, ErrDenied):
		b.render(w, r, http.StatusForbidden, "error", "", "")
	default:
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
	}
}

func parseAppointmentCancellation(r *http.Request) (string, int, bool) {
	if r.ParseForm() != nil {
		return "", 0, false
	}
	for field, values := range r.PostForm {
		if len(values) != 1 {
			return "", 0, false
		}
		switch field {
		case "csrf_token", "lang", "reservation", "revision":
		default:
			return "", 0, false
		}
	}
	reservation := r.PostForm.Get("reservation")
	revision, err := strconv.ParseUint(r.PostForm.Get("revision"), 10, 31)
	return reservation, int(revision), err == nil && revision > 0 && opaqueRequestID.MatchString(reservation)
}

func (b *browser) cancelAppointmentWithAuthority(ctx context.Context, snapshot durableResponseSnapshot, reservation string, revision int) error {
	original := snapshot.original
	p, client := original.principal, original.client
	deadline := original.deadline
	if p.expires.Before(deadline) {
		deadline = p.expires
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	s := b.foundation
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := cancelIntroductionGuarded(ctx, tx, reservation, client, revision, func() error {
			// The reservation lock can wait. Decide policy and durable authority
			// again before writing under the same transaction.
			if !s.appointmentCancellationReady(ctx) || p.membershipIncarnation == "" {
				return ErrDenied
			}
			if err := s.checkPreviewWrite(ctx, tx, p, client); err != nil {
				return err
			}
			member := workMember{Issuer: p.issuer, Subject: p.subject, Client: client, Role: "client", Incarnation: p.membershipIncarnation}
			_, matches, err := lockWorkMembers(ctx, tx, []workMember{member})
			if err != nil {
				return err
			}
			if !matches[0] || (s.previewAuthority == nil && !lockBrowserSession(ctx, tx, snapshot)) ||
				!b.durableSnapshotAllowed(ctx, snapshot, client, "client", tx) {
				return ErrDenied
			}
			return nil
		})
		return err
	})
}
