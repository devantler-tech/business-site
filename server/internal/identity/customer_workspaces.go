package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
)

type customerWorkspaceChoice struct {
	ID, Name string
	Invited  bool
}

type customerAccountSession struct {
	Principal
	deadline time.Time
	key      string
	data     []byte
}

// A workspace-less account may select access but cannot be used by a
// workspace handler until an exact current membership is bound to the session.
func (b *browser) loadCustomerAccount(ctx context.Context) (customerAccountSession, bool) {
	if !b.signedInCustomer(ctx) || !b.foundation.ready(ctx) {
		return customerAccountSession{}, false
	}
	token := b.sessions.Token(ctx)
	store, ok := b.sessions.Store.(scs.CtxStore)
	if token == "" || !ok {
		return customerAccountSession{}, false
	}
	key := base64.RawURLEncoding.EncodeToString(digest(token))
	data, found, err := store.FindCtx(ctx, key)
	if err != nil || !found {
		return customerAccountSession{}, false
	}
	deadline, values, err := b.sessions.Codec.Decode(data)
	issuer, _ := values["issuer"].(string)
	subject, _ := values["subject"].(string)
	expires, _ := values["expires"].(int64)
	signedIn, _ := values["customer_signed_in"].(bool)
	if err != nil || !signedIn || values[previewSessionKey] != nil ||
		issuer != b.sessions.GetString(ctx, "issuer") || subject != b.sessions.GetString(ctx, "subject") ||
		expires != b.sessions.GetInt64(ctx, "expires") || !time.Now().Before(deadline) {
		return customerAccountSession{}, false
	}
	p := Principal{issuer: issuer, subject: subject, audience: b.foundation.broker.clientID, expires: time.Unix(expires, 0)}
	return customerAccountSession{Principal: p, deadline: deadline, key: key, data: data}, true
}

func (a customerAccountSession) current(ctx context.Context, db privateResponseQuery) bool {
	var ok bool
	return ctx.Err() == nil && time.Now().Before(a.expires) &&
		db.QueryRow(ctx, `SELECT NOT `+customerDeletionExists+`
 AND EXISTS (SELECT 1 FROM portal_sessions WHERE token=$3 AND data=$4 AND expiry>clock_timestamp())`,
			a.issuer, a.subject, a.key, a.data).Scan(&ok) == nil && ok && ctx.Err() == nil && time.Now().Before(a.expires)
}

const customerWorkspaceChoicesSQL = `WITH visible AS (
 SELECT client_id,false AS invited FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked
 UNION
 SELECT client_id,true AS invited FROM portal_invitations i WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked AND expires_at>now()
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.issuer=i.issuer AND m.subject=i.subject AND m.client_id=i.client_id)
) SELECT v.client_id,COALESCE(w.display_name,v.client_id),v.invited
 FROM visible v LEFT JOIN portal_workspace_settings w ON w.workspace_id=v.client_id
 WHERE w.closed_at IS NULL AND v.client_id NOT IN ($3,$4) ORDER BY 2,1`

