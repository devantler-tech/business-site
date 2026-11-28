package identity

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

// operatorAPIRoot is where the operator API lives. The operator pages read the
// same data through the same authorization and audit path.
const operatorAPIRoot = "/portal/operator/api"

// operatorOpenAPI describes every operator API route. A test keeps it and
// operatorRoutes identical.
//
//go:embed openapi/operator.json
var operatorOpenAPI []byte

// auditLimit bounds one read of the audit log.
const auditLimit = 100

// operatorListLimit bounds inventory reads. Oversized results fail rather than
// silently hiding pending invitations or workspace members.
const operatorListLimit = 1000

// operatorRoute is one operator API action. Its action name is what the audit
// log records and what later per-action grants refer to.
type operatorRoute struct {
	method, path, action string
	run                  func(*browser, context.Context, *http.Request) (any, error)
}

// operatorRoutes is the complete operator API.
var operatorRoutes = append([]operatorRoute{
	{"GET", "/invitations", "invitations.list", operatorRead((*browser).pendingInvitations)},
	{"POST", "/invitations", "invitations.create", (*browser).createCustomerInvitation},
	{"GET", "/workspaces", "workspaces.list", operatorRead((*browser).workspaces)},
	{"GET", "/work-eligibility", "work.eligibility.list", (*browser).workEligibility},
	{"GET", "/audit", "audit.list", operatorRead((*browser).auditLog)},
	{"GET", "/openapi.json", "openapi.read", func(*browser, context.Context, *http.Request) (any, error) {
		return json.RawMessage(operatorOpenAPI), nil
	}},
}, managementRoutes...)

var errOperatorInput = errors.New("invalid or conflicting operator input")

// operatorRead keeps request input out of read-only inventory operations.
func operatorRead[T any](run func(*browser, context.Context) (T, error)) func(*browser, context.Context, *http.Request) (any, error) {
	return func(b *browser, ctx context.Context, _ *http.Request) (any, error) { return run(b, ctx) }
}

type (
	pendingInvitation struct {
		Subject   string                   `json:"subject"`
		Workspace string                   `json:"workspace"`
		Role      string                   `json:"role"`
		Expires   time.Time                `json:"expires"`
		Person    customeridentity.Profile `json:"-"`
	}
	workspaceMember struct {
		Subject string                   `json:"subject"`
		Role    string                   `json:"role"`
		Revoked bool                     `json:"revoked"`
		Person  customeridentity.Profile `json:"-"`
	}
	workspaceView struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Members    []workspaceMember `json:"members"`
		Manageable bool              `json:"-"`
	}
	auditEntry struct {
		At      time.Time `json:"at"`
		Kind    string    `json:"actorKind"`
		Subject string    `json:"actorSubject"`
		Action  string    `json:"action"`
		Target  string    `json:"target"`
		Outcome string    `json:"outcome"`
	}
	operatorError struct {
		Error string `json:"error"`
	}
)

// An old invitation alone does not create a workspace once it can no longer be accepted.
const activeWorkspaceInventory = `SELECT id FROM portal_workspaces
 UNION SELECT client_id FROM portal_memberships WHERE issuer=$1
 UNION SELECT client_id FROM portal_invitations
 WHERE issuer=$1 AND NOT revoked AND expires_at>now()`

