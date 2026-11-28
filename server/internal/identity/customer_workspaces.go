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
		db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_sessions WHERE token=$1 AND data=$2 AND expiry>clock_timestamp())`,
			a.key, a.data).Scan(&ok) == nil && ok && ctx.Err() == nil && time.Now().Before(a.expires)
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
		if err = b.foundation.redeem(ctx, account.Principal, workspace); err == nil || errors.Is(err, ErrDenied) {
			incarnation, err = b.foundation.customerWorkspaceIncarnation(ctx, account.Principal, workspace)
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, ErrDenied):
		b.render(w, r, http.StatusForbidden, "error", "", "")
	case err != nil:
		b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
	case !account.current(ctx, b.foundation.pool):
		b.render(w, r, http.StatusForbidden, "error", "", "")
	default:
		if err := b.renewEmptySession(ctx); err != nil {
			b.render(w, r, http.StatusServiceUnavailable, "unavailable", "", "")
			return
		}
		b.sessions.Put(ctx, "issuer", account.issuer)
		b.sessions.Put(ctx, "subject", account.subject)
		b.sessions.Put(ctx, "expires", account.expires.Unix())
		b.sessions.Put(ctx, "membership_incarnation", incarnation)
		b.sessions.Put(ctx, "customer_signed_in", true)
		b.sessions.Put(ctx, "lang", language(r))
		b.sessions.SetDeadline(ctx, account.deadline)
		b.sessions.RememberMe(ctx, true)
		// Forms already open in another tab belong to the previous workspace.
		b.csrf.RegenerateToken(w, r)
		http.Redirect(w, r, "/portal/?lang="+language(r), http.StatusSeeOther)
	}
}

func (s *Foundation) customerWorkspaceIncarnation(ctx context.Context, p Principal, workspace string) (string, error) {
	var incarnation string
	err := s.pool.QueryRow(ctx, `SELECT m.incarnation::text FROM portal_memberships m
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role='client' AND NOT m.revoked
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings w WHERE w.workspace_id=m.client_id AND w.closed_at IS NOT NULL)`,
		p.issuer, p.subject, workspace).Scan(&incarnation)
	return incarnation, err
}
