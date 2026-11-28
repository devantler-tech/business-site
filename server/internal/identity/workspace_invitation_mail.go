package identity

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/devantler-tech/business-site/server/internal/mail"
	"github.com/jackc/pgx/v5"
)

type workspaceInvitationNotice struct {
	recipient, lang, signIn string
	nativeSetup             bool
}

func (notice workspaceInvitationNotice) queue(ctx context.Context, tx pgx.Tx, token []byte) error {
	var to, signIn any
	if !notice.nativeSetup {
		to, signIn = notice.recipient, notice.signIn
	}
	_, err := tx.Exec(ctx, `INSERT INTO portal_workspace_invitation_notices(invitation_digest,native_setup,recipient,language,sign_in_url)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(invitation_digest) DO NOTHING`, token, notice.nativeSetup, to, notice.lang, signIn)
	return err
}

const pendingWorkspaceInvitationMail = `FROM portal_workspace_invitation_notices n
 JOIN portal_invitations i ON i.token_digest=n.invitation_digest
 WHERE NOT n.native_setup AND i.role='client' AND NOT i.revoked AND i.expires_at>clock_timestamp()
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_settings w WHERE w.workspace_id=i.client_id AND w.closed_at IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM portal_workspace_invitation_deliveries d WHERE d.invitation_digest=n.invitation_digest)`

func (s *Foundation) sendWorkspaceInvitationNotices(ctx context.Context, conn *pgx.Conn, m Mailer) error {
	rows, err := conn.Query(ctx, `SELECT i.token_digest `+pendingWorkspaceInvitationMail+`
 ORDER BY n.last_attempt_at NULLS FIRST,i.expires_at,i.token_digest LIMIT 20`)
	if err != nil {
		return errTriageStorage
	}
	tokens, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return errTriageStorage
	}
	var failures error
	for _, token := range tokens {
		if !s.ready(ctx) {
			break
		}
		failures = errors.Join(failures, sendWorkspaceInvitationNotice(ctx, conn, m, token))
	}
	return failures
}

func sendWorkspaceInvitationNotice(ctx context.Context, conn *pgx.Conn, m Mailer, token []byte) error {
	// A redemption or revocation must finish before this check or wait for the
	// bounded delivery attempt. No mail is sent for an already invalid invite.
	delivery, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	tx, err := conn.Begin(delivery)
	if err != nil {
		return errTriageStorage
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var notice workspaceInvitationNotice
	err = tx.QueryRow(delivery, `SELECT n.recipient,n.language,n.sign_in_url `+pendingWorkspaceInvitationMail+`
 AND i.token_digest=$1 FOR UPDATE OF i`, token).Scan(&notice.recipient, &notice.lang, &notice.signIn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return errTriageStorage
	}
	if _, err := tx.Exec(ctx, `UPDATE portal_workspace_invitation_notices SET last_attempt_at=clock_timestamp()
 WHERE invitation_digest=$1`, token); err != nil {
		return errTriageStorage
	}
	sendErr := m.Send(delivery, notice.message(token))
	if sendErr == nil {
		if _, err := tx.Exec(ctx, `INSERT INTO portal_workspace_invitation_deliveries(invitation_digest) VALUES($1)`, token); err != nil {
			return errTriageStorage
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return sendErr
}

func (notice workspaceInvitationNotice) message(token []byte) mail.Message {
	subject := "Your Devantler Tech workspace invitation"
	body := "You’ve been invited to a workspace in the Devantler Tech client portal. To open it, sign in with your existing account.\n\n"
	if notice.lang == "da" {
		subject = "Din invitation til et arbejdsområde hos Devantler Tech"
		body = "Du er inviteret til et arbejdsområde i Devantler Techs kundeportal. Log ind med din eksisterende konto for at åbne det.\n\n"
	}
	return mail.Message{ID: "workspace-invitation-" + hex.EncodeToString(token), To: notice.recipient,
		Subject: subject, Body: body + notice.signIn + "\n"}
}
