package identity

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestInvoiceTaxDatabaseConstraints exercises the restricted runtime role, not
// only the form parser; SQL NULL cannot silently bypass the exact-sum boundary.
func TestInvoiceTaxDatabaseConstraints(t *testing.T) {
	b := invoiceFixture(t)
	recordFixtureInvoice(t, b)
	for i, tc := range []struct{ net, vat any }{
		{nil, nil}, {nil, int64(123456)}, {int64(123456), nil},
		{int64(-1), int64(123457)}, {int64(123457), int64(-1)},
		{int64(100000), int64(23455)}, {int64(1000000001), int64(0)},
		{int64(9223372036854775807), int64(1)},
	} {
		_, err := b.pool.Exec(t.Context(), `INSERT INTO portal_invoices(request_id,client_id,sequence,expected_sequence,submission_key,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,reference,purpose,currency,amount_minor,net_minor,vat_minor) SELECT request_id,client_id,2,1,$1,acceptance_id,proposal_version,visibility_event,content_hash,operator_issuer,operator_subject,$2,purpose,currency,amount_minor,$3,$4 FROM portal_invoices WHERE sequence=1`, secret(), fmt.Sprintf("TAX-INVALID-%d", i), tc.net, tc.vat)
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
			t.Fatal("database accepted invalid components or failed for another reason", err)
		}
	}
	for _, column := range []string{"net_minor", "vat_minor"} {
		if _, err := b.pool.Exec(t.Context(), `UPDATE portal_invoices SET `+column+`=0`); err == nil {
			t.Fatal("runtime mutated immutable tax metadata")
		}
	}
}

// TestInvoiceTaxMigrationRetainsLegacyReceipt preserves the historical amount
// and acceptance without inventing tax components for old invoices.
func TestInvoiceTaxMigrationRetainsLegacyReceipt(t *testing.T) {
	b := invoiceFixture(t)
	recordFixtureInvoice(t, b)
	m := migrationDB(t)
	rewindInvoiceTaxFixture(t, m)
	var before, after, ledger, retained string
	if err := m.QueryRow(t.Context(), `SELECT to_jsonb(i)::text FROM portal_invoices i`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=11`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Migrate(t.Context(), m); err != nil {
			t.Fatal("v12 could not retain the old invoice", err)
		}
	}
	if err := m.QueryRow(t.Context(), `SELECT (to_jsonb(i)-'net_minor'-'vat_minor')::text FROM portal_invoices i WHERE net_minor IS NULL AND vat_minor IS NULL`).Scan(&after); err != nil || before != after {
		t.Fatal("migration rewrote historical invoice facts", err)
	}
	if err := m.QueryRow(t.Context(), `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=11`).Scan(&retained); err != nil || ledger != retained {
		t.Fatal("migration changed the v1-v11 ledger", err)
	}
}
