package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type closedCustomerWorkspace struct {
	Name string
	At   time.Time
}

type staleWorkspaceInvitations struct {
	Workspace string
	Name      string
	Count     int
}

type cleanupPage struct {
	managementPage
	CleanupFingerprint string
	CleanupCount       int
}

const invitationCleanupLimit = 50
const staleInvitations = `issuer=$1 AND client_id=$2 AND role='client' AND (revoked OR expires_at<=now())`
const customerWorkspacePath = "/portal/operator/customers"

func (b *browser) loadCustomerWorkspacePage(ctx context.Context, page *managementPage, query url.Values) error {
	workspace, view := query.Get("workspace"), query.Get("view")
	page.Query = strings.TrimSpace(query.Get("q"))
	if utf8.RuneCountInString(page.Query) > 100 || !utf8.ValidString(page.Query) || strings.ContainsFunc(page.Query, unicode.IsControl) {
		return errOperatorInput
	}
	switch view {
	case "new":
		page.View = view
		page.Title, page.Intro = "New workspace", ""
		if page.Lang == "da" {
			page.Title = "Nyt arbejdsområde"
		}
		return nil
	case "closed":
		page.View = view
		page.Intro = ""
		return b.readClosedWorkspaces(ctx, page)
	case "cleanup":
		page.View = view
		page.Intro = ""
		return b.readStaleWorkspaces(ctx, page)
	case "", "close", "remove", "support":
	default:
		return errOperatorInput
	}
	if workspace != "" && workspace != operatorWorkspace && !validCustomerWorkspace(workspace) {
		return errOperatorInput
	}
	var err error
	if page.Workspaces, err = b.workspacesIn(ctx, workspace); err != nil {
		return err
	}
	if workspace != "" {
		if len(page.Workspaces) != 1 || view == "close" && !page.Workspaces[0].Manageable {
			return errOperatorInput
		}
		page.View = "workspace"
		if view != "" {
			page.View = view
		}
	} else if view != "" {
		return errOperatorInput
	}
	page.Invitations, err = b.pendingInvitationsFor(ctx, workspace)
	page.InvitationCounts = map[string]int{}
	for _, invitation := range page.Invitations {
		page.InvitationCounts[invitation.Workspace]++
	}
	slices.SortStableFunc(page.Workspaces, func(a, b workspaceView) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return err
}

func workspaceMatchesSearch(workspace workspaceView, invitations []pendingInvitation, query string) bool {
	query = strings.ToLower(query)
	matches := func(s string) bool { return strings.Contains(strings.ToLower(s), query) }
	if matches(workspace.Name) {
		return true
	}
	for _, member := range workspace.Members {
		if matches(member.Person.Name) || matches(member.Person.Email) {
			return true
		}
	}
	for _, invitation := range invitations {
		if invitation.Workspace == workspace.ID && (matches(invitation.Person.Name) || matches(invitation.Person.Email)) {
			return true
		}
	}
	return false
}

func (b *browser) readClosedWorkspaces(ctx context.Context, page *managementPage) error {
	rows, err := b.foundation.pool.Query(ctx, `SELECT COALESCE(display_name,workspace_id),closed_at
 FROM portal_workspace_settings WHERE closed_at IS NOT NULL
 ORDER BY closed_at DESC,workspace_id LIMIT 100`)
	if err != nil {
		return err
	}
	page.Closed, err = pgx.CollectRows(rows, pgx.RowToStructByPos[closedCustomerWorkspace])
	return err
}

func (b *browser) readStaleWorkspaces(ctx context.Context, page *managementPage) error {
	rows, err := b.foundation.pool.Query(ctx, `SELECT i.client_id,COALESCE(w.display_name,i.client_id),count(*)
 FROM portal_invitations i LEFT JOIN portal_workspace_settings w ON w.workspace_id=i.client_id
 WHERE i.issuer=$1 AND i.role='client' AND (i.revoked OR i.expires_at<=now())
 AND i.client_id NOT IN ($2,$3)
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.client_id=i.client_id AND m.role='operator')
 GROUP BY i.client_id,w.display_name ORDER BY min(i.expires_at),i.client_id LIMIT 100`, b.foundation.broker.issuer, operatorWorkspace, previewWorkspace)
	if err != nil {
		return err
	}
	page.Stale, err = pgx.CollectRows(rows, pgx.RowToStructByPos[staleWorkspaceInvitations])
	return err
}

type cleanupQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (b *browser) cleanupSelection(ctx context.Context, db cleanupQuery, workspace string) ([][]byte, string, error) {
	rows, err := db.Query(ctx, `SELECT token_digest FROM portal_invitations WHERE `+staleInvitations+`
 AND NOT EXISTS (SELECT 1 FROM portal_memberships WHERE client_id=$2 AND role='operator')
 ORDER BY token_digest LIMIT $3`, b.foundation.broker.issuer, workspace, invitationCleanupLimit)
	if err != nil {
		return nil, "", err
	}
	digests, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return nil, "", err
	}
	hash := sha256.New()
	for _, digest := range digests {
		_, _ = hash.Write(digest)
	}
	return digests, hex.EncodeToString(hash.Sum(nil)), nil
}

func (b *browser) readCleanupSelection(ctx context.Context, r *http.Request, page *cleanupPage) error {
	workspace := r.URL.Query().Get("workspace")
	if !validCustomerWorkspace(workspace) {
		return errOperatorInput
	}
	name, err := workspaceDisplayName(ctx, b.foundation.pool, workspace)
	if err != nil {
		return err
	}
	digests, fingerprint, err := b.cleanupSelection(ctx, b.foundation.pool, workspace)
	page.View = "cleanup-confirm"
	page.Detail = &workspaceView{ID: workspace, Name: name}
	page.CleanupCount = len(digests)
	page.CleanupFingerprint = fingerprint
	setOperatorTarget(ctx, workspace)
	return err
}

func (b *browser) cleanupCustomerInvitations(ctx context.Context, r *http.Request) (any, error) {
	if humanOperatorAuthority(ctx) == nil {
		return nil, ErrDenied
	}
	var workspace, fingerprint string
	if r.ParseForm() != nil || decodeOperatorInput(r, nil, map[string]*string{"workspace": &workspace, "fingerprint": &fingerprint}) != nil ||
		!validCustomerWorkspace(workspace) || len(fingerprint) != sha256.Size*2 {
		return nil, errOperatorInput
	}
	tx, err := b.beginWorkspaceChange(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digests, current, err := b.cleanupSelection(ctx, tx, workspace)
	if err != nil {
		return nil, err
	}
	if len(digests) == 0 || current != fingerprint {
		return nil, errOperatorInput
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	result, err := tx.Exec(ctx, `DELETE FROM portal_invitations WHERE `+staleInvitations+` AND token_digest=ANY($3::bytea[])`,
		b.foundation.broker.issuer, workspace, digests)
	if err != nil || result.RowsAffected() != int64(len(digests)) {
		return nil, errTriageStorage
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return nil, ErrDenied
	}
	return map[string]string{"removed": strconv.Itoa(len(digests))}, tx.Commit(ctx)
}
