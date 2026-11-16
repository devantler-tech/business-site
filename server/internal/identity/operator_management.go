package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
	"github.com/justinas/nosurf"
)

var managementRoutes = []operatorRoute{
	{"POST", "/invitations/revoke", "invitations.revoke", (*browser).revokeCustomerInvitation},
	{"POST", "/workspaces", "workspaces.create", (*browser).createCustomerWorkspace},
	{"GET", "/workspaces/members", "workspaces.members.list", (*browser).customerWorkspaceMembers},
	{"POST", "/members/deactivate", "members.deactivate", (*browser).deactivateCustomerMember},
	{"POST", "/members/reactivate", "members.reactivate", (*browser).reactivateCustomerMember},
}

type managementPage struct {
	pageData
	Workspaces  []workspaceView
	Invitations []pendingInvitation
}

func (b *browser) operatorCustomersPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "customers.view", func(ctx context.Context, client string) ([]byte, error) {
		spaces, err := b.workspaces(ctx)
		if err != nil {
			return nil, err
		}
		invitations, err := b.pendingInvitations(ctx)
		if err != nil {
			return nil, err
		}
		data := managementPage{pageData: b.data(r, "operator-customers", client, nosurf.Token(r)), Workspaces: spaces.([]workspaceView), Invitations: invitations.([]pendingInvitation)}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "portal.html", data)
		return buffer.Bytes(), err
	})
	if status != http.StatusOK {
		if status != http.StatusServiceUnavailable {
			status = http.StatusForbidden
		}
		b.render(w, r, status, "unavailable", "", "")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(result)
}

type managementInput struct {
	Workspace string `json:"workspace"`
	Subject   string `json:"subject"`
}

func managementFields(r *http.Request, fields ...string) (managementInput, error) {
	var input managementInput
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			return input, ErrDenied
		}
	} else {
		if r.ParseForm() != nil {
			return input, ErrDenied
		}
		allowed := map[string]bool{"csrf_token": true, "lang": true}
		for _, field := range fields {
			allowed[field] = true
		}
		for name, values := range r.PostForm {
			if !allowed[name] || len(values) != 1 {
				return input, ErrDenied
			}
		}
		input.Workspace, input.Subject = r.PostForm.Get("workspace"), r.PostForm.Get("subject")
	}
	allowed := map[string]bool{}
	for _, field := range fields {
		allowed[field] = true
	}
	if (input.Workspace != "" && !allowed["workspace"]) || (input.Subject != "" && !allowed["subject"]) {
		return input, ErrDenied
	}
	return input, nil
}

func managementWorkspace(id string) bool {
	return id != "" && id != operatorWorkspace && len(id) <= 100 && strings.TrimSpace(id) == id && utf8.ValidString(id) && !strings.ContainsFunc(id, unicode.IsControl)
}

func (b *browser) revokeCustomerInvitation(r *http.Request, ctx context.Context) (any, error) {
	input, err := managementFields(r, "subject")
	if err != nil || input.Subject == "" || len(input.Subject) > 512 || strings.ContainsFunc(input.Subject, unicode.IsControl) || !canInvite(input.Subject, "client") {
		return nil, ErrDenied
	}
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockIdentity(ctx, tx, b.foundation.broker.issuer, input.Subject); err != nil {
		return nil, err
	}
	setOperatorTarget(ctx, input.Subject)
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	result, err := tx.Exec(ctx, `UPDATE portal_invitations SET revoked=true WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked AND expires_at>now()`, b.foundation.broker.issuer, input.Subject)
	if err != nil || result.RowsAffected() != 1 {
		return nil, ErrDenied
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]bool{"revoked": true}, nil
}

