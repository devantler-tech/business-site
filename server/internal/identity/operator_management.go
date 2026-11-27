package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

var managementRoutes = []operatorRoute{
	{"POST", "/invitations/revoke", "invitations.revoke", (*browser).revokeCustomerInvitation},
	{"POST", "/workspaces", "workspaces.create", (*browser).createCustomerWorkspace},
	{"POST", "/workspaces/rename", "workspaces.rename", (*browser).renameCustomerWorkspace},
	{"POST", "/workspaces/delete", "workspaces.delete", (*browser).deleteCustomerWorkspace},
	{"GET", "/workspaces/members", "workspaces.members.list", (*browser).customerWorkspaceMembers},
	{"POST", "/members/deactivate", "members.deactivate", (*browser).deactivateCustomerMember},
	{"POST", "/members/reactivate", "members.reactivate", (*browser).reactivateCustomerMember},
	{"POST", "/members/remove", "members.remove", (*browser).removeCustomerMember},
}

type managementPage struct {
	pageData
	Workspaces       []workspaceView
	Invitations      []pendingInvitation
	SupportAvailable bool
	SupportMembers   bool
	View, Query      string
	Detail           *workspaceView
	SelectedMember   *workspaceMember
	InvitationCounts map[string]int
	Closed           []closedCustomerWorkspace
	Stale            []staleWorkspaceInvitations
}

