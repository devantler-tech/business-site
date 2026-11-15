package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionYears is how long the portal keeps a former client's records after
// their last portal membership ended: the general Danish limitation period for
// claims (forældelsesloven §3), as the privacy notice states (#614). Invoices
// and other accounting records live in Numina and Stripe, not here.
const RetentionYears = 3

// retentionDue lists the clients due for purging; with a nonempty $2, only that
// client. Every membership event of theirs was a client membership, the last
// one lies before the cutoff $1, and no membership or invitation is still open.
// An identity that moved to another client is recorded under the new client,
// so the event that follows each of the client's events counts too.
const retentionDue = `SELECT a.client_id FROM portal_membership_transitions a
 LEFT JOIN portal_membership_transitions n ON n.previous_incarnation=a.incarnation
 WHERE $2::text='' OR a.client_id=$2::text
 GROUP BY a.client_id
 HAVING bool_and(a.role='client') AND max(greatest(a.recorded_at,n.recorded_at)) < $1
 AND NOT EXISTS (SELECT 1 FROM portal_memberships m WHERE m.client_id=a.client_id AND NOT m.revoked)
 AND NOT EXISTS (SELECT 1 FROM portal_invitations i WHERE i.client_id=a.client_id AND NOT i.revoked AND i.expires_at>now())
 ORDER BY a.client_id`

// purgeRecords deletes one client's portal records in dependency order:
// billing and email first, then work, proposals, requests, contacts and
// invitations. The memberships and their history go last, in purgeOne.
var purgeRecords = []string{
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
}

// clientTables hold a client's records by client_id; the order and email
// tables hang off portal_orders.
var clientTables = []string{"portal_memberships", "portal_invitations", "portal_contacts", "portal_requests", "portal_proposals",
	"portal_proposal_visibility", "portal_proposal_reviews", "portal_proposal_acceptances", "portal_invoices", "portal_credit_transfers",
	"portal_credit_revisions", "portal_numina_contacts", "portal_orders", "portal_work_approvals", "portal_work_revocations", "portal_work_current"}

// retentionHistory lists the identities, among those with any membership
// event under client $1, whose whole membership history may go: their last
// event lies before the cutoff $2; no other client their events name still
// has records, so no other client loses the evidence that makes it due and
// no other membership remains; no remaining work approval cites any of their
// events; and no other identity's history continues from theirs.
var retentionHistory = `SELECT t.issuer,t.subject FROM portal_membership_transitions t
 WHERE EXISTS (SELECT 1 FROM portal_membership_transitions a WHERE a.client_id=$1 AND a.issuer=t.issuer AND a.subject=t.subject)
 GROUP BY t.issuer,t.subject
 HAVING max(t.recorded_at) < $2
 AND NOT EXISTS (SELECT 1 FROM portal_membership_transitions o WHERE o.issuer=t.issuer AND o.subject=t.subject AND o.client_id<>$1
   AND (` + clientHasRecords("o.client_id") + `))
 AND NOT EXISTS (SELECT 1 FROM portal_work_approvals w JOIN portal_membership_transitions x ON x.incarnation IN (w.client_incarnation,w.operator_incarnation)
   WHERE x.issuer=t.issuer AND x.subject=t.subject)
 AND NOT EXISTS (SELECT 1 FROM portal_membership_transitions r JOIN portal_membership_transitions p ON p.incarnation=r.previous_incarnation
   WHERE p.issuer=t.issuer AND p.subject=t.subject AND (r.issuer<>t.issuer OR r.subject<>t.subject))`

// clientHasRecords is a condition that the client named by column has a
// record in any of clientTables.
func clientHasRecords(column string) string {
	conditions := make([]string, len(clientTables))
	for i, table := range clientTables {
		conditions[i] = "EXISTS (SELECT 1 FROM " + table + " r WHERE r.client_id=" + column + ")"
	}
	return strings.Join(conditions, " OR ")
}

// PurgeExpiredClients deletes the portal records of every client whose
// memberships were all client memberships and all ended more than
// RetentionYears before now, and reports how many clients lost records. Each
// client goes in one transaction that first rechecks it. It needs the
// migration role: the web runtime can delete none of this history.
func PurgeExpiredClients(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int, error) {
	cutoff := now.AddDate(-RetentionYears, 0, 0)
	rows, err := pool.Query(ctx, retentionDue, cutoff, "")
	if err != nil {
		return 0, errTriageStorage
	}
	clients, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, errTriageStorage
	}
	purged := 0
	for _, client := range clients {
		deleted, err := purgeOne(ctx, pool, client, cutoff)
		if err != nil {
			return purged, err
		}
		if deleted {
			purged++
		}
	}
	return purged, nil
}

// purgeOne deletes one client's records if the client is still due. Holding
// both tables in SHARE ROW EXCLUSIVE mode keeps invitations and memberships
// from changing until it commits, so a redeemed invitation either shows in
// the recheck or waits for the purge and then finds no invitation. Locking
// invitations first follows the order in which redemption writes them.
func purgeOne(ctx context.Context, pool *pgxpool.Pool, client string, cutoff time.Time) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, errTriageStorage
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `LOCK TABLE portal_invitations, portal_memberships IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return false, errors.Join(errTriageStorage, err)
	}
	var due string
	if err = tx.QueryRow(ctx, retentionDue, cutoff, client).Scan(&due); errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, errors.Join(errTriageStorage, err)
	}
	var deleted int64
	for _, statement := range purgeRecords {
		tag, err := tx.Exec(ctx, statement, client)
		if err != nil {
			return false, errors.Join(errTriageStorage, err)
		}
		deleted += tag.RowsAffected()
	}
	// Read which histories may go before deleting the memberships records
	// their deletion as new events.
	rows, err := tx.Query(ctx, retentionHistory, client, cutoff)
	if err != nil {
		return false, errors.Join(errTriageStorage, err)
	}
	var issuers, subjects []string
	var issuer, subject string
	if _, err = pgx.ForEachRow(rows, []any{&issuer, &subject}, func() error {
		issuers, subjects = append(issuers, issuer), append(subjects, subject)
		return nil
	}); err != nil {
		return false, errors.Join(errTriageStorage, err)
	}
	for _, step := range []struct {
		statement string
		args      []any
	}{
		{`DELETE FROM portal_memberships WHERE client_id=$1`, []any{client}},
		{`DELETE FROM portal_membership_transitions WHERE (issuer,subject) IN (SELECT * FROM unnest($1::text[],$2::text[]))`, []any{issuers, subjects}},
	} {
		tag, err := tx.Exec(ctx, step.statement, step.args...)
		if err != nil {
			return false, errors.Join(errTriageStorage, err)
		}
		deleted += tag.RowsAffected()
	}
	if err = tx.Commit(ctx); err != nil {
		return false, errTriageStorage
	}
	return deleted > 0, nil
}
