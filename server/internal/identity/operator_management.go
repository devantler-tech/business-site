package identity

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
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
		data := managementPage{pageData: b.operatorData(r, "operator-customers", client), Workspaces: spaces, Invitations: invitations}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", data)
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
	if len(fields) == 1 {
		if fields[0] == "workspace" {
			var value struct {
				Workspace string `json:"workspace"`
			}
			err := decodeOperatorInput(r, &value, map[string]*string{"workspace": &value.Workspace})
			return managementInput{Workspace: value.Workspace}, err
		}
		var value struct {
			Subject string `json:"subject"`
		}
		err := decodeOperatorInput(r, &value, map[string]*string{"subject": &value.Subject})
		return managementInput{Subject: value.Subject}, err
	}
	allowed := map[string]*string{}
	for _, field := range fields {
		switch field {
		case "workspace":
			allowed[field] = &input.Workspace
		case "subject":
			allowed[field] = &input.Subject
		}
	}
	if err := decodeOperatorInput(r, &input, allowed); err != nil {
		return input, err
	}
	if (input.Workspace != "" && allowed["workspace"] == nil) || (input.Subject != "" && allowed["subject"] == nil) {
		return input, errOperatorInput
	}
	return input, nil
}

func (b *browser) revokeCustomerInvitation(ctx context.Context, r *http.Request) (any, error) {
	input, err := managementFields(r, "subject")
	if err != nil || input.Subject == "" || len(input.Subject) > 512 || strings.ContainsFunc(input.Subject, unicode.IsControl) || !b.foundation.canInvite(input.Subject, "client") {
		return nil, errOperatorInput
	}
	tx, err := b.customerIdentityTransaction(ctx, input.Subject)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Commit closes a successful transaction; rollback releases a failed one.
		_ = tx.Rollback(ctx)
	}()
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

func (b *browser) createCustomerWorkspace(ctx context.Context, r *http.Request) (any, error) {
	input, err := managementFields(r, "workspace")
	if err != nil || !validCustomerWorkspace(input.Workspace) {
		return nil, errOperatorInput
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
	members, err := b.membersOfWorkspace(ctx, input.Workspace)
	return workspaceView{ID: input.Workspace, Members: members}, err
}

func (b *browser) customerWorkspaceMembers(ctx context.Context, r *http.Request) (any, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["workspace"]) != 1 || !validCustomerWorkspace(query.Get("workspace")) || len(query) > 2 || (len(query) == 2 && len(query["lang"]) != 1) {
		return nil, errOperatorInput
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

func (b *browser) deactivateCustomerMember(ctx context.Context, r *http.Request) (any, error) {
	return b.changeCustomerMember(ctx, r, false)
}

func (b *browser) reactivateCustomerMember(ctx context.Context, r *http.Request) (any, error) {
	return b.changeCustomerMember(ctx, r, true)
}

// customerIdentityTransaction serializes invitation and membership transitions
// for the same broker identity, and releases a transaction whose lock fails.
func (b *browser) customerIdentityTransaction(ctx context.Context, subject string) (pgx.Tx, error) {
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = lockIdentity(ctx, tx, b.foundation.broker.issuer, subject); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (b *browser) changeCustomerMember(ctx context.Context, r *http.Request, active bool) (any, error) {
	input, err := managementFields(r, "workspace", "subject")
	if err != nil || !validCustomerWorkspace(input.Workspace) {
		return nil, errOperatorInput
	}
	user := customeridentity.DexUser(input.Subject, customeridentity.ZitadelConnectorID)
	if user == "" || b.foundation.customers == nil {
		return nil, ErrDenied
	}
	connection, release, err := b.customerIdentityConnection(ctx, input.Subject)
	if err != nil {
		return nil, err
	}
	defer release()
	tx, err := connection.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Commit closes a successful transaction; rollback releases a failed one.
		_ = tx.Rollback(ctx)
	}()
	var current bool
	if err = tx.QueryRow(ctx, `SELECT revoked FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' FOR UPDATE`, b.foundation.broker.issuer, input.Subject, input.Workspace).Scan(&current); err != nil {
		return nil, ErrDenied
	}
	setOperatorTarget(ctx, input.Subject)
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if active {
		if err = b.foundation.customers.ReactivateUser(ctx, user, func() bool { return b.operatorMutationAllowed(ctx, tx) }); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE portal_memberships SET revoked=$4 WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client' AND revoked IS DISTINCT FROM $4`, b.foundation.broker.issuer, input.Subject, input.Workspace, !active); err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	if !active {
		if !b.operatorMutationAllowed(ctx, connection) {
			return nil, ErrDenied
		}
		if err = b.foundation.customers.DeactivateUser(ctx, user, func() bool { return b.operatorMutationAllowed(ctx, connection) }); err != nil {
			return nil, err
		}
	}
	return map[string]bool{"active": active}, nil
}
