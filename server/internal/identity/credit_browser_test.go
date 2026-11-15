package identity

import (
	"testing"
)

// creditFixture reuses the signed-in proposal operator while real service calls
// prepare the client's consent. Credit controls do not need unrelated extra logins.
func creditFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := acceptedProposalFixture(t)
	recordFixtureInvoice(t, b)
	return b
}

// acceptedProposalFixture shares the fixture proposal and records the client's
// acceptance, with the same synthetic decisions as the credit fixture.
func acceptedProposalFixture(t *testing.T) *browserFixture {
	t.Helper()
	b := sharingFixture(t)
	saveVisibility(t, b, visibilityForm(t, b, "0", "1", "1", "share"))
	creditFixtureFlags(t, b)
	client := enroll(t, b.svc, b.f, "credit-fixture-client", "private-client-key", "client")
	review, err := b.svc.prepareProposalReview(t.Context(), client, proposalRequestID)
	if err != nil {
		t.Fatal("fixture consent review failed", err)
	}
	if _, err := b.svc.acceptProposal(t.Context(), client, proposalRequestID, review.Key); err != nil {
		t.Fatal("fixture consent failed", err)
	}
	return b
}

// creditFixtureFlags enables only the same synthetic decisions in both setups.
func creditFixtureFlags(t *testing.T, b *browserFixture) {
	t.Helper()
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance"}, []any{true, true, true, true, true, true})
}

// TestCreditMigrationPreservesInvoices reapplies only v11 and retains v1-v10 bytes.
func TestCreditMigrationPreservesInvoices(t *testing.T) {
	creditFixture(t)
	m := migrationDB(t)
	var before, after, reference string
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=10`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	rewindCreditsFixture(t, m)
	for range 2 {
		if err := Migrate(t.Context(), m); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=10`).Scan(&after); err != nil || before != after {
		t.Fatal("prior ledger changed", err)
	}
	if err := m.QueryRow(t.Context(), `SELECT reference FROM portal_invoices`).Scan(&reference); err != nil || reference != "TEST-2026-01" {
		t.Fatal("invoice lost during credit migration", err)
	}
}

// cloneCreditInvoice creates a second internally consistent fictional client invoice
// using the privileged fixture role; global allocation is still attempted through HTTP.
func cloneCreditInvoice(t *testing.T) string {
	t.Helper()
	id, acceptance := secret(), secret()
	m := migrationDB(t)
	for _, query := range []string{
		`INSERT INTO portal_requests SELECT $1,'other-credit-client',submission_key,product,title,brief,submitted_at FROM portal_requests WHERE id=$2`,
		`INSERT INTO portal_proposals SELECT $1,'other-credit-client',version,expected_version,submission_key,operator_issuer,operator_subject,scope,deliverables,currency,one_time_minor,monthly_minor,one_time_terms,monthly_terms,prepared_at FROM portal_proposals WHERE request_id=$2`,
		`INSERT INTO portal_proposal_visibility SELECT $1,'other-credit-client',event,expected_event,proposal_version,expected_proposal,action,submission_key,operator_issuer,operator_subject,recorded_at FROM portal_proposal_visibility WHERE request_id=$2`,
	} {
		if _, err := m.Exec(t.Context(), query, id, proposalRequestID); err != nil {
			t.Fatal("second fictional client setup failed", err)
		}
	}
	if _, err := m.Exec(t.Context(), `INSERT INTO portal_proposal_acceptances SELECT $3,$1,'other-credit-client',client_issuer,client_subject,context_key,proposal_version,visibility_event,content_hash,recorded_at FROM portal_proposal_acceptances WHERE request_id=$2`, id, proposalRequestID, acceptance); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Exec(t.Context(), `INSERT INTO portal_invoices SELECT $1,'other-credit-client',sequence,expected_sequence,submission_key,$3,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,'SECOND-INVOICE',purpose,currency,amount_minor,recorded_at,net_minor,vat_minor FROM portal_invoices WHERE request_id=$2`, id, proposalRequestID, acceptance); err != nil {
		t.Fatal(err)
	}
	return "/portal/operator/requests/" + id + "/invoices/1/credits"
}