func (b *browser) customerWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := b.loadCustomerAccount(ctx)
	if !ok {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	rows, err := b.foundation.pool.Query(ctx, customerWorkspaceChoicesSQL, account.issuer, account.subject, operatorWorkspace, previewWorkspace)
	if err != nil {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	choices, err := pgx.CollectRows(rows, pgx.RowToStructByPos[customerWorkspaceChoice])
	if err != nil {
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
		return
	}
	page := b.data(r, "workspace-picker", "", nosurf.Token(r))
	page.Workspaces = choices
	page.Title, page.Intro = "Your workspaces", ""
	if page.Lang == "da" {
		page.Title = "Dine arbejdsområder"
	}
	if len(choices) == 0 {
		page.Kind = "no-workspaces"
	}
	var result bytes.Buffer
	if err := b.templates().ExecuteTemplate(&result, "portal.html", page); err != nil || !account.current(ctx, b.foundation.pool) {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	b.writeOperatorActionPage(w, r, http.StatusOK, result.Bytes())
}

func (b *browser) openCustomerWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := b.loadCustomerAccount(ctx)
	if !ok || r.ParseForm() != nil || len(r.PostForm["workspace"]) != 1 || !validCustomerWorkspace(r.PostForm.Get("workspace")) {
		b.render(w, r, http.StatusForbidden, "error", "", "")
		return
	}
	workspace := r.PostForm.Get("workspace")
	incarnation, err := b.foundation.customerWorkspaceIncarnation(ctx, account.Principal, workspace)
	if errors.Is(err, pgx.ErrNoRows) {
		session := durableResponseSnapshot{
			original: contactAuthority{principal: account.Principal, deadline: account.deadline},
			key:      account.key,
			data:     account.data,
		}
		if err = b.foundation.redeem(ctx, account.Principal, workspace, &session); err == nil || errors.Is(err, ErrDenied) {
			incarnation, err = b.foundation.customerWorkspaceIncarnation(ctx, account.Principal, workspace)
		}
	}
	if err == nil {
		err = b.switchCustomerWorkspace(w, r, account, incarnation)
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, ErrDenied):
		b.render(w, r, http.StatusForbidden, "error", "", "")
	case err != nil:
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
	default:
		// Forms already open in another tab belong to the previous workspace.
		b.csrf.RegenerateToken(w, r)
		http.Redirect(w, r, "/portal/?lang="+language(r), http.StatusSeeOther)
	}
}

// Replace the exact authenticated session in one database statement. A
// concurrent sign-out or another successful switch consumes the original
// token, so this request cannot issue another token from its stale snapshot.
func (b *browser) switchCustomerWorkspace(w http.ResponseWriter, r *http.Request, account customerAccountSession, incarnation string) error {
	ctx := r.Context()
	tx, err := b.customerIdentityTransaction(ctx, account.subject)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !b.foundation.ready(ctx) {
		return ErrDenied
	}
	deadline := account.deadline
	if account.expires.Before(deadline) {
		deadline = account.expires
	}
	data, err := b.sessions.Codec.Encode(deadline, map[string]any{
		"issuer":                 account.issuer,
		"subject":                account.subject,
		"expires":                account.expires.Unix(),
		"membership_incarnation": incarnation,
		"customer_signed_in":     true,
		"lang":                   language(r),
		"__rememberMe":           b.sessions.GetBool(ctx, "__rememberMe"),
	})
	if err != nil {
		return err
	}
	token := secret() + secret()
	key := base64.RawURLEncoding.EncodeToString(digest(token))
	var expiry time.Time
	err = tx.QueryRow(ctx, `WITH original AS (
 DELETE FROM portal_sessions WHERE token=$3 AND data=$4 AND expiry>clock_timestamp()
 AND NOT `+customerDeletionExists+` RETURNING expiry
) INSERT INTO portal_sessions(token,data,expiry)
 SELECT $6,$7,LEAST(expiry,$5::timestamptz) FROM original
 WHERE LEAST(expiry,$5::timestamptz)>clock_timestamp() RETURNING expiry`,
		account.issuer, account.subject, account.key, account.data, deadline, key, data).Scan(&expiry)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Leave the middleware's loaded context untouched so its save step cannot
	// write the original token back after rotation.
	b.sessions.WriteSessionCookie(ctx, w, token, expiry)
	return nil
}

func (s *Foundation) customerWorkspaceIncarnation(ctx context.Context, p Principal, workspace string) (string, error) {
	var incarnation string
	err := s.pool.QueryRow(ctx, `SELECT m.incarnation::text FROM portal_memberships m
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings w WHERE w.workspace_id=m.client_id AND w.closed_at IS NOT NULL)`,
		p.issuer, p.subject, workspace).Scan(&incarnation)
	return incarnation, err
}