// audit records one operator action. A call that cannot be recorded is not answered.
func (s *Foundation) audit(ctx context.Context, p Principal, kind, action, target, outcome string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO portal_operator_audit(actor_issuer,actor_subject,actor_kind,action,target,outcome) VALUES($1,$2,$3,$4,$5,$6)`, p.issuer, p.subject, kind, action, target, outcome); err != nil {
		return fmt.Errorf("record operator action: %w", err)
	}
	return nil
}

// operatorCall authorizes, runs and records one operator action. A current human
// operator or an explicitly permitted machine identity runs it. Refusals are
// recorded, and writes require an immutable audit attempt before any effects.
// A browser without a session has no one to record and gets 401.
// The action buffers its complete response before the final authority checks.
func (b *browser) operatorCall(r *http.Request, action string, run func(context.Context, string) ([]byte, error)) (int, []byte) {
	ctx := r.Context()
	if len(r.Header.Values("Authorization")) > 0 {
		bound, status := b.agentAuthorization(r, action)
		if bound == nil {
			return status, nil
		}
		bound.mutating = r.Method != "GET" && r.Method != "HEAD"
		return b.authorizedOperatorCall(ctx, bound, status, run)
	}
	original := b.contactAuthority(ctx, "")
	p := original.principal
	if p.subject == "" {
		return http.StatusUnauthorized, nil
	}
	snapshot, current := b.captureDurableResponse(ctx, original)
	if !current {
		return http.StatusServiceUnavailable, nil
	}
	client, role, err := b.foundation.membership(ctx, p)
	if err != nil && !errors.Is(err, ErrDenied) {
		return http.StatusServiceUnavailable, nil
	}
	bound := &operatorAuthority{principal: p, kind: "other", action: action, client: client, mutating: r.Method != "GET" && r.Method != "HEAD"}
	status := http.StatusForbidden
	if err == nil && role == "operator" {
		bound.kind, status = "operator", http.StatusOK
		bound.current = func(ctx context.Context, query privateResponseQuery) bool {
			return b.durableSnapshotAllowed(ctx, snapshot, client, "operator", query)
		}
	}
	return b.authorizedOperatorCall(ctx, bound, status, run)
}

// authorizedOperatorCall is shared by human and machine calls. Every wait is
// followed by the original authority check, including before a callback can write.
func (b *browser) authorizedOperatorCall(ctx context.Context, bound *operatorAuthority, status int, run func(context.Context, string) ([]byte, error)) (int, []byte) {
	ctx, cancel := context.WithDeadline(context.WithValue(ctx, operatorAuthorityKey{}, bound), bound.principal.expires)
	defer cancel()
	outcome := "refused"
	var result []byte
	if status == http.StatusOK {
		outcome = "allowed"
		switch {
		case !b.operatorMutationAllowed(ctx, b.foundation.pool):
			status, outcome = http.StatusServiceUnavailable, "failed"
		case bound.mutating && !b.beginOperatorWrite(ctx, bound):
			status, outcome = http.StatusServiceUnavailable, "failed"
		default:
			data, err := run(ctx, bound.client)
			switch {
			case !b.operatorMutationAllowed(ctx, b.foundation.pool):
				status, outcome = http.StatusServiceUnavailable, "failed"
			case errors.Is(err, errOperatorInput), errors.Is(err, errInvitationAccess):
				status, outcome = http.StatusBadRequest, "refused"
			case errors.Is(err, errWorkspaceStillBills):
				status, outcome = http.StatusConflict, "refused"
			case err != nil:
				status, outcome = http.StatusServiceUnavailable, "failed"
			default:
				result = data
			}
		}
	}
	if b.foundation.audit(ctx, bound.principal, bound.kind, bound.action, bound.target, outcome) != nil {
		return http.StatusServiceUnavailable, nil
	}
	// Recording can wait too. Bind the original session bytes and workspace once
	// more after it, with no further SDK calls or response rendering afterward.
	if status == http.StatusOK && !bound.current(ctx, b.foundation.pool) {
		return http.StatusServiceUnavailable, nil
	}
	return status, result
}

// beginOperatorWrite persists an immutable authorization attempt before any
// provider or database effect. A separate normal action row records completion;
// an attempt without completion remains visible when later recording fails.
func (b *browser) beginOperatorWrite(ctx context.Context, bound *operatorAuthority) bool {
	return b.foundation.audit(ctx, bound.principal, bound.kind, bound.action+".attempt", "", "allowed") == nil && b.operatorMutationAllowed(ctx, b.foundation.pool)
}

// operatorAPI answers one route as JSON.
func (b *browser) operatorAPI(route operatorRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status, result := b.operatorCall(r, route.action, func(ctx context.Context, _ string) ([]byte, error) {
			data, err := route.run(b, ctx, r)
			if err != nil {
				return nil, err
			}
			return json.Marshal(data)
		})
		// A native form uses the same action and audit record as JSON callers.
		// The fixed return path cannot be supplied by the caller.
		if r.Method == "POST" && strings.Contains(r.Header.Get("Accept"), "text/html") {
			switch status {
			case http.StatusOK:
				http.Redirect(w, r, b.operatorFormReturn(r, route.action, result), http.StatusSeeOther)
			case http.StatusConflict:
				b.render(w, r, status, "operator-workspace-billing-error", "", "")
			case http.StatusBadRequest:
				if route.action == "invitations.create" {
					b.render(w, r, status, "operator-invitation-error", "", "")
				} else if route.action == "workspaces.purge" {
					b.render(w, r, status, "operator-workspace-purge-error", "", "")
				} else {
					b.render(w, r, status, "operator-action-error", "", "")
				}
			default:
				b.render(w, r, status, "operator-action-error", "", "")
			}
			return
		}
		problem := ""
		switch status {
		case http.StatusOK:
		case http.StatusUnauthorized:
			problem = "sign in as an operator"
		case http.StatusForbidden:
			problem = "operators only"
		case http.StatusBadRequest:
			problem = "invalid or conflicting action"
		case http.StatusConflict:
			problem = "end active billing before deleting this workspace"
		default:
			problem = "unavailable"
		}
		if problem != "" {
			result, _ = json.Marshal(operatorError{problem})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(result)
	}
}

func (b *browser) operatorFormReturn(r *http.Request, action string, result []byte) string {
	query := url.Values{"lang": {language(r)}}
	switch {
	case action == "invitations.cleanup":
		query.Set("view", "cleanup")
	case action == "workspaces.purge" || action == "workspaces.delete" && r.URL.Query().Get("focus") == "workspace":
		query.Set("view", "closed")
	case action == "workspaces.create" && r.PostForm.Has("display_name"):
		var workspace workspaceView
		if json.Unmarshal(result, &workspace) == nil && validCustomerWorkspace(workspace.ID) {
			query.Set("workspace", workspace.ID)
		}
	case r.URL.Query().Get("focus") == "workspace":
		if workspace := r.PostForm.Get("workspace"); validCustomerWorkspace(workspace) {
			query.Set("workspace", workspace)
		}
	}
	return b.visible(customerWorkspacePath) + "?" + query.Encode()
}

// operatorAuditPage shows the newest audit entries on the operator page.
func (b *browser) operatorAuditPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "audit.view", func(ctx context.Context, client string) ([]byte, error) {
		search, err := readAuditSearch(r.URL.RawQuery)
		if err != nil {
			return nil, err
		}
		d := auditSearchPage{pageData: b.operatorData(r, "operator-audit", client), Search: search}
		if err := b.populateAuditPage(ctx, &d); err != nil {
			return nil, err
		}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", d)
		return buffer.Bytes(), err
	})
	switch status {
	case http.StatusOK:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result)
	case http.StatusServiceUnavailable:
		b.render(w, r, status, "unavailable", "", "")
	case http.StatusBadRequest:
		b.render(w, r, status, "operator-action-error", "", "")
	default:
		b.render(w, r, http.StatusForbidden, "error", "", "")
	}
}

// pendingInvitations lists invitations that can still be accepted.
func (b *browser) pendingInvitations(ctx context.Context) ([]pendingInvitation, error) {
	return b.pendingInvitationsFor(ctx, "")
}

func (b *browser) pendingInvitationsFor(ctx context.Context, workspace string) ([]pendingInvitation, error) {
	rows, err := b.foundation.pool.Query(ctx, `SELECT subject,client_id,role,expires_at FROM portal_invitations WHERE
 issuer=$1 AND NOT revoked AND expires_at>now() AND ($3='' OR client_id=$3)
 ORDER BY expires_at,client_id LIMIT $2`, b.foundation.broker.issuer, operatorListLimit+1, workspace)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingInvitation, error) {
		var i pendingInvitation
		return i, row.Scan(&i.Subject, &i.Workspace, &i.Role, &i.Expires)
	})
	if len(list) > operatorListLimit {
		return nil, errors.New("invitation inventory exceeds response limit")
	}
	return nonNil(list), err
}

// workspaces lists every workspace with its members, revoked ones included.
func (b *browser) workspaces(ctx context.Context) ([]workspaceView, error) {
	return b.workspacesIn(ctx, "")
}

func (b *browser) workspacesIn(ctx context.Context, workspace string) ([]workspaceView, error) {
	rows, err := b.foundation.pool.Query(ctx, `WITH inventory AS (`+activeWorkspaceInventory+`)
 SELECT i.id,COALESCE(w.display_name,i.id),COALESCE(m.subject,''),COALESCE(m.role,''),COALESCE(m.revoked,false)
 FROM inventory i LEFT JOIN portal_memberships m ON m.client_id=i.id AND m.issuer=$1
 LEFT JOIN portal_workspace_settings w ON w.workspace_id=i.id
 WHERE w.closed_at IS NULL AND ($3='' OR i.id=$3)
 ORDER BY i.id,m.role,m.subject LIMIT $2`, b.foundation.broker.issuer, operatorListLimit+1, workspace)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	views := []workspaceView{}
	var id, name string
	var m workspaceMember
	count := 0
	_, err = pgx.ForEachRow(rows, []any{&id, &name, &m.Subject, &m.Role, &m.Revoked}, func() error {
		count++
		if count > operatorListLimit {
			return errors.New("membership inventory exceeds response limit")
		}
		if len(views) == 0 || views[len(views)-1].ID != id {
			views = append(views, workspaceView{ID: id, Name: name, Members: []workspaceMember{}, Manageable: validCustomerWorkspace(id)})
		}
		if m.Subject != "" {
			view := &views[len(views)-1]
			view.Members = append(view.Members, m)
			if m.Role == "operator" {
				view.Manageable = false
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return views, nil
}

// auditLog returns the newest audit entries.
func (b *browser) auditLog(ctx context.Context) (any, error) {
	rows, err := b.foundation.pool.Query(ctx, `SELECT at,actor_kind,actor_subject,action,target,outcome FROM portal_operator_audit ORDER BY at DESC,id LIMIT $1`, auditLimit)
	if err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (auditEntry, error) {
		var e auditEntry
		return e, row.Scan(&e.At, &e.Kind, &e.Subject, &e.Action, &e.Target, &e.Outcome)
	})
	return nonNil(list), err
}

// nonNil keeps an empty list a JSON array rather than null.
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
