package identity

import "testing"

// seedEveryRelation seeds every portal relation, including real consent,
// funding and authority history. The approval fixture's client owns the
// request, order and billing rows; other clients own a contact and an
// invitation. It returns the order.
func seedEveryRelation(t *testing.T) (*browserFixture, string) {
	t.Helper()
	b, op := approvalFixture(t)
	a := savedApproval(t, b, op)
	if err := b.svc.revokeWorkApproval(t.Context(), op, proposalRequestID, workRevocationInput{Key: secret(), Approval: a.ID, Expected: 1}); err != nil {
		t.Fatal("fixture revocation unavailable", err)
	}
	client := enroll(t, b.svc, b.f, "reset-contact", "reset-contact-client", "client")
	if err := b.svc.saveContact(t.Context(), client, contactData{Name: "Fictional reset client", Email: "reset@example.invalid"}); err != nil {
		t.Fatal("fixture contact unavailable", err)
	}
	_, _ = invitedAttempt(t, b.svc, b.f, "reset-pending", "reset-pending-client", "client")
	if _, err := migrationDB(t).Exec(t.Context(), `INSERT INTO portal_sessions(token,data,expiry) VALUES('fixture-reset-session','fixture-reset-data',clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatal("fixture session unavailable", err)
	}
	migrator := migrationDB(t)
	// The approval fixture's funded order supplies the billing rows; retired
	// invoice and credit evidence is seeded through its fixture writers.
	recordFixtureInvoice(t, b)
	recordFixtureCredit(t, b, 0, 123456, "Fictional reset credit")
	var order string
	if err := migrator.QueryRow(t.Context(), `SELECT id FROM portal_orders`).Scan(&order); err != nil {
		t.Fatal("fixture order unavailable", err)
	}
	if _, err := migrator.Exec(t.Context(), `INSERT INTO portal_numina_contacts(client_id,contact_id) VALUES('reset-contact-client','fixture-contact')`); err != nil {
		t.Fatal("fixture Numina contact unavailable", err)
	}
	for _, seed := range []string{
		`INSERT INTO portal_card_checkouts(order_id,purpose,session_id,url,expires_at,attempt) VALUES($1,'one-time','cs_test_reset','https://checkout.stripe.invalid/reset',clock_timestamp(),1)`,
		`INSERT INTO portal_order_ends(order_id,last_month,source) VALUES($1,'2026-12-01','stripe')`,
		`INSERT INTO portal_card_cancellations(subscription_id,order_id,cancel_at) VALUES('sub_reset',$1,clock_timestamp())`,
		`INSERT INTO portal_charge_reversals(order_id,period,reason,reference) VALUES($1,'acceptance','refunded','ch_reset')`,
		`INSERT INTO portal_order_withdrawals(order_id,client_issuer,client_subject) VALUES($1,'https://fixture.invalid','reset-client')`,
		`INSERT INTO portal_withdrawal_cancellations(subscription_id,order_id,cancel_at) VALUES('sub_reset_withdrawal',$1,clock_timestamp())`,
		`INSERT INTO portal_withdrawal_settlements(order_id,owed_minor,operator_issuer,operator_subject) VALUES($1,0,'https://fixture.invalid','reset-operator')`,
		`INSERT INTO portal_order_completions(order_id,operator_issuer,operator_subject) VALUES($1,'https://fixture.invalid','reset-operator')`,
		`INSERT INTO portal_mail_outbox(id,order_id,kind,recipient,subject,body) VALUES('AAAAAAAAAAAAAAAAAAAAAAAAAA',$1,'order-confirmation','reset@example.invalid','Reset','Reset')`,
		`INSERT INTO portal_mail_deliveries(outbox_id) SELECT id FROM portal_mail_outbox WHERE order_id=$1`,
		`INSERT INTO portal_order_addons(order_id,addon_id,kind,quantity,unit_minor) VALUES($1,'statistics','monthly',1,7500)`,
		`INSERT INTO portal_addon_ends(order_id,addon_id,last_month,client_issuer,client_subject) VALUES($1,'statistics','2026-12-01','https://fixture.invalid','reset-client')`,
		`INSERT INTO portal_addon_removals(order_id,addon_id) VALUES($1,'statistics')`,
		`INSERT INTO portal_request_closures(request_id,client_id,operator_issuer,operator_subject) SELECT request_id,client_id,'https://fixture.invalid','reset-operator' FROM portal_orders WHERE id=$1`,
		`INSERT INTO portal_order_addons(order_id,addon_id,kind,quantity,unit_minor) VALUES($1,'contact-form','one-time',1,150000)`,
		`INSERT INTO portal_launch_places(place,client_id,order_id,percent,list_minor,discount_minor) SELECT 1,client_id,id,25,400000,100000 FROM portal_orders WHERE id=$1`,
	} {
		if _, err := migrator.Exec(t.Context(), seed, order); err != nil {
			t.Fatal("fixture billing row unavailable", err)
		}
	}
	if _, err := migrator.Exec(t.Context(), `INSERT INTO portal_stripe_events(id,type) VALUES('evt_reset','checkout.session.completed')`); err != nil {
		t.Fatal("fixture Stripe event unavailable", err)
	}
	return b, order
}

// TestFixtureResetClearsEveryRelation seeds every relation before exercising
// the next reset. It must remain serial: resetting a database is not
// concurrent fixture isolation.
func TestFixtureResetClearsEveryRelation(t *testing.T) {
	b, _ := seedEveryRelation(t)
	tables := []string{
		"portal_login_attempts", "portal_invitations", "portal_memberships",
		"portal_sessions", "portal_contacts", "portal_requests", "portal_proposals",
		"portal_proposal_visibility", "portal_proposal_reviews", "portal_proposal_acceptances",
		"portal_invoices", "portal_credit_transfers", "portal_credit_revisions",
		"portal_membership_transitions", "portal_work_approvals", "portal_work_revocations", "portal_work_current",
		"portal_orders", "portal_order_charges", "portal_numina_contacts", "portal_card_checkouts", "portal_stripe_events",
		"portal_order_ends", "portal_card_cancellations", "portal_charge_reversals", "portal_order_withdrawals", "portal_withdrawal_cancellations", "portal_order_completions", "portal_withdrawal_settlements", "portal_mail_outbox", "portal_mail_deliveries", "portal_launch_places", "portal_order_addons", "portal_request_closures", "portal_addon_ends", "portal_addon_removals",
	}
	for _, table := range tables {
		var count int
		if err := b.pool.QueryRow(t.Context(), "SELECT count(*) FROM public."+table).Scan(&count); err != nil || count == 0 {
			t.Fatalf("reset precondition absent for %s: count=%d err=%v", table, count, err)
		}
	}
	pool := fixtureDB(t)
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM public."+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("reset left records in %s: count=%d err=%v", table, count, err)
		}
	}
}