func (b *browser) createCustomerWorkspace(r *http.Request, ctx context.Context) (any, error) {
	input, err := managementFields(r, "workspace")
	if err != nil || !managementWorkspace(input.Workspace) {
		return nil, ErrDenied
	}
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	setOperatorTarget(ctx, input.Workspace)
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1) ON CONFLICT DO NOTHING`, input.Workspace); err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return workspaceView{ID: input.Workspace, Members: []workspaceMember{}}, nil
}

func (b *browser) customerWorkspaceMembers(r *http.Request, ctx context.Context) (any, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["workspace"]) != 1 || !managementWorkspace(query.Get("workspace")) || len(query) > 2 || (len(query) == 2 && len(query["lang"]) != 1) {
		return nil, ErrDenied
	}
	setOperatorTarget(ctx, query.Get("workspace"))
	return b.membersOfWorkspace(ctx, query.Get("workspace"))
}

func (b *browser) membersOfWorkspace(ctx context.Context, workspace string) ([]workspaceMember, error) {
	rows, err := b.foundation.pool.Query(ctx, `SELECT subject,role,revoked FROM portal_memberships WHERE issuer=$1 AND client_id=$2 ORDER BY role,subject LIMIT $3`, b.foundation.broker.issuer, workspace, operatorListLimit+1)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (workspaceMember, error) {
		var member workspaceMember
		return member, row.Scan(&member.Subject, &member.Role, &member.Revoked)
	})
	if len(list) > operatorListLimit {
		return nil, errors.New("workspace membership exceeds response limit")
	}
	return nonNil(list), err
}

func (b *browser) deactivateCustomerMember(r *http.Request, ctx context.Context) (any, error) {
	return b.changeCustomerMember(r, ctx, false)
}

func (b *browser) reactivateCustomerMember(r *http.Request, ctx context.Context) (any, error) {
	return b.changeCustomerMember(r, ctx, true)
}

func (b *browser) changeCustomerMember(r *http.Request, ctx context.Context, active bool) (any, error) {
	input, err := managementFields(r, "workspace", "subject")
	if err != nil || !managementWorkspace(input.Workspace) {
		return nil, ErrDenied
	}
	user := customeridentity.DexUser(input.Subject, customeridentity.ZitadelConnectorID)
	if user == "" || b.foundation.customers == nil {
		return nil, ErrDenied
	}
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockIdentity(ctx, tx, b.foundation.broker.issuer, input.Subject); err != nil {
		return nil, err
	}
	var current bool
	if err = tx.QueryRow(ctx, `SELECT revoked FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' FOR UPDATE`, b.foundation.broker.issuer, input.Subject, input.Workspace).Scan(&current); err != nil {
		return nil, ErrDenied
	}
	setOperatorTarget(ctx, input.Subject)
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if active {
		if err = b.foundation.customers.ReactivateUser(ctx, user); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE portal_memberships SET revoked=$4 WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client'`, b.foundation.broker.issuer, input.Subject, input.Workspace, !active); err != nil {
		return nil, err
	}
	if !active {
		if err = b.endMemberSessions(ctx, tx, input.Subject); err != nil {
			return nil, err
		}
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if !active {
		if !b.operatorMutationAllowed(ctx, b.foundation.pool) {
			return nil, ErrDenied
		}
		if err = b.foundation.customers.DeactivateUser(ctx, user); err != nil {
			return nil, err
		}
	}
	return map[string]bool{"active": active}, nil
}

// endMemberSessions uses the maintained codec, retaining all other identities'
// sessions. The bounded scan fails rather than silently leaving sessions behind.
func (b *browser) endMemberSessions(ctx context.Context, tx pgx.Tx, subject string) error {
	rows, err := tx.Query(ctx, `SELECT token,data FROM portal_sessions WHERE expiry>now() ORDER BY token LIMIT $1`, operatorListLimit+1)
	if err != nil {
		return err
	}
	type session struct {
		token string
		data  []byte
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (session, error) {
		var value session
		return value, row.Scan(&value.token, &value.data)
	})
	if err != nil || len(list) > operatorListLimit {
		return errors.New("session inventory unavailable")
	}
	for _, value := range list {
		_, data, err := b.sessions.Codec.Decode(value.data)
		if err != nil {
			return errors.New("session inventory unavailable")
		}
		if data["issuer"] == b.foundation.broker.issuer && data["subject"] == subject {
			if _, err = tx.Exec(ctx, `DELETE FROM portal_sessions WHERE token=$1 AND data=$2`, value.token, value.data); err != nil {
				return fmt.Errorf("end member session: %w", err)
			}
		}
	}
	return nil
}
