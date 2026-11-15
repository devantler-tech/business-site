package identity

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// formerClient seeds a client whose only member left long ago, with one
// record the purge would delete.
func formerClient(t *testing.T, migrator *pgxpool.Pool, client string) {
	t.Helper()
	ctx := t.Context()
	for _, seed := range []string{
		`INSERT INTO portal_memberships(issuer,subject,client_id,role,revoked) VALUES('https://retention.example.invalid',$1||'-member',$1,'client',true)`,
		`UPDATE portal_membership_transitions SET recorded_at=now()-interval '4 years' WHERE client_id=$1`,
		`INSERT INTO portal_numina_contacts(client_id,contact_id) VALUES($1,$1||'-contact')`,
	} {
		if _, err := migrator.Exec(ctx, seed, client); err != nil {
			t.Fatal(err)
		}
	}
}

func numinaContacts(t *testing.T, migrator *pgxpool.Pool, client string) int {
	t.Helper()
	var n int
	if err := migrator.QueryRow(t.Context(), `SELECT count(*) FROM portal_numina_contacts WHERE client_id=$1`, client).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRetentionRechecksUnderLock serializes the purge against a membership
// written after the candidate query: the purge waits for the writer, then
// finds the client active again and deletes nothing. A pending invitation
// keeps a former client as well.
func TestRetentionRechecksUnderLock(t *testing.T) {
	runtime := fixtureDB(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	formerClient(t, migrator, "returning-client")
	formerClient(t, migrator, "invited-client")
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at) VALUES(sha256('retention-invitation'),'https://retention.example.invalid','invited','invited-client','client',now()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().AddDate(-RetentionYears, 0, 0)
	redeem, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = redeem.Rollback(ctx) }()
	if _, err = redeem.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES('https://retention.example.invalid','returning','returning-client','client')`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		deleted bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		deleted, err := purgeOne(ctx, migrator, "returning-client", cutoff)
		done <- result{deleted, err}
	}()
	for waiting := 0; waiting == 0; {
		select {
		case r := <-done:
			t.Fatalf("the purge did not wait for the membership writer: %+v", r)
		case <-time.After(20 * time.Millisecond):
		}
		if err = migrator.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND relation='public.portal_memberships'::regclass`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
	}
	if err = redeem.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != nil || r.deleted {
		t.Fatalf("a client who returned during the purge was purged: %+v", r)
	}
	if n, err := PurgeExpiredClients(ctx, migrator, time.Now()); err != nil || n != 0 {
		t.Fatalf("an active or invited client was purged: %d %v", n, err)
	}
	if numinaContacts(t, migrator, "returning-client") != 1 || numinaContacts(t, migrator, "invited-client") != 1 {
		t.Fatal("records of an active or invited client were deleted")
	}
}

// TestRetentionCountsMoveToAnotherClient dates a client's last membership by
// the member's move to another client, which is recorded under the new client.
func TestRetentionCountsMoveToAnotherClient(t *testing.T) {
	_ = fixtureDB(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	formerClient(t, migrator, "left-client")
	if _, err := migrator.Exec(ctx, `UPDATE portal_memberships SET client_id='new-client',revoked=false WHERE client_id='left-client'`); err != nil {
		t.Fatal(err)
	}
	if n, err := PurgeExpiredClients(ctx, migrator, time.Now()); err != nil || n != 0 || numinaContacts(t, migrator, "left-client") != 1 {
		t.Fatalf("a client whose member moved away today was purged: %d %v", n, err)
	}
	if n := purgeAged(t, migrator); n != 1 || numinaContacts(t, migrator, "left-client") != 0 {
		t.Fatalf("the former client was not purged three years after the move: %d", n)
	}
	var members, history int
	if err := migrator.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_memberships WHERE client_id='new-client' AND NOT revoked),(SELECT count(*) FROM portal_membership_transitions WHERE client_id='new-client')`).Scan(&members, &history); err != nil || members != 1 || history == 0 {
		t.Fatalf("the moved member's membership or history was deleted: %d %d %v", members, history, err)
	}
}

// TestRetentionKeepsHistoryOtherApprovalsCite purges a former client whose
// member once approved another client's work as operator, and keeps the
// history that approval cites.
func TestRetentionKeepsHistoryOtherApprovalsCite(t *testing.T) {
	b, op := approvalFixture(t)
	_ = savedApproval(t, b, op)
	migrator := migrationDB(t)
	ctx := t.Context()
	if _, err := migrator.Exec(ctx, `UPDATE portal_memberships SET client_id='retired-operator-client',role='client',revoked=true WHERE (issuer,subject) IN (SELECT operator_issuer,operator_subject FROM portal_work_approvals)`); err != nil {
		t.Fatal(err)
	}
	if n := purgeAged(t, migrator); n != 1 {
		t.Fatalf("the former client was not purged: %d", n)
	}
	var approvals, cited, members int
	if err := migrator.QueryRow(ctx, `SELECT (SELECT count(*) FROM portal_work_approvals),
 (SELECT count(*) FROM portal_work_approvals w JOIN portal_membership_transitions x ON x.incarnation=w.operator_incarnation),
 (SELECT count(*) FROM portal_memberships WHERE client_id='retired-operator-client')`).Scan(&approvals, &cited, &members); err != nil || approvals != 1 || cited != 1 || members != 0 {
		t.Fatalf("approvals=%d cited=%d retired memberships=%d %v", approvals, cited, members, err)
	}
}

// purgeAged dates every membership event four years back and purges.
func purgeAged(t *testing.T, migrator *pgxpool.Pool) int {
	t.Helper()
	if _, err := migrator.Exec(t.Context(), `UPDATE portal_membership_transitions SET recorded_at=now()-interval '4 years'`); err != nil {
		t.Fatal(err)
	}
	n, err := PurgeExpiredClients(t.Context(), migrator, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return n
}
