package identity

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	previewWorkspace  = "preview-demo"
	previewSubject    = "preview:fictional-client"
	previewSessionKey = "preview_membership_incarnation"
)

type previewOperator struct {
	subject, incarnation string
}

func (p Principal) isClientPreview() bool { return p.preview != (previewOperator{}) }

func (p Principal) previewOwner() Principal {
	return Principal{issuer: p.issuer, subject: p.preview.subject, audience: p.audience, expires: p.expires, membershipIncarnation: p.preview.incarnation}
}

// previewClient reads both memberships in one database snapshot. The synthetic
// identity has no external login, and cannot grant access when the operator is
// no longer eligible or either durable membership has been replaced.
func (s *Foundation) previewClient(ctx context.Context, p Principal) (string, string, string, error) {
	if !s.operatorOnly || p.subject != previewSubject || p.membershipIncarnation == "" ||
		p.preview.incarnation == "" || !s.operators[p.preview.subject] {
		return "", "", "", ErrDenied
	}
	var incarnation string
	err := s.pool.QueryRow(ctx, `SELECT m.incarnation::text
 FROM portal_memberships m JOIN portal_memberships a ON a.issuer=m.issuer
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked AND m.incarnation::text=$4
 AND a.subject=$5 AND a.role='operator' AND NOT a.revoked AND a.incarnation::text=$6`,
		p.issuer, p.subject, previewWorkspace, p.membershipIncarnation, p.preview.subject, p.preview.incarnation).Scan(&incarnation)
	if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return "", "", "", ErrDenied
	}
	if err != nil {
		return "", "", "", fmt.Errorf("read preview membership: %w", err)
	}
	return previewWorkspace, "client", incarnation, nil
}

func (s *Foundation) lockPreviewOperator(ctx context.Context, tx pgx.Tx, p Principal) error {
	if !s.operatorOnly || p.issuer != s.broker.issuer || p.audience != s.broker.clientID ||
		!s.operators[p.subject] || p.membershipIncarnation == "" || !time.Now().Before(p.expires) {
		return ErrDenied
	}
	var incarnation string
	err := tx.QueryRow(ctx, `SELECT incarnation::text FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 AND role='operator' AND NOT revoked AND incarnation::text=$3 FOR SHARE`,
		p.issuer, p.subject, p.membershipIncarnation).Scan(&incarnation)
	if err != nil || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return ErrDenied
	}
	return nil
}

func (s *Foundation) checkPreviewWrite(ctx context.Context, tx pgx.Tx, p Principal, client string) error {
	if !p.isClientPreview() {
		return nil
	}
	if client != previewWorkspace {
		return ErrDenied
	}
	return s.lockPreviewOperator(ctx, tx, p.previewOwner())
}

// startClientPreview prepares the one fictional member under the current
// operator's durable lock. An existing revoked or mismatched record stays
// closed. No invitation, customer record, message or order is created.
func (s *Foundation) startClientPreview(ctx context.Context, operator Principal) (string, error) {
	if !s.ready(ctx) || operator.isClientPreview() {
		return "", ErrDenied
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		// A committed transaction is already closed.
		_ = tx.Rollback(ctx)
	}()
	if err = s.lockPreviewOperator(ctx, tx, operator); err != nil {
		return "", err
	}
	var reserved bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
 SELECT 1 FROM portal_workspaces WHERE id=$1
 UNION ALL SELECT 1 FROM portal_invitations WHERE client_id=$1
 UNION ALL SELECT 1 FROM portal_memberships WHERE client_id=$1 AND (issuer<>$2 OR subject<>$3)
 )`, previewWorkspace, operator.issuer, previewSubject).Scan(&reserved)
	if err != nil {
		return "", err
	}
	if reserved {
		return "", ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role)
 VALUES($1,$2,$3,'client') ON CONFLICT(issuer,subject) DO NOTHING`, operator.issuer, previewSubject, previewWorkspace); err != nil {
		return "", err
	}
	var incarnation string
	err = tx.QueryRow(ctx, `SELECT incarnation::text FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' AND NOT revoked FOR SHARE`,
		operator.issuer, previewSubject, previewWorkspace).Scan(&incarnation)
	if errors.Is(err, pgx.ErrNoRows) || !s.ready(ctx) || !time.Now().Before(operator.expires) {
		return "", ErrDenied
	}
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return incarnation, nil
}