func (b *browser) operatorCustomersPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "customers.view", func(ctx context.Context, client string) ([]byte, error) {
		authority := humanOperatorAuthority(ctx)
		if authority == nil {
			return nil, ErrDenied
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, errOperatorInput
		}
		data := managementPage{pageData: b.operatorData(r, "operator-customers", client)}
		if query.Get("view") == "cleanup-confirm" {
			detail := cleanupPage{managementPage: data}
			if err := b.readCleanupSelection(ctx, r, &detail); err != nil {
				return nil, err
			}
			var buffer bytes.Buffer
			err := b.templates().ExecuteTemplate(&buffer, "operator.html", detail)
			return buffer.Bytes(), err
		}
		if err := b.loadCustomerWorkspacePage(ctx, &data, query); err != nil {
			return nil, err
		}
		if data.View == "" {
			b.identifyDirectoryPeople(ctx, &data, authority.principal.subject)
			data.Workspaces = slices.DeleteFunc(data.Workspaces, func(workspace workspaceView) bool {
				return !workspaceMatchesSearch(workspace, data.Invitations, data.Query)
			})
		}
		if len(data.Workspaces) == 1 && (data.View == "workspace" || data.View == "close" || data.View == "remove" || data.View == "support") {
			b.identifyDirectoryPeople(ctx, &data, authority.principal.subject)
			data.Detail = &data.Workspaces[0]
			data.Title = data.Detail.Name
			data.Intro = ""
			data.SupportAvailable = b.foundation.supportReady(ctx)
			data.SupportMembers = slices.ContainsFunc(data.Detail.Members, func(member workspaceMember) bool {
				return member.Role == "client" && !member.Revoked
			})
			if data.View == "support" && (!data.SupportAvailable || !data.SupportMembers) {
				return nil, errOperatorInput
			}
		}
		if data.View == "remove" {
			subject := query.Get("member")
			for i := range data.Detail.Members {
				member := &data.Detail.Members[i]
				if member.Role == "client" && member.Subject == subject {
					data.SelectedMember = member
				}
			}
			if data.SelectedMember == nil {
				return nil, errOperatorInput
			}
		}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", data)
		return buffer.Bytes(), err
	})
	if status != http.StatusOK {
		if status != http.StatusServiceUnavailable && status != http.StatusBadRequest {
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
	var input managementInput
	var expires *time.Time
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") && r.ParseForm() == nil && r.PostForm.Has("expires") {
		var expiresText string
		err = decodeOperatorInput(r, nil, map[string]*string{"subject": &input.Subject, "workspace": &input.Workspace, "expires": &expiresText})
		at, dateErr := time.Parse(time.RFC3339Nano, expiresText)
		if err != nil || dateErr != nil || !validCustomerWorkspace(input.Workspace) {
			return nil, errOperatorInput
		}
		expires = &at
	} else {
		input, err = managementFields(r, "subject")
	}
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
	result, err := tx.Exec(ctx, `UPDATE portal_invitations SET revoked=true
 WHERE issuer=$1 AND subject=$2 AND role='client' AND NOT revoked AND expires_at>now()
 AND ($3='' OR client_id=$3) AND ($4::timestamptz IS NULL OR expires_at=$4)`,
		b.foundation.broker.issuer, input.Subject, input.Workspace, expires)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
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
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") && r.ParseForm() == nil && r.PostForm.Has("display_name") {
		var displayName string
		if err := decodeOperatorInput(r, nil, map[string]*string{"display_name": &displayName}); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(displayName)
		if !validWorkspaceDisplayName(name) {
			return nil, errOperatorInput
		}
		return b.createNamedWorkspace(ctx, name)
	}
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
	if err = lockWorkspace(ctx, tx, input.Workspace); err != nil {
		return nil, err
	}
	open, err := workspaceOpen(ctx, tx, input.Workspace)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, errOperatorInput
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1) ON CONFLICT DO NOTHING`, input.Workspace); err != nil {
		return nil, err
	}
	name, err := workspaceDisplayName(ctx, tx, input.Workspace)
	if err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	members, err := b.workspaceMembers(ctx, tx, input.Workspace)
	if err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return workspaceView{ID: input.Workspace, Name: name, Members: members}, tx.Commit(ctx)
}

// Display names can be reused after closure. The new workspace has a different
// identity, so invitations never expose records retained for the previous one.
func (b *browser) createNamedWorkspace(ctx context.Context, name string) (any, error) {
	if humanOperatorAuthority(ctx) == nil {
		return nil, ErrDenied
	}
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	const nameLock = 1869820526
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int,hashtext(lower($2)))`, nameLock, name); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `WITH inventory AS (`+activeWorkspaceInventory+`)
 SELECT i.id,COALESCE(w.display_name,i.id) FROM inventory i
 LEFT JOIN portal_workspace_settings w ON w.workspace_id=i.id
 WHERE lower(COALESCE(w.display_name,i.id))=lower($2) AND w.closed_at IS NULL
 LIMIT 2`, b.foundation.broker.issuer, name)
	if err != nil {
		return nil, err
	}
	type named struct{ ID, Name string }
	matching, err := pgx.CollectRows(rows, pgx.RowToStructByPos[named])
	if err != nil {
		return nil, err
	}
	if len(matching) > 1 {
		return nil, errOperatorInput
	}
	if len(matching) == 1 {
		if !validCustomerWorkspace(matching[0].ID) {
			return nil, errOperatorInput
		}
		setOperatorTarget(ctx, matching[0].ID)
		return workspaceView{ID: matching[0].ID, Name: matching[0].Name}, nil
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	id := "ws-" + strings.ToLower(secret())
	setOperatorTarget(ctx, id)
	if _, err = tx.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1)`, id); err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_workspace_settings(workspace_id,display_name) VALUES($1,$2)`, id, name); err != nil {
		return nil, fmt.Errorf("name workspace: %w", err)
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return workspaceView{ID: id, Name: name, Members: []workspaceMember{}}, tx.Commit(ctx)
}

func validWorkspaceDisplayName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name && utf8.ValidString(name) &&
		utf8.RuneCountInString(name) <= 100 && !strings.ContainsFunc(name, unicode.IsControl)
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
	return b.workspaceMembers(ctx, b.foundation.pool, workspace)
}

func (b *browser) workspaceMembers(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, workspace string) ([]workspaceMember, error) {
	rows, err := db.Query(ctx, `SELECT subject,role,revoked FROM portal_memberships WHERE issuer=$1 AND client_id=$2
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings WHERE workspace_id=$2 AND closed_at IS NOT NULL)
 ORDER BY role,subject LIMIT $3`, b.foundation.broker.issuer, workspace, operatorListLimit+1)
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
	connection, release, err := b.foundation.customerIdentityConnection(ctx, input.Subject)
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
