package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/justinas/nosurf"
	"github.com/open-feature/go-sdk/openfeature"
)

const (
	previewWorkspace  = "preview-demo"
	previewSubject    = "preview:fictional-client"
	previewSessionKey = "preview_membership_incarnation"
)

type previewOperator struct {
	subject, incarnation string
}

type previewAuthorityKey struct{}

type previewAuthority struct {
	portal   *Foundation
	operator Principal
	session  durableResponseSnapshot
}

// EnableClientPreviewSessions is startup-only. It lets an existing preview
// session return to the operator even when the fictional storage cannot open.
func (s *Foundation) EnableClientPreviewSessions() {
	s.previewSessions = !s.operatorOnly && s.OperatorsLinked()
}

// LinkClientPreview connects the isolated preview at startup. Each process
// must validate its database before it can serve the first preview request.
func (s *Foundation) LinkClientPreview(pool *pgxpool.Pool, flags *openfeature.Client, checkStore func(context.Context) error) error {
	if s == nil || s.operatorOnly || s.clientPreview != nil || pool == nil || s.pool == nil ||
		pool == s.pool || s.broker == nil || flags == nil || checkStore == nil || !s.OperatorsLinked() {
		return ErrDenied
	}
	if pool.Config().ConnConfig.Database == s.pool.Config().ConnConfig.Database {
		return ErrDenied
	}
	preview := New(pool, s.broker, flags)
	preview.operatorOnly = true
	preview.previewAuthority = s
	preview.operators = s.operators
	preview.site = s.site
	preview.previewStoreCheck = checkStore
	preview.orderTerms.Store(s.orderTerms.Load())
	s.clientPreview = preview
	s.EnableClientPreviewSessions()
	return nil
}

func (s *Foundation) verifyPreviewStore(ctx context.Context) error {
	if s.previewStoreVerified.Load() {
		return nil
	}
	check, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if s.previewStoreCheck == nil || s.previewStoreCheck(check) != nil || check.Err() != nil {
		return errTriageStorage
	}
	s.previewStoreVerified.Store(true)
	return nil
}

func (s *Foundation) previewAvailable() bool {
	return s != nil && (s.operatorOnly || s.clientPreview != nil)
}

func (s *Foundation) canLeaveClientPreview() bool {
	return s != nil && (s.operatorOnly || s.previewSessions)
}

// The primary transaction locks the real operator and exact durable session
// before the preview database is used. Hold both until the request finishes.
func (b *browser) lockUnifiedPreview(ctx context.Context) (context.Context, func(), error) {
	original := b.contactAuthority(ctx, previewWorkspace)
	snapshot, ok := b.captureDurableResponse(ctx, original)
	if !ok {
		return ctx, func() {}, ErrDenied
	}
	operator := original.principal
	if operator.isClientPreview() {
		operator = operator.previewOwner()
	}
	deadline := original.deadline
	if operator.expires.Before(deadline) {
		deadline = operator.expires
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		cancel()
		return ctx, func() {}, err
	}
	release := func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		defer cancel()
		_ = tx.Rollback(cleanup)
	}
	if err = b.foundation.lockPreviewOperator(ctx, tx, operator); err != nil {
		release()
		return ctx, func() {}, err
	}
	if !lockBrowserSession(ctx, tx, snapshot) {
		release()
		return ctx, func() {}, ErrDenied
	}
	grant := &previewAuthority{portal: b.foundation, operator: operator, session: snapshot}
	return context.WithValue(ctx, previewAuthorityKey{}, grant), release, nil
}

func lockBrowserSession(ctx context.Context, tx pgx.Tx, snapshot durableResponseSnapshot) bool {
	var storedDeadline time.Time
	err := tx.QueryRow(ctx, `SELECT expiry FROM portal_sessions
 WHERE token=$1 AND data=$2 AND expiry>clock_timestamp() FOR SHARE`, snapshot.key, snapshot.data).Scan(&storedDeadline)
	now := time.Now()
	return err == nil && ctx.Err() == nil && now.Before(storedDeadline) &&
		now.Before(snapshot.original.deadline) && now.Before(snapshot.original.principal.expires)
}

func (s *Foundation) currentPreviewAuthority(ctx context.Context, operator Principal) (*previewAuthority, bool) {
	grant, _ := ctx.Value(previewAuthorityKey{}).(*previewAuthority)
	return grant, s.previewAuthority != nil && grant != nil && grant.portal == s.previewAuthority &&
		grant.operator == operator && s.previewAuthority.ready(ctx) && ctx.Err() == nil && time.Now().Before(operator.expires) &&
		time.Now().Before(grant.session.original.deadline)
}

func samePreviewSnapshot(a, b durableResponseSnapshot) bool {
	return a.original == b.original && a.key == b.key && bytes.Equal(a.data, b.data)
}

