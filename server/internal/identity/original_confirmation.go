package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type confirmationState string

const (
	confirmationMissing        confirmationState = "missing"
	confirmationPrepared       confirmationState = "prepared"
	confirmationSenderAccepted confirmationState = "sender-accepted"
)

// originalConfirmation contains no recipient, body or provider response. Sender
// acceptance is evidence from the mail sender, never proof of inbox receipt.
type originalConfirmation struct {
	State                confirmationState
	OutboxID, BodySHA256 string
	TermsSHA256          string
	QueuedAt             time.Time
	SenderAcceptedAt     *time.Time
}

// readOriginalConfirmation reads only inside a caller's already authorized,
// request-locked transaction. It grants no authority and makes no service-start
// decision. Only the known complete-agreement producer format can supply proof;
// a legacy link-only confirmation remains missing even after its sender accepts it.
// Database failure returns no usable evidence and a sanitized, nonnil error.
func readOriginalConfirmation(ctx context.Context, heldQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, requestID, clientID, acceptanceID, orderID string) (originalConfirmation, error) {
	var result originalConfirmation
	err := heldQuery.QueryRow(ctx, `SELECT m.id,m.body_sha256,m.terms_sha256,m.queued_at,d.delivered_at
 FROM portal_orders o
 JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id AND a.request_id=o.request_id AND a.client_id=o.client_id
 JOIN portal_mail_outbox m ON m.order_id=o.id AND m.kind='order-confirmation'
 AND m.producer_format=1 AND m.acceptance_id=a.id AND m.proposal_version=a.proposal_version AND m.proposal_sha256=a.content_hash
 LEFT JOIN portal_mail_deliveries d ON d.outbox_id=m.id
 WHERE o.request_id=$1 AND o.client_id=$2 AND o.acceptance_id=$3 AND o.id=$4
 AND m.body_sha256=encode(sha256(convert_to(m.body,'UTF8')),'hex')`, requestID, clientID, acceptanceID, orderID).
		Scan(&result.OutboxID, &result.BodySHA256, &result.TermsSHA256, &result.QueuedAt, &result.SenderAcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return originalConfirmation{State: confirmationMissing}, nil
	}
	if err != nil {
		return originalConfirmation{}, errTriageStorage
	}
	result.State = confirmationPrepared
	result.QueuedAt = result.QueuedAt.UTC()
	if result.SenderAcceptedAt != nil {
		accepted := result.SenderAcceptedAt.UTC()
		result.SenderAcceptedAt = &accepted
		result.State = confirmationSenderAccepted
	}
	return result, nil
}
