package identity

import "testing"

// TestFixtureResetClearsEveryRelation seeds all seventeen relations, including
// real consent, funding and authority history, before exercising the next reset.
// It must remain serial: resetting a database is not concurrent fixture isolation.
func TestFixtureResetClearsEveryRelation(t *testing.T) {
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
	tables := []string{
		"portal_login_attempts", "portal_invitations", "portal_memberships",
		"portal_sessions", "portal_contacts", "portal_requests", "portal_proposals",
		"portal_proposal_visibility", "portal_proposal_reviews", "portal_proposal_acceptances",
		"portal_invoices", "portal_credit_transfers", "portal_credit_revisions",
		"portal_membership_transitions", "portal_work_approvals", "portal_work_revocations", "portal_work_current",
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