func validPreviewForm(r *http.Request) bool {
	if r.ParseForm() != nil {
		return false
	}
	for key, values := range r.Form {
		if len(values) != 1 || (key != "csrf_token" && key != "lang") {
			return false
		}
	}
	return true
}

func (b *browser) switchPreviewSession(ctx context.Context, w http.ResponseWriter, previewIncarnation string) error {
	authority := b.contactAuthority(ctx, "")
	current, ok := b.captureDurableResponse(ctx, authority)
	if !ok {
		return ErrDenied
	}
	operator := authority.principal
	if operator.isClientPreview() {
		operator = operator.previewOwner()
	}
	// Prepare cookie settings on a fresh SCS context. LoadAndSave must leave
	// the original request's session untouched after the database rotation.
	replacement, err := b.sessions.Load(context.Background(), "")
	if err != nil {
		return err
	}
	_, values, err := b.sessions.Codec.Decode(current.data)
	if err != nil {
		return err
	}
	if previewIncarnation == "" {
		delete(values, previewSessionKey)
	} else {
		values[previewSessionKey] = previewIncarnation
	}
	b.sessions.RememberMe(replacement, previewIncarnation == "")
	for _, key := range b.sessions.Keys(replacement) {
		values[key] = b.sessions.Get(replacement, key)
	}
	data, err := b.sessions.Codec.Encode(authority.deadline, values)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, authority.deadline)
	defer cancel()
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := b.foundation.lockPreviewOperator(ctx, tx, operator); err != nil {
		return err
	}
	token := secret()
	key := base64.RawURLEncoding.EncodeToString(digest(token))
	// Only one request can replace these exact bytes. Changing the durable
	// key and payload together preserves the old session on a failed write.
	updated, err := tx.Exec(ctx, `UPDATE portal_sessions SET token=$3,data=$4
 WHERE token=$1 AND data=$2 AND expiry>clock_timestamp()`, current.key, current.data, key, data)
	if err != nil {
		return err
	}
	if updated.RowsAffected() != 1 {
		return ErrDenied
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	b.sessions.WriteSessionCookie(replacement, w, token, authority.deadline)
	return nil
}

func (b *browser) enterClientPreview(w http.ResponseWriter, r *http.Request) {
	if !validPreviewForm(r) {
		b.render(w, r, http.StatusBadRequest, "error", "", "")
		return
	}
	incarnation, err := b.foundation.startClientPreview(r.Context(), b.principal(r.Context()))
	if err == nil {
		err = b.switchPreviewSession(r.Context(), w, incarnation)
	}
	b.finishPreviewTransition(w, r, "/portal/", err)
}

func (b *browser) leaveClientPreview(w http.ResponseWriter, r *http.Request) {
	if !validPreviewForm(r) {
		b.render(w, r, http.StatusBadRequest, "error", "", "")
		return
	}
	p := b.principal(r.Context())
	if !p.isClientPreview() {
		b.finishPreviewTransition(w, r, "", ErrDenied)
		return
	}
	b.finishPreviewTransition(w, r, "/portal/operator", b.switchPreviewSession(r.Context(), w, ""))
}

func (b *browser) finishPreviewTransition(w http.ResponseWriter, r *http.Request, path string, err error) {
	switch {
	case errors.Is(err, ErrDenied):
		b.render(w, r, http.StatusForbidden, "error", "", "")
	case err != nil:
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
	default:
		http.Redirect(w, r, path+"?lang="+language(r), http.StatusSeeOther)
	}
}