func (b *browser) withUnifiedPreview(ordinary *http.ServeMux, secured http.Handler) (http.Handler, error) {
	demoHandler := b.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.finishPreviewTransition(w, r, "", errTriageStorage)
	}))
	if b.foundation.clientPreview != nil {
		var err error
		demoHandler, err = b.unifiedPreviewHandler()
		if err != nil {
			return nil, err
		}
	}
	forbidden := b.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.finishPreviewTransition(w, r, "", ErrDenied)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := ordinary.Handler(r)
		// Switching back and logout mutate only the real session. Neither
		// needs the preview store or holds its preview request locks.
		if !b.sessions.Exists(r.Context(), previewSessionKey) ||
			pattern == "POST /portal/preview/leave" || pattern == "POST /portal/logout" ||
			strings.HasPrefix(pattern, "GET /portal/assets/") {
			if !b.foundation.browserRouteAvailable(r) {
				http.NotFound(w, r)
				return
			}
			secured.ServeHTTP(w, r)
			return
		}
		if !unifiedPreviewRouteAllowed(pattern) {
			forbidden.ServeHTTP(w, r)
			return
		}
		demoHandler.ServeHTTP(w, r)
	}), nil
}

func (b *browser) unifiedPreviewHandler() (http.Handler, error) {
	demo := *b
	demo.foundation = b.foundation.clientPreview
	demoRoutes, err := demo.routes()
	if err != nil {
		return nil, err
	}
	return demo.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !demo.foundation.ready(r.Context()) || !demo.foundation.browserRouteAvailable(r) {
			http.NotFound(w, r)
			return
		}
		ctx, release, err := b.lockUnifiedPreview(r.Context())
		if err != nil {
			demo.finishPreviewTransition(w, r, "", err)
			return
		}
		defer release()
		if err := demo.foundation.verifyPreviewStore(ctx); err != nil {
			demo.finishPreviewTransition(w, r, "", err)
			return
		}
		demoRoutes.ServeHTTP(w, r.WithContext(ctx))
	})), nil
}

// The client templates share ordinary routes, so preview routing must refuse
// real customer, operator and billing operations before their handlers run.
func unifiedPreviewRouteAllowed(pattern string) bool {
	switch pattern {
	case "GET /portal/{$}", "GET /portal/contact", "POST /portal/contact",
		"GET /portal/requests", "GET /portal/requests/new", "GET /portal/requests/{id}", "POST /portal/requests",
		"POST /portal/requests/{id}/proposal-review", "POST /portal/requests/{id}/proposal-acceptance",
		"POST /portal/requests/{id}/preview-orders":
		return true
	default:
		return false
	}
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
	var err error
	if s.previewAuthority != nil {
		if _, ok := s.currentPreviewAuthority(ctx, p.previewOwner()); !ok {
			return "", "", "", ErrDenied
		}
		err = s.pool.QueryRow(ctx, `SELECT incarnation::text FROM portal_memberships
 WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' AND NOT revoked AND incarnation::text=$4`,
			p.issuer, p.subject, previewWorkspace, p.membershipIncarnation).Scan(&incarnation)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT m.incarnation::text
 FROM portal_memberships m JOIN portal_memberships a ON a.issuer=m.issuer
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked AND m.incarnation::text=$4
 AND a.subject=$5 AND a.role='operator' AND NOT a.revoked AND a.incarnation::text=$6`,
			p.issuer, p.subject, previewWorkspace, p.membershipIncarnation, p.preview.subject, p.preview.incarnation).Scan(&incarnation)
	}
	if errors.Is(err, pgx.ErrNoRows) || ctx.Err() != nil || !time.Now().Before(p.expires) {
		return "", "", "", ErrDenied
	}
	if err != nil {
		return "", "", "", fmt.Errorf("read preview membership: %w", err)
	}
	if s.previewAuthority != nil {
		if _, ok := s.currentPreviewAuthority(ctx, p.previewOwner()); !ok {
			return "", "", "", ErrDenied
		}
	}
	return previewWorkspace, "client", incarnation, nil
}

func (s *Foundation) lockPreviewOperator(ctx context.Context, tx pgx.Tx, p Principal) error {
	if !s.canLeaveClientPreview() || p.issuer != s.broker.issuer || p.audience != s.broker.clientID ||
		!s.operators[p.subject] || p.membershipIncarnation == "" || !time.Now().Before(p.expires) {
		return ErrDenied
	}
	if s.previewAuthority != nil {
		if _, ok := s.currentPreviewAuthority(ctx, p); !ok {
			return ErrDenied
		}
		return nil
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
 VALUES($1,$2,$3,'client') ON CONFLICT DO NOTHING`, operator.issuer, previewSubject, previewWorkspace); err != nil {
		return "", err
	}
	if s.previewAuthority != nil {
		if err = seedPreviewFixtures(ctx, tx); err != nil {
			return "", err
		}
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
	if !b.foundation.ready(ctx) || !time.Now().Before(operator.expires) {
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
	ctx, store := r.Context(), b.foundation
	release := func() {}
	var err error
	if b.foundation.clientPreview != nil {
		store = b.foundation.clientPreview
		ctx, release, err = b.lockUnifiedPreview(ctx)
		if err != nil {
			b.finishPreviewTransition(w, r, "", err)
			return
		}
		if err := store.verifyPreviewStore(ctx); err != nil {
			release()
			b.finishPreviewTransition(w, r, "", err)
			return
		}
	}
	incarnation, err := store.startClientPreview(ctx, b.principal(r.Context()))
	release()
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
		b.render(w, r, http.StatusForbidden, "error", "", nosurf.Token(r))
	case err != nil:
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", nosurf.Token(r))
	default:
		http.Redirect(w, r, path+"?lang="+language(r), http.StatusSeeOther)
	}
}
