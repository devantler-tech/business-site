package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

const workspaceLock = 1869820525

// Take the identity lock, if needed, before this lock. A workspace deletion
// never waits on an identity lock. Both precede invitation and membership writes.
func lockWorkspace(ctx context.Context, tx pgx.Tx, workspace string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::int,hashtext($2))`, workspaceLock, workspace); err != nil {
		return fmt.Errorf("lock workspace: %w", err)
	}
	return nil
}

func workspaceOpen(ctx context.Context, query queryRower, workspace string) (bool, error) {
	var open bool
	err := query.QueryRow(ctx, `SELECT NOT EXISTS (
 SELECT 1 FROM portal_workspace_settings WHERE workspace_id=$1 AND closed_at IS NOT NULL)`, workspace).Scan(&open)
	return open, err
}

func workspaceDisplayName(ctx context.Context, query queryRower, workspace string) (string, error) {
	var name string
	err := query.QueryRow(ctx, `SELECT COALESCE(
 (SELECT display_name FROM portal_workspace_settings WHERE workspace_id=$1),$1)`, workspace).Scan(&name)
	return name, err
}

func (b *browser) renameCustomerWorkspace(ctx context.Context, r *http.Request) (any, error) {
	var input struct {
		Workspace string `json:"workspace"`
		Name      string `json:"name"`
	}
	fields := map[string]*string{"workspace": &input.Workspace, "name": &input.Name}
	if err := decodeOperatorInput(r, &input, fields); err != nil || !validCustomerWorkspace(input.Workspace) ||
		input.Name == "" || strings.TrimSpace(input.Name) != input.Name || !utf8.ValidString(input.Name) ||
		utf8.RuneCountInString(input.Name) > 100 || strings.ContainsFunc(input.Name, unicode.IsControl) {
		return nil, errOperatorInput
	}
	return b.changeWorkspace(ctx, input.Workspace, func(tx pgx.Tx, _ string, closed bool) (any, error) {
		if closed {
			return nil, errOperatorInput
		}
		_, err := tx.Exec(ctx, `INSERT INTO portal_workspace_settings(workspace_id,display_name) VALUES($1,$2)
 ON CONFLICT(workspace_id) DO UPDATE SET display_name=EXCLUDED.display_name`, input.Workspace, input.Name)
		return map[string]string{"id": input.Workspace, "name": input.Name}, err
	})
}

var errWorkspaceStillBills = errors.New("workspace has active billing")

func (b *browser) deleteCustomerWorkspace(ctx context.Context, r *http.Request) (any, error) {
	var input struct {
		Workspace string `json:"workspace"`
		Confirm   string `json:"confirm"`
	}
	fields := map[string]*string{"workspace": &input.Workspace, "confirm": &input.Confirm}
	if err := decodeOperatorInput(r, &input, fields); err != nil || !validCustomerWorkspace(input.Workspace) || input.Confirm == "" {
		return nil, errOperatorInput
	}
	return b.changeWorkspace(ctx, input.Workspace, func(tx pgx.Tx, name string, closed bool) (any, error) {
		if input.Confirm != name {
			return nil, errOperatorInput
		}
		if !closed {
			if err := b.closeWorkspace(ctx, tx, input.Workspace); err != nil {
				return nil, err
			}
		}
		return map[string]bool{"deleted": true}, nil
	})
}

func (b *browser) changeWorkspace(ctx context.Context, workspace string, change func(pgx.Tx, string, bool) (any, error)) (any, error) {
	authority, _ := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority)
	if authority == nil || authority.kind != "operator" {
		return nil, ErrDenied
	}
	setOperatorTarget(ctx, workspace)
	tx, err := b.foundation.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockWorkspace(ctx, tx, workspace); err != nil {
		return nil, err
	}
	// Existing invitations and memberships can predate the workspace registry.
	var name string
	var closed, hasOperator bool
	err = tx.QueryRow(ctx, `WITH inventory AS (
 SELECT id FROM portal_workspaces WHERE id=$1
 UNION SELECT client_id FROM portal_memberships WHERE issuer=$2 AND client_id=$1
 UNION SELECT client_id FROM portal_invitations WHERE issuer=$2 AND client_id=$1)
 SELECT COALESCE(w.display_name,i.id),w.closed_at IS NOT NULL,
 EXISTS (SELECT 1 FROM portal_memberships WHERE client_id=i.id AND role='operator')
 FROM inventory i LEFT JOIN portal_workspace_settings w ON w.workspace_id=i.id`, workspace, b.foundation.broker.issuer).Scan(&name, &closed, &hasOperator)
	if errors.Is(err, pgx.ErrNoRows) || hasOperator {
		return nil, errOperatorInput
	}
	if err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	if _, err = tx.Exec(ctx, `INSERT INTO portal_workspaces(id) VALUES($1) ON CONFLICT DO NOTHING`, workspace); err != nil {
		return nil, err
	}
	result, err := change(tx, name, closed)
	if err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return result, tx.Commit(ctx)
}

func (b *browser) closeWorkspace(ctx context.Context, tx pgx.Tx, workspace string) error {
	issuer := b.foundation.broker.issuer
	if _, err := tx.Exec(ctx, `UPDATE portal_invitations SET revoked=true
 WHERE issuer=$1 AND client_id=$2 AND role='client' AND NOT revoked`, issuer, workspace); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `DELETE FROM portal_memberships
 WHERE issuer=$1 AND client_id=$2 AND role='client' RETURNING subject,incarnation::text`, issuer, workspace)
	if err != nil {
		return err
	}
	type removedMember struct{ Subject, Incarnation string }
	removed, err := pgx.CollectRows(rows, pgx.RowToStructByPos[removedMember])
	if err != nil {
		return err
	}
	var billing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM portal_orders o WHERE o.client_id=$1 AND (`+activeWorkspaceBilling+`))`, workspace).Scan(&billing); err != nil {
		return err
	}
	if billing {
		return errWorkspaceStillBills
	}
	if _, err := tx.Exec(ctx, `INSERT INTO portal_workspace_settings(workspace_id,closed_at) VALUES($1,now())
 ON CONFLICT(workspace_id) DO UPDATE SET closed_at=EXCLUDED.closed_at`, workspace); err != nil {
		return err
	}
	closedAccounts := make([]string, 0, len(removed))
	for _, member := range removed {
		if customeridentity.DexUser(member.Subject, customeridentity.ZitadelConnectorID) != "" && b.foundation.canInvite(member.Subject, "client") {
			closedAccounts = append(closedAccounts, member.Incarnation)
		}
	}
	return recordClosedCustomerAccounts(ctx, tx, issuer, workspace, closedAccounts)
}

