package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// orderConfirmationSource is the immutable renderer projection. Recorded human
// lineage is verified here without granting current authority or locking a late
// membership. Actor and session identifiers never enter the email projection.
type orderConfirmationSource struct {
	ContentFormat   int
	ProposalVersion int
	ProposalSHA256  string
	Service         proposalServiceTerms
}

func readOrderConfirmationSource(ctx context.Context, tx confirmationQuery, clientID, orderID, acceptanceID string) (orderConfirmationSource, error) {
	if tx == nil || ctx.Err() != nil {
		return orderConfirmationSource{}, errTriageStorage
	}
	var proposal projectProposal
	var acceptedHash, sharedHash, actorClient, actorIncarnation, acceptorIncarnation string
	var recorded bool
	var orderFormat *int
	dest := proposalScanTargets(&proposal)
	dest = append(dest, &acceptedHash, &sharedHash, &actorClient, &actorIncarnation, &acceptorIncarnation, &recorded, &orderFormat)
	err := tx.QueryRow(ctx, `SELECT `+proposalColumns+`,a.content_hash,COALESCE(v.content_hash,''),COALESCE(v.operator_client,''),COALESCE(v.operator_incarnation::text,''),COALESCE(a.client_incarnation::text,''),
 EXISTS(SELECT 1 FROM portal_membership_transitions h WHERE h.incarnation=v.operator_incarnation
   AND h.issuer=v.operator_issuer AND h.subject=v.operator_subject AND h.client_id=v.operator_client
   AND h.role='operator' AND NOT h.revoked AND h.kind IN ('baseline','insert','update'))
 AND EXISTS(SELECT 1 FROM portal_membership_transitions h WHERE h.incarnation=a.client_incarnation
   AND h.issuer=a.client_issuer AND h.subject=a.client_subject AND h.client_id=a.client_id
   AND h.role='client' AND NOT h.revoked AND h.kind IN ('baseline','insert','update')),
 o.service_content_format
 FROM portal_orders o JOIN portal_proposal_acceptances a ON a.id=o.acceptance_id
   AND a.request_id=o.request_id AND a.client_id=o.client_id
 JOIN portal_proposals p ON p.request_id=a.request_id AND p.client_id=a.client_id AND p.version=a.proposal_version
 JOIN portal_proposal_visibility v ON v.request_id=a.request_id AND v.client_id=a.client_id
   AND v.event=a.visibility_event AND v.proposal_version=a.proposal_version AND v.action='share'
 WHERE o.client_id=$1 AND o.id=$2 AND a.id=$3`, clientID, orderID, acceptanceID).Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return orderConfirmationSource{}, errProposalConflict
	}
	if err != nil {
		return orderConfirmationSource{}, errTriageStorage
	}
	hash, err := proposalContentHash(proposal)
	if err != nil || hash != acceptedHash {
		return orderConfirmationSource{}, errProposalConflict
	}
	if proposal.ContentFormat == 2 {
		if orderFormat == nil || *orderFormat != 2 || !recorded || actorClient == "" || actorIncarnation == "" || acceptorIncarnation == "" || hash != sharedHash || hash != proposal.ContentSHA256 {
			return orderConfirmationSource{}, errProposalConflict
		}
	} else if proposal.ContentFormat != 1 || orderFormat != nil {
		return orderConfirmationSource{}, errProposalConflict
	}
	return orderConfirmationSource{ContentFormat: proposal.ContentFormat, ProposalVersion: proposal.Version, ProposalSHA256: hash, Service: proposal.Service}, nil
}
