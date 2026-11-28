package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/devantler-tech/business-site/server/internal/customeridentity"
	"github.com/jackc/pgx/v5"
)

type customerInvitationInput struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Workspace string `json:"workspace"`
}

var errInvitationAccess = errors.New("invitation conflicts with existing access")

func validCustomerWorkspace(workspace string) bool {
	return workspace != "" && workspace != operatorWorkspace && workspace != previewWorkspace && len(workspace) <= 100 && strings.TrimSpace(workspace) == workspace && utf8.ValidString(workspace) && !strings.ContainsFunc(workspace, unicode.IsControl)
}

// decodeOperatorInput admits one bounded JSON object or the native form's exact
// named fields. Authentication and CSRF happen before this callback is entered.
func decodeOperatorInput(r *http.Request, value any, fields map[string]*string) error {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return errOperatorInput
	}
	switch kind {
	case "application/json":
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
			return errOperatorInput
		}
	case "application/x-www-form-urlencoded":
		if r.ParseForm() != nil {
			return errOperatorInput
		}
		for name, values := range r.PostForm {
			if len(values) != 1 {
				return errOperatorInput
			}
			if name == "csrf_token" {
				continue
			}
			field, ok := fields[name]
			if !ok {
				return errOperatorInput
			}
			*field = values[0]
		}
	default:
		return errOperatorInput
	}
	return nil
}

func (b *browser) createCustomerInvitation(ctx context.Context, r *http.Request) (any, error) {
	var input customerInvitationInput
	if err := decodeOperatorInput(r, &input, map[string]*string{"name": &input.Name, "email": &input.Email, "workspace": &input.Workspace}); err != nil {
		return nil, err
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Name != "" || address.Address != input.Email || len(input.Email) > 254 || len(strings.Fields(input.Name)) < 2 || len(input.Name) > 200 || strings.TrimSpace(input.Name) != input.Name || !utf8.ValidString(input.Name) || strings.ContainsFunc(input.Name, unicode.IsControl) || !validCustomerWorkspace(input.Workspace) {
		return nil, errOperatorInput
	}
	if b.foundation.customers == nil || !b.operatorMutationAllowed(ctx, b.foundation.pool) {
		return nil, ErrDenied
	}
	selected := r.URL.Query().Get("focus") == "workspace"
	open, err := workspaceOpen(ctx, b.foundation.pool, input.Workspace)
	if err == nil && open && selected {
		open, err = workspaceAcceptsInvitations(ctx, b.foundation.pool, b.foundation.broker.issuer, input.Workspace)
	}
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, errOperatorInput
	}
	userID, created, err := b.foundation.customers.EnsureUser(ctx, input.Name, input.Email, func() bool { return b.operatorMutationAllowed(ctx, b.foundation.pool) })
	if err != nil {
		return nil, err
	}
	subject := customeridentity.DexSubject(userID, customeridentity.ZitadelConnectorID)
	invitation := Invitation{b.foundation.broker.issuer, subject, input.Workspace, "client", time.Now().Add(7 * 24 * time.Hour)}
	setOperatorTarget(ctx, input.Workspace)
	notice := workspaceInvitationNotice{recipient: input.Email, lang: language(r),
		signIn: b.origin + b.visible("/portal/sign-in"), nativeSetup: created}
	expires, err := b.issueDirectoryInvitation(ctx, invitation, userID, selected, notice)
	if err != nil {
		return nil, err
	}
	return pendingInvitation{Subject: subject, Workspace: input.Workspace, Role: "client", Expires: expires}, nil
}

