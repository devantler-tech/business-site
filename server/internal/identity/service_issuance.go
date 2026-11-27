package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// readSharedServiceActor returns original recorded issuance provenance. The
// caller subsequently locks the full actor/acceptor/viewer union; this read
// deliberately takes no early membership lock and grants no current authority.
func readSharedServiceActor(ctx context.Context, tx pgx.Tx, id, client string, version, event int, hash string) (workMember, error) {
	actor := workMember{Role: "operator"}
	err := tx.QueryRow(ctx, `SELECT v.operator_issuer,v.operator_subject,v.operator_client,v.operator_incarnation::text
 FROM portal_proposal_visibility v JOIN portal_proposals p ON p.request_id=v.request_id
 AND p.client_id=v.client_id AND p.version=v.proposal_version
 JOIN portal_membership_transitions h ON h.incarnation=v.operator_incarnation
 AND h.issuer=v.operator_issuer AND h.subject=v.operator_subject AND h.client_id=v.operator_client
 AND h.role='operator' AND NOT h.revoked AND h.kind IN ('baseline','insert','update')
 WHERE v.request_id=$1 AND v.client_id=$2 AND v.proposal_version=$3 AND v.event=$4
 AND v.action='share' AND p.content_format=2 AND v.content_hash=$5 AND p.content_sha256=$5`, id, client, version, event, hash).
		Scan(&actor.Issuer, &actor.Subject, &actor.Client, &actor.Incarnation)
	if errors.Is(err, pgx.ErrNoRows) {
		return workMember{}, errProposalConflict
	}
	if err != nil {
		return workMember{}, errTriageStorage
	}
	return actor, nil
}

func lockProposalAcceptanceMembers(ctx context.Context, tx pgx.Tx, id, client string, review proposalReview, viewer workMember) error {
	wanted := []workMember{viewer}
	if review.Proposal.ContentFormat == 2 {
		actor, err := readSharedServiceActor(ctx, tx, id, client, review.Proposal.Version, review.Event, review.Hash)
		if err != nil {
			return err
		}
		wanted = append(wanted, actor)
	}
	_, matches, err := lockWorkMembers(ctx, tx, wanted)
	if err != nil {
		return err
	}
	if !matches[0] {
		return ErrDenied
	}
	for _, match := range matches[1:] {
		if !match {
			return errProposalConflict
		}
	}
	return nil
}
