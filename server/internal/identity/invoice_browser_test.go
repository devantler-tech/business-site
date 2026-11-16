package identity

import (
	"testing"
)

// invoiceFixture provisions fictional accepted scope and a current operator over TLS and SQL.
func invoiceFixture(t *testing.T) *browserFixture {
	t.Helper()
	b, _, _ := acceptedOperatorFixture(t)
	setFixturePortalFlags(t, b, []string{"portal-identity", "portal-requests", "portal-triage", "portal-proposals", "portal-proposal-sharing", "portal-proposal-acceptance"}, []any{true, true, true, true, true, true})
	triageSignIn(t, b, "invoice-operator", "operator-workspace", "operator")
	return b
}

// TestInvoiceMigrationPreservesConsent reapplies v10 without changing prior consent or checksums.
func TestInvoiceMigrationPreservesConsent(t *testing.T) {
	recordFixtureInvoice(t, invoiceFixture(t))
	migrator := migrationDB(t)
	var before, after, acceptance string
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=9`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := migrator.QueryRow(t.Context(), `SELECT id FROM portal_proposal_acceptances`).Scan(&acceptance); err != nil {
		t.Fatal(err)
	}
	rewindCreditsFixture(t, migrator)
	if _, err := migrator.Exec(t.Context(), `DROP TABLE portal_invoices; ALTER TABLE portal_proposal_acceptances DROP CONSTRAINT portal_acceptance_invoice_binding; DELETE FROM portal_schema_version WHERE version=10`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(t.Context(), migrator); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrator.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=9`).Scan(&after); err != nil || before != after {
		t.Fatal("invoice migration changed prior schema ledger", err)
	}
	var retained bool
	if err := migrator.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM portal_proposal_acceptances WHERE id=$1) AND (SELECT count(*)=29 FROM portal_schema_version)`, acceptance).Scan(&retained); err != nil || !retained {
		t.Fatal("invoice migration changed consent or omitted its own version", err)
	}
}
