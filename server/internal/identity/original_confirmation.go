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

type confirmationQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// readOriginalConfirmation reads only inside a caller's already authorized,
// request-locked transaction. It grants no authority and makes no service-start
// decision. Only a recognized complete-agreement producer can supply proof;
// a legacy link-only confirmation remains missing even after its sender accepts it.
// Database failure returns no usable evidence and a sanitized error.
func readOriginalConfirmation(ctx context.Context, heldQuery confirmationQuery, requestID, clientID, acceptanceID, orderID string) (originalConfirmation, error) {
	var result originalConfirmation
	var format, version int
	var proposalHash string
	err := heldQuery.QueryRow(ctx, `SELECT m.id,m.body_sha256,m.terms_sha256,m.queued_at,d.delivered_at,
 m.producer_format,m.proposal_version,m.proposal_sha256
 FROM portal_orders o
 JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id AND a.request_id=o.request_id AND a.client_id=o.client_id
 JOIN portal_proposals p ON p.request_id=a.request_id AND p.client_id=a.client_id AND p.version=a.proposal_version
 JOIN portal_mail_outbox m ON m.order_id=o.id AND m.kind='order-confirmation'
 AND m.acceptance_id=a.id AND m.proposal_version=a.proposal_version AND m.proposal_sha256=a.content_hash
 LEFT JOIN portal_mail_deliveries d ON d.outbox_id=m.id
 WHERE o.request_id=$1 AND o.client_id=$2 AND o.acceptance_id=$3 AND o.id=$4
 AND ((m.producer_format=1 AND p.content_format=1 AND o.service_content_format IS NULL)
   OR (m.producer_format=2 AND p.content_format=2 AND p.service_kind='software' AND o.service_content_format=2)
   OR (m.producer_format=3 AND p.content_format=2 AND p.service_kind='appointment' AND o.service_content_format=2
       AND o.appointment_reservation_id IS NOT NULL AND o.appointment_revision IS NOT NULL))
 AND m.body_sha256=encode(sha256(convert_to(m.body,'UTF8')),'hex')`, requestID, clientID, acceptanceID, orderID).
		Scan(&result.OutboxID, &result.BodySHA256, &result.TermsSHA256, &result.QueuedAt, &result.SenderAcceptedAt, &format, &version, &proposalHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return originalConfirmation{State: confirmationMissing}, nil
	}
	if err != nil {
		return originalConfirmation{}, errTriageStorage
	}
	if format == 2 || format == 3 {
		source, err := readOrderConfirmationSource(ctx, heldQuery, clientID, orderID, acceptanceID)
		if errors.Is(err, errProposalConflict) {
			return originalConfirmation{State: confirmationMissing}, nil
		}
		if err != nil {
			return originalConfirmation{}, err
		}
		if source.ContentFormat != 2 || source.ProducerFormat != format ||
			source.ProposalVersion != version || source.ProposalSHA256 != proposalHash {
			return originalConfirmation{State: confirmationMissing}, nil
		}
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
