package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// bindSoftwareFulfillment records one permanent software consumer for a typed
// order in the same transaction as its human approval. The order ID identifies
// the consumer across successive approvals. Future execution still requires
// independent current approval and delivery checks.
func bindSoftwareFulfillment(ctx context.Context, tx pgx.Tx, e workEvidence) error {
	var root string
	err := tx.QueryRow(ctx, `INSERT INTO portal_service_fulfillments(id,request_id,client_id,order_id,acceptance_id,proposal_version,proposal_sha256,service_kind)
 VALUES($1,$2,$3,$4,$5,$6,$7,'software') ON CONFLICT(order_id) DO NOTHING RETURNING id`,
		secret(), e.Request, e.Client, e.Order.ID, e.Acceptance, e.Proposal.Version, e.Hash).Scan(&root)
	if errors.Is(err, pgx.ErrNoRows) {
		// A separate statement sees the committed root after a conflicting insert.
		err = tx.QueryRow(ctx, `SELECT id FROM portal_service_fulfillments WHERE order_id=$1 AND request_id=$2 AND client_id=$3
 AND acceptance_id=$4 AND proposal_version=$5 AND proposal_sha256=$6 AND service_kind='software'`,
			e.Order.ID, e.Request, e.Client, e.Acceptance, e.Proposal.Version, e.Hash).Scan(&root)
		if errors.Is(err, pgx.ErrNoRows) {
			return errProposalConflict
		}
	}
	if err != nil {
		return creditWriteError(err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO portal_service_consumers(fulfillment_id,service_kind,aggregate_id)
 VALUES($1,'software',$2) ON CONFLICT DO NOTHING`, root, e.Order.ID)
	if err != nil {
		return errTriageStorage
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_service_consumers WHERE fulfillment_id=$1 AND service_kind='software' AND aggregate_id=$2)`, root, e.Order.ID).Scan(&same)
	if err != nil {
		return errTriageStorage
	}
	if !same {
		return errProposalConflict
	}
	return nil
}
