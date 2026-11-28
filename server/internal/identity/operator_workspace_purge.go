package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

func (b *browser) readWorkspacePurge(ctx context.Context, page *managementPage, workspace string) error {
	if !validCustomerWorkspace(workspace) {
		return errOperatorInput
	}
	closed, err := b.closedWorkspaces(ctx, workspace)
	if err != nil {
		return err
	}
	if len(closed) != 1 {
		return errOperatorInput
	}
	setOperatorTarget(ctx, workspace)
	page.View, page.Intro = "purge-confirm", ""
	page.Title = closed[0].Name
	page.Purge = &closed[0]
	return nil
}

func (b *browser) purgeClosedWorkspace(ctx context.Context, r *http.Request) (any, error) {
	authority := humanOperatorAuthority(ctx)
	if authority == nil {
		return nil, ErrDenied
	}
	var workspace, confirmation, closed, reason string
	fields := map[string]*string{"workspace": &workspace, "confirm": &confirmation, "closed": &closed, "reason": &reason}
	if err := decodeOperatorInput(r, nil, fields); err != nil || !validCustomerWorkspace(workspace) ||
		confirmation == "" || (reason != "test" && reason != "request") {
		return nil, errOperatorInput
	}
	expected, err := time.Parse(time.RFC3339Nano, closed)
	if err != nil {
		return nil, errOperatorInput
	}
	tx, err := b.beginWorkspaceChange(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var name string
	err = tx.QueryRow(ctx, `SELECT COALESCE(display_name,workspace_id) FROM portal_workspace_settings
 WHERE workspace_id=$1 AND closed_at=$2`, workspace, expected).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && name != confirmation {
		return nil, errOperatorInput
	}
	if err != nil {
		return nil, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	var deleted bool
	if err = tx.QueryRow(ctx, `SELECT public.portal_purge_closed_workspace($1,$2,$3,$4,$5)`,
		workspace, expected, authority.principal.issuer, authority.principal.subject, reason).Scan(&deleted); err != nil {
		return nil, err
	}
	if !deleted {
		return nil, errOperatorInput
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return map[string]bool{"deleted": true}, tx.Commit(ctx)
}
