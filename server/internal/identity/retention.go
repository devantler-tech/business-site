package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionYears is how long the portal keeps a former client's records after
// their last portal membership ended: the general Danish limitation period for
// claims (forældelsesloven §3), as the privacy notice states (#614). Invoices
// and other accounting records live in Numina and Stripe, not here.
const RetentionYears = 3

// purgeClient deletes one client's portal records in dependency order: billing
// and email first, then work, proposals, requests, contacts and invitations,
// and last the memberships and the membership history of identities with no
// membership left.
var purgeClient = []string{
	`DELETE FROM portal_mail_deliveries WHERE outbox_id IN (SELECT m.id FROM portal_mail_outbox m JOIN portal_orders o ON o.id=m.order_id WHERE o.client_id=$1)`,
	`DELETE FROM portal_mail_outbox WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_withdrawal_settlements WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_withdrawal_cancellations WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_order_withdrawals WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_order_completions WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_work_current WHERE client_id=$1`,
	`DELETE FROM portal_work_revocations WHERE client_id=$1`,
	`DELETE FROM portal_work_approvals WHERE client_id=$1`,
	`DELETE FROM portal_charge_reversals WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_card_cancellations WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_order_ends WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_card_checkouts WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_order_charges WHERE order_id IN (SELECT id FROM portal_orders WHERE client_id=$1)`,
	`DELETE FROM portal_orders WHERE client_id=$1`,
	`DELETE FROM portal_numina_contacts WHERE client_id=$1`,
	`DELETE FROM portal_credit_revisions WHERE client_id=$1`,
	`DELETE FROM portal_credit_transfers WHERE client_id=$1`,
	`DELETE FROM portal_invoices WHERE client_id=$1`,
	`DELETE FROM portal_proposal_reviews WHERE client_id=$1`,
	`DELETE FROM portal_proposal_acceptances WHERE client_id=$1`,
	`DELETE FROM portal_proposal_visibility WHERE client_id=$1`,
	`DELETE FROM portal_proposals WHERE client_id=$1`,
	`DELETE FROM portal_requests WHERE client_id=$1`,
	`DELETE FROM portal_contacts WHERE client_id=$1`,
	`DELETE FROM portal_invitations WHERE client_id=$1`,
	// The membership trigger records each deletion; the history of every
	// identity that has no membership left then goes too.
	`DELETE FROM portal_memberships WHERE client_id=$1`,
	`DELETE FROM portal_membership_transitions t WHERE EXISTS (SELECT 1 FROM portal_membership_transitions x WHERE x.client_id=$1 AND x.issuer=t.issuer AND x.subject=t.subject)
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.issuer=t.issuer AND m.subject=t.subject)`,
}

// PurgeExpiredClients deletes the portal records of every client whose
// memberships were all client memberships and all ended more than
// RetentionYears before now. Each client goes in one transaction. It needs the
// migration role: the web runtime can delete none of this history.
func PurgeExpiredClients(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	rows, err := pool.Query(ctx, `SELECT t.client_id FROM portal_membership_transitions t GROUP BY t.client_id
 HAVING bool_and(t.role='client') AND max(t.recorded_at) < $1
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.client_id=t.client_id AND NOT m.revoked) ORDER BY t.client_id`, now.AddDate(-RetentionYears, 0, 0))
	if err != nil {
		return 0, errTriageStorage
	}
	clients, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, errTriageStorage
	}
	for i, client := range clients {
		if err := purgeOne(ctx, pool, client); err != nil {
			return i, err
		}
	}
	return len(clients), nil
}

func purgeOne(ctx context.Context, pool *pgxpool.Pool, client string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errTriageStorage
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range purgeClient {
		if _, err := tx.Exec(ctx, statement, client); err != nil {
			return errors.Join(errTriageStorage, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errTriageStorage
	}
	return nil
}
