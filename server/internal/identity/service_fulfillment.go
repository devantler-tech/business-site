package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type serviceFulfillment struct {
	request, client, order, acceptance, quoteHash, consumer string
	quoteVersion                                            int
	kind                                                    proposalServiceKind
}

// bindServiceFulfillment records one permanent consumer for the typed order.
// Callers already hold the request lock and have verified service authority.
func bindServiceFulfillment(ctx context.Context, tx pgx.Tx, f serviceFulfillment) error {
	var root string
	err := tx.QueryRow(ctx, `INSERT INTO portal_service_fulfillments(id,request_id,client_id,order_id,acceptance_id,proposal_version,proposal_sha256,service_kind)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(order_id) DO NOTHING RETURNING id`,
		secret(), f.request, f.client, f.order, f.acceptance, f.quoteVersion, f.quoteHash, f.kind).Scan(&root)
	if errors.Is(err, pgx.ErrNoRows) {
		// A separate statement sees the committed root after a conflicting insert.
		err = tx.QueryRow(ctx, `SELECT id FROM portal_service_fulfillments WHERE order_id=$1 AND request_id=$2 AND client_id=$3
 AND acceptance_id=$4 AND proposal_version=$5 AND proposal_sha256=$6 AND service_kind=$7`,
			f.order, f.request, f.client, f.acceptance, f.quoteVersion, f.quoteHash, f.kind).Scan(&root)
		if errors.Is(err, pgx.ErrNoRows) {
			return errProposalConflict
		}
	}
	if err != nil {
		return creditWriteError(err)
	}
	tag, err := tx.Exec(ctx, `INSERT INTO portal_service_consumers(fulfillment_id,service_kind,aggregate_id)
 VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, root, f.kind, f.consumer)
	if err != nil {
		return errTriageStorage
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_service_consumers WHERE fulfillment_id=$1 AND service_kind=$2 AND aggregate_id=$3)`, root, f.kind, f.consumer).Scan(&same)
	if err != nil {
		return errTriageStorage
	}
	if !same {
		return errProposalConflict
	}
	return nil
}