func recordClosedCustomerAccounts(ctx context.Context, tx pgx.Tx, issuer, workspace string, closedAccounts []string) error {
	if len(closedAccounts) == 0 {
		return nil
	}
	saved, err := tx.Exec(ctx, `INSERT INTO portal_customer_account_closures(membership_event,closed_at,remove_by)
 SELECT h.incarnation,w.closed_at,w.closed_at+interval '720 hours'
 FROM portal_membership_transitions h
 JOIN portal_workspace_settings w ON w.workspace_id=h.client_id
 JOIN unnest($3::text[]) AS prior(incarnation) ON h.previous_incarnation=prior.incarnation::uuid
 WHERE h.issuer=$1 AND h.client_id=$2 AND h.role='client' AND h.kind='delete'`, issuer, workspace, closedAccounts)
	if err != nil || saved.RowsAffected() != int64(len(closedAccounts)) {
		return errTriageStorage
	}
	return nil
}

func (b *browser) removeCustomerMember(ctx context.Context, r *http.Request) (any, error) {
	input, err := managementFields(r, "workspace", "subject")
	if err != nil || !validCustomerWorkspace(input.Workspace) || input.Subject == "" || len(input.Subject) > 512 ||
		strings.ContainsFunc(input.Subject, unicode.IsControl) || !b.foundation.canInvite(input.Subject, "client") {
		return nil, errOperatorInput
	}
	authority, _ := ctx.Value(operatorAuthorityKey{}).(*operatorAuthority)
	if authority == nil || authority.kind != "operator" {
		return nil, ErrDenied
	}
	setOperatorTarget(ctx, input.Subject)
	tx, err := b.customerIdentityTransaction(ctx, input.Subject)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockWorkspace(ctx, tx, input.Workspace); err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	result, err := tx.Exec(ctx, `DELETE FROM portal_memberships WHERE issuer=$1 AND subject=$2 AND client_id=$3 AND role='client'`,
		b.foundation.broker.issuer, input.Subject, input.Workspace)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() != 1 {
		return nil, errOperatorInput
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return map[string]bool{"removed": true}, tx.Commit(ctx)
}