func (b *browser) issueDirectoryInvitation(ctx context.Context, invitation Invitation, userID string, selected bool, notice workspaceInvitationNotice) (time.Time, error) {
	if invitation.Subject == "" || !b.foundation.canInvite(invitation.Subject, "client") {
		return time.Time{}, ErrDenied
	}
	connection, release, err := b.foundation.customerIdentityConnection(ctx, invitation.Subject)
	if err != nil {
		return time.Time{}, err
	}
	defer release()
	if !b.operatorMutationAllowed(ctx, connection) {
		return time.Time{}, ErrDenied
	}
	var available bool
	if err := connection.QueryRow(ctx, `SELECT `+invitationIdentityAvailable, invitation.Issuer, invitation.Subject).Scan(&available); err != nil {
		return time.Time{}, err
	}
	if !available {
		if selected {
			return b.retryWorkspaceInvitation(ctx, connection.Begin, invitation, notice)
		}
		return time.Time{}, ErrDenied
	}
	// Confirm the exact account under the lock shared with account changes.
	// The workspace stays available during this provider call.
	if err := b.foundation.customers.VerifyUser(ctx, userID); err != nil {
		return time.Time{}, err
	}
	err = b.foundation.invite(ctx, invitation, selected, connection.Begin, func(tx pgx.Tx, token []byte) error {
		if !b.operatorMutationAllowed(ctx, tx) {
			return ErrDenied
		}
		if err := notice.queue(ctx, tx, token); err != nil {
			return err
		}
		if !b.operatorMutationAllowed(ctx, tx) {
			return ErrDenied
		}
		return nil
	})
	return invitation.Expires, err
}

func (b *browser) retryWorkspaceInvitation(ctx context.Context, begin func(context.Context) (pgx.Tx, error),
	invitation Invitation, notice workspaceInvitationNotice,
) (time.Time, error) {
	tx, err := begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockWorkspace(ctx, tx, invitation.ClientID); err != nil {
		return time.Time{}, err
	}
	var expires time.Time
	var token []byte
	err = tx.QueryRow(ctx, `SELECT i.expires_at,i.token_digest FROM portal_invitations i
 WHERE i.issuer=$1 AND i.subject=$2 AND i.client_id=$3 AND i.role='client' AND NOT i.revoked AND i.expires_at>now()
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings w WHERE w.workspace_id=$3 AND w.closed_at IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.issuer=$1 AND m.subject=$2)`,
		invitation.Issuer, invitation.Subject, invitation.ClientID).Scan(&expires, &token)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errInvitationAccess
	}
	if err != nil {
		return time.Time{}, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return time.Time{}, ErrDenied
	}
	if err := notice.queue(ctx, tx, token); err != nil {
		return time.Time{}, err
	}
	if !b.operatorMutationAllowed(ctx, tx) {
		return time.Time{}, ErrDenied
	}
	return expires, tx.Commit(ctx)
}

func workspaceAcceptsInvitations(ctx context.Context, query queryRower, issuer, workspace string) (bool, error) {
	var found bool
	err := query.QueryRow(ctx, `WITH inventory AS (`+activeWorkspaceInventory+`)
 SELECT EXISTS (SELECT FROM inventory WHERE id=$2)
 AND NOT EXISTS (SELECT FROM portal_memberships WHERE client_id=$2 AND role='operator')`, issuer, workspace).Scan(&found)
	return found, err
}

// operatorInvitationPage uses the API's exact authorization and audit boundary.
// Native forms POST to that API; their success returns to the customer inventory.
func (b *browser) operatorInvitationPage(w http.ResponseWriter, r *http.Request) {
	status, result := b.operatorCall(r, "invitations.view", func(ctx context.Context, client string) ([]byte, error) {
		if humanOperatorAuthority(ctx) == nil {
			return nil, ErrDenied
		}
		d := managementPage{pageData: b.operatorData(r, "operator-invite", client)}
		spaces, err := b.workspaces(ctx)
		if err != nil {
			return nil, err
		}
		for _, workspace := range spaces {
			if workspace.Manageable {
				d.Workspaces = append(d.Workspaces, workspace)
				if workspace.ID == r.URL.Query().Get("workspace") {
					d.InviteWorkspace, d.InviteName = workspace.ID, workspace.Name
				}
			}
		}
		if r.URL.Query().Has("workspace") && d.InviteWorkspace == "" {
			return nil, errOperatorInput
		}
		var buffer bytes.Buffer
		err = b.templates().ExecuteTemplate(&buffer, "operator.html", d)
		return buffer.Bytes(), err
	})
	if status != http.StatusOK && status != http.StatusServiceUnavailable && status != http.StatusBadRequest {
		status = http.StatusForbidden
	}
	b.writeOperatorActionPage(w, r, status, result)
}
