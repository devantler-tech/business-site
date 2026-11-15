package identity

import (
	"testing"
	"time"
)

// TestRetentionPurgesFormerClientsAfterThreeYears deletes every portal record
// of a client whose memberships all ended more than three years ago, and
// nothing of a client who is still a member, an operator workspace or a
// client who left more recently.
func TestRetentionPurgesFormerClientsAfterThreeYears(t *testing.T) {
	_, _ = seedEveryRelation(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	var client string
	if err := migrator.QueryRow(ctx, `SELECT client_id FROM portal_orders`).Scan(&client); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_numina_contacts(client_id,contact_id) VALUES($1,'fixture-former-contact')`, client); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(ctx, `UPDATE portal_memberships SET revoked=true WHERE client_id=$1`, client); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if n, err := PurgeExpiredClients(ctx, migrator, now); err != nil || n != 0 {
		t.Fatalf("a client who left today was purged: %d %v", n, err)
	}
	// Everyone's history now lies more than three years back; only the client
	// with no active membership is due.
	if _, err := migrator.Exec(ctx, `UPDATE portal_membership_transitions SET recorded_at=$1`, now.AddDate(-RetentionYears, 0, -1)); err != nil {
		t.Fatal(err)
	}
	if n, err := PurgeExpiredClients(ctx, migrator, now.AddDate(0, 0, -2)); err != nil || n != 0 {
		t.Fatalf("a client purged before three years had passed: %d %v", n, err)
	}
	if n, err := PurgeExpiredClients(ctx, migrator, now); err != nil || n != 1 {
		t.Fatalf("the former client was not purged once: %d %v", n, err)
	}
	for _, table := range []string{"portal_memberships", "portal_membership_transitions", "portal_invitations", "portal_contacts", "portal_requests", "portal_proposals",
		"portal_proposal_visibility", "portal_proposal_reviews", "portal_proposal_acceptances", "portal_invoices", "portal_credit_transfers", "portal_credit_revisions",
		"portal_work_approvals", "portal_work_revocations", "portal_work_current", "portal_orders", "portal_numina_contacts"} {
		var left int
		if err := migrator.QueryRow(ctx, "SELECT count(*) FROM public."+table+" WHERE client_id=$1", client).Scan(&left); err != nil || left != 0 {
			t.Errorf("%s kept %d of the former client's records: %v", table, left, err)
		}
	}
	for _, table := range []string{"portal_order_charges", "portal_card_checkouts", "portal_order_ends", "portal_card_cancellations", "portal_charge_reversals",
		"portal_order_withdrawals", "portal_withdrawal_cancellations", "portal_order_completions", "portal_withdrawal_settlements", "portal_mail_outbox", "portal_mail_deliveries"} {
		var left int
		if err := migrator.QueryRow(ctx, "SELECT count(*) FROM public."+table).Scan(&left); err != nil || left != 0 {
			t.Errorf("%s kept %d of the former client's order records: %v", table, left, err)
		}
	}
	var others, operators, events int
	if err := migrator.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_memberships WHERE client_id='reset-contact-client' AND NOT revoked)+(SELECT count(*) FROM portal_contacts WHERE client_id='reset-contact-client'),
 (SELECT count(*) FROM portal_memberships WHERE role='operator'),(SELECT count(*) FROM portal_stripe_events)`).Scan(&others, &operators, &events); err != nil || others != 2 || operators == 0 || events != 1 {
		t.Fatalf("records of others were purged: client=%d operators=%d events=%d %v", others, operators, events, err)
	}
	if n, err := PurgeExpiredClients(ctx, migrator, now); err != nil || n != 0 {
		t.Fatalf("a second purge found more: %d %v", n, err)
	}
}
