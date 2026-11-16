package identity

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	run                  func(*browser, *http.Request, context.Context) (any, error)
}

// operatorRoutes is the complete operator API.
var operatorRoutes = append([]operatorRoute{
	{"GET", "/invitations", "invitations.list", operatorRead((*browser).pendingInvitations)},
	{"POST", "/invitations", "invitations.create", (*browser).createCustomerInvitation},
	{"GET", "/workspaces", "workspaces.list", operatorRead((*browser).workspaces)},
	{"GET", "/audit", "audit.list", operatorRead((*browser).auditLog)},
	{"GET", "/openapi.json", "openapi.read", func(*browser, *http.Request, context.Context) (any, error) {
		return json.RawMessage(operatorOpenAPI), nil
	}},
}, managementRoutes...)

var errOperatorInput = errors.New("invalid or conflicting operator input")

// operatorRead keeps request input out of read-only inventory operations.
func operatorRead(run func(*browser, context.Context) (any, error)) func(*browser, *http.Request, context.Context) (any, error) {
	return func(b *browser, _ *http.Request, ctx context.Context) (any, error) { return run(b, ctx) }
}

type (
	pendingInvitation struct {
		Subject   string    `json:"subject"`
		Workspace string    `json:"workspace"`
		Role      string    `json:"role"`
		Expires   time.Time `json:"expires"`
	}
	workspaceMember struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
		Revoked bool   `json:"revoked"`
	}
	workspaceView struct {
		ID      string            `json:"id"`
		Members []workspaceMember `json:"members"`
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

// audit records one operator action. A call that cannot be recorded is not answered.
func (s *Foundation) audit(ctx context.Context, p Principal, kind, action, target, outcome string) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO portal_operator_audit(actor_issuer,actor_subject,actor_kind,action,target,outcome) VALUES($1,$2,$3,$4,$5,$6)`, p.issuer, p.subject, kind, action, target, outcome); err != nil {
		return fmt.Errorf("record operator action: %w", err)
	}
	return nil
}

// operatorCall authorizes, runs and records one operator action. Only a current
// operator membership runs it. Every call by a signed-in identity writes one
// audit row before it is answered, refusals included, and a call that cannot be
// recorded fails. A browser without a session has no one to record and gets 401.
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
		if !b.operatorMutationAllowed(ctx, b.foundation.pool) {
			status, outcome = http.StatusServiceUnavailable, "failed"
		} else if bound.mutating && !b.beginOperatorWrite(ctx, bound) {
			status, outcome = http.StatusServiceUnavailable, "failed"
		} else {
			data, err := run(ctx, bound.client)
			switch {
			case !b.operatorMutationAllowed(ctx, b.foundation.pool):
				status, outcome = http.StatusServiceUnavailable, "failed"
			case errors.Is(err, errOperatorInput):
				status, outcome = http.StatusBadRequest, "refused"
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
			data, err := route.run(b, r, ctx)
			if err != nil {
				return nil, err
			}
			return json.Marshal(data)
		})
		// A native form uses the same action and audit record as JSON callers.
		// The fixed return path cannot be supplied by the caller.
		if r.Method == "POST" && strings.Contains(r.Header.Get("Accept"), "text/html") {
			if status == http.StatusOK {
				http.Redirect(w, r, b.visible("/portal/operator/customers")+"?lang="+language(r), http.StatusSeeOther)
			} else {
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

// operatorAuditPage shows the newest audit entries on the operator page.
func (b *browser) operatorAuditPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "audit.view", func(ctx context.Context, client string) ([]byte, error) {
		entries, err := b.auditLog(ctx)
		if err != nil {
			return nil, err
		}
		d := b.data(r, "operator-audit", client, "")
		d.Audit, _ = entries.([]auditEntry)
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "portal.html", d)
		return buffer.Bytes(), err
	})
	switch status {
	case http.StatusOK:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result)
	case http.StatusServiceUnavailable:
		b.render(w, r, status, "unavailable", "", "")
	default:
		b.render(w, r, http.StatusForbidden, "error", "", "")
	}
}

// pendingInvitations lists invitations that can still be accepted.
func (b *browser) pendingInvitations(ctx context.Context) (any, error) {
	rows, err := b.foundation.pool.Query(ctx, `SELECT subject,client_id,role,expires_at FROM portal_invitations WHERE NOT revoked AND expires_at>now() ORDER BY expires_at,client_id LIMIT $1`, operatorListLimit+1)
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
func (b *browser) workspaces(ctx context.Context) (any, error) {
	rows, err := b.foundation.pool.Query(ctx, `WITH inventory AS (
 SELECT id FROM portal_workspaces
 UNION SELECT client_id FROM portal_memberships WHERE issuer=$1
 UNION SELECT client_id FROM portal_invitations WHERE issuer=$1)
 SELECT i.id,COALESCE(m.subject,''),COALESCE(m.role,''),COALESCE(m.revoked,false)
 FROM inventory i LEFT JOIN portal_memberships m ON m.client_id=i.id AND m.issuer=$1
 ORDER BY i.id,m.role,m.subject LIMIT $2`, b.foundation.broker.issuer, operatorListLimit+1)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	views := []workspaceView{}
	var id string
	var m workspaceMember
	count := 0
	_, err = pgx.ForEachRow(rows, []any{&id, &m.Subject, &m.Role, &m.Revoked}, func() error {
		count++
		if count > operatorListLimit {
			return errors.New("membership inventory exceeds response limit")
		}
		if len(views) == 0 || views[len(views)-1].ID != id {
			views = append(views, workspaceView{ID: id, Members: []workspaceMember{}})
		}
		if m.Subject != "" {
			views[len(views)-1].Members = append(views[len(views)-1].Members, m)
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
