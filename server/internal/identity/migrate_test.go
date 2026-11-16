package identity

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// rewindContactVerificationFixture reconstructs the pre-v8 columns and floor in
// the disposable fixture before a historical migration test rewinds its ledger.
func rewindContactVerificationFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindAcceptanceFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `ALTER TABLE portal_login_attempts DROP CONSTRAINT portal_contact_attempt_binding,
 DROP COLUMN purpose, DROP COLUMN verification_issuer, DROP COLUMN verification_subject,
 DROP COLUMN verification_client, DROP COLUMN verification_email, DROP COLUMN verification_revision;
 ALTER TABLE portal_contacts DROP CONSTRAINT portal_contact_verification_provenance,
 DROP COLUMN email_verified_at, ADD CONSTRAINT portal_contacts_email_verified_check CHECK (NOT email_verified);
 DELETE FROM portal_schema_version WHERE version=8`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-confirmation schema", err)
	}
}

func rewindAcceptanceFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindCreditsFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_invoices; ALTER TABLE portal_proposal_acceptances DROP CONSTRAINT portal_acceptance_invoice_binding; DELETE FROM portal_schema_version WHERE version=10`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-invoice schema", err)
	}
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_proposal_acceptances; DROP TABLE portal_proposal_reviews; DROP DOMAIN portal_consent_key,portal_consent_event,portal_consent_hash; DELETE FROM portal_schema_version WHERE version=9`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-consent schema", err)
	}
}

// rewindCreditsFixture removes only the disposable v11 fixture before historical controls.
func rewindCreditsFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindInvoiceTaxFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_credit_revisions,portal_credit_transfers; ALTER TABLE portal_invoices DROP CONSTRAINT portal_invoice_credit_binding; DELETE FROM portal_schema_version WHERE version=11`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-credit schema", err)
	}
}

// rewindInvoiceTaxFixture reconstructs only the disposable pre-v12 columns.
func rewindInvoiceTaxFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindMembershipHistoryFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `ALTER TABLE portal_invoices DROP COLUMN net_minor, DROP COLUMN vat_minor; DELETE FROM portal_schema_version WHERE version=12`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-tax-breakdown schema", err)
	}
}

// Only disposable historical fixtures remove v13 before replaying older schema.
func rewindMembershipHistoryFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindWorkApprovalFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TRIGGER portal_membership_incarnation ON portal_memberships;
 DROP TRIGGER portal_membership_transition ON portal_memberships;
 DROP FUNCTION portal_assign_membership_incarnation(),portal_record_membership_transition();
 DROP TABLE portal_membership_transitions;
 ALTER TABLE portal_memberships DROP COLUMN incarnation;
 DELETE FROM portal_schema_version WHERE version=13`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-membership-history schema", err)
	}
}

// Rewinds touch only the disposable migrator's synthetic schema, never deployed
// runtime grants or a production rollback. Remove new dependent tables before v13.
// rewindLaunchPlacesFixture removes only the disposable v24 launch places.
func rewindLaunchPlacesFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_launch_places;
 DELETE FROM portal_schema_version WHERE version=24`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-launch schema", err)
	}
}

// rewindMailFixture removes only the disposable v23 mail outbox and deliveries.
func rewindMailFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindLaunchPlacesFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_mail_deliveries,portal_mail_outbox;
 DELETE FROM portal_schema_version WHERE version=23`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-mail schema", err)
	}
}

// rewindEarlyStartFixture removes only the disposable v22 early start, completion
// and settlement records.
func rewindEarlyStartFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindMailFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_withdrawal_settlements,portal_order_completions;
 ALTER TABLE portal_orders DROP COLUMN early_start_at;
 DELETE FROM portal_schema_version WHERE version=22`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-early-start schema", err)
	}
}

// rewindWithdrawalsFixture removes only the disposable v21 withdrawal table.
func rewindWithdrawalsFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindEarlyStartFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_withdrawal_cancellations,portal_order_withdrawals;
 DELETE FROM portal_schema_version WHERE version=21`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-withdrawal schema", err)
	}
}

// rewindClientOrderEndsFixture removes only the disposable v20 client ends,
// with the synthetic subscription ends that follow them.
func rewindClientOrderEndsFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindWithdrawalsFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DELETE FROM portal_card_cancellations WHERE order_id IN (SELECT order_id FROM portal_order_ends WHERE source='client');
 DELETE FROM portal_order_ends WHERE source='client';
 ALTER TABLE portal_order_ends DROP CONSTRAINT portal_order_end_client, DROP CONSTRAINT portal_order_end_source, DROP COLUMN client_issuer, DROP COLUMN client_subject,
  ADD CONSTRAINT portal_order_ends_source_check CHECK(source IN ('operator','stripe'));
 DELETE FROM portal_schema_version WHERE version=20`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-client-end schema", err)
	}
}

// rewindWorkFundingFixture removes only the disposable v19 funding binding,
// with the synthetic approvals that use it.
func rewindWorkFundingFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindClientOrderEndsFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DELETE FROM portal_work_current WHERE approval_id IN (SELECT id FROM portal_work_approvals WHERE order_id IS NOT NULL);
 DELETE FROM portal_work_revocations WHERE approval_id IN (SELECT id FROM portal_work_approvals WHERE order_id IS NOT NULL);
 DELETE FROM portal_work_approvals WHERE order_id IS NOT NULL;
 ALTER TABLE portal_work_approvals DROP CONSTRAINT portal_work_approval_charge, DROP CONSTRAINT portal_work_approval_order, DROP CONSTRAINT portal_work_approval_agreement,
  DROP CONSTRAINT portal_work_approval_one_binding, DROP COLUMN order_id, DROP COLUMN charge_period, ALTER COLUMN invoice_sequence SET NOT NULL;
 ALTER TABLE portal_order_charges DROP CONSTRAINT portal_charge_work_binding;
 ALTER TABLE portal_orders DROP CONSTRAINT portal_order_work_binding;
 DELETE FROM portal_schema_version WHERE version=19`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-work-funding schema", err)
	}
}

// rewindOrderEndsFixture removes only the disposable v18 end and reversal tables.
func rewindOrderEndsFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindWorkFundingFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_charge_reversals,portal_card_cancellations,portal_order_ends;
 DELETE FROM portal_schema_version WHERE version=18`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-order-end schema", err)
	}
}

// rewindCardsFixture removes only the disposable v17 card checkout and event tables.
func rewindCardsFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindOrderEndsFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_card_checkouts,portal_stripe_events;
 DELETE FROM portal_schema_version WHERE version=17`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-card schema", err)
	}
}

// rewindOrdersFixture removes only the disposable v16 order and billing tables.
func rewindOrdersFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindCardsFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_order_charges,portal_orders,portal_numina_contacts;
 DELETE FROM portal_schema_version WHERE version=16`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-order schema", err)
	}
}

func rewindWorkApprovalFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	rewindOrdersFixture(t, pool)
	if _, err := pool.Exec(t.Context(), `ALTER TABLE portal_requests DROP CONSTRAINT portal_requests_product_check;
 ALTER TABLE portal_requests ADD CONSTRAINT portal_requests_product_check CHECK(product IN ('website','app','service'));
 DELETE FROM portal_schema_version WHERE version=15`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-other-request schema", err)
	}
	if _, err := pool.Exec(t.Context(), `DROP TABLE portal_work_current,portal_work_revocations,portal_work_approvals;
 ALTER TABLE portal_invoices DROP CONSTRAINT portal_invoice_work_binding;
 DELETE FROM portal_schema_version WHERE version=14`); err != nil {
		t.Fatal("historical fixture could not reconstruct pre-approval schema", err)
	}
}

func TestRuntimeHasNoMigrationAuthority(t *testing.T) {
	pool := fixtureDB(t)
	ctx := context.Background()
	var superuser, bypass, createRole, createDB bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper,rolbypassrls,rolcreaterole,rolcreatedb FROM pg_roles WHERE rolname=current_user`).Scan(&superuser, &bypass, &createRole, &createDB); err != nil {
		t.Fatal(err)
	}
	if superuser || bypass || createRole || createDB {
		t.Fatal("test runtime has privileged database authority")
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE portal_unexpected_runtime_ddl(id integer)`); err == nil {
		t.Fatal("runtime unexpectedly owns schema creation")
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET version=999`); err == nil {
		t.Fatal("runtime unexpectedly owns migration ledger")
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("runtime unexpectedly performed a migration")
	}
}

func TestMigrationVersionAndConcurrency(t *testing.T) {
	fixtureDB(t)
	pool := migrationDB(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := Migrate(ctx, pool); err != nil {
				t.Errorf("concurrent repeat migration: %v", err)
			}
		})
	}
	wg.Wait()
	var original string
	if err := pool.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=1`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`UPDATE portal_schema_version SET checksum='changed' WHERE version=1`, `UPDATE portal_schema_version SET version=999 WHERE version=1`} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
		if err := Migrate(ctx, pool); err == nil {
			t.Error("unknown or changed migration accepted")
		}
		if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET version=1,checksum=$1 WHERE version=1 OR version=999`, original); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal("version checks changed durable schema", err)
	}
}

// TestSessionMigrationPreservesIdentity applies later migrations to a real v1
// fixture while preserving revoked authority and refusing altered ledger history.
func TestSessionMigrationPreservesIdentity(t *testing.T) {
	fixtureDB(t)
	pool := migrationDB(t)
	rewindContactVerificationFixture(t, pool)
	ctx := context.Background()
	// Recreate the actual v1 starting point in the disposable fixture only.
	if _, err := pool.Exec(ctx, `DROP TABLE portal_proposal_visibility; DROP TABLE portal_proposals; DROP TABLE portal_requests; DROP TABLE portal_contacts; DROP TABLE portal_sessions; DELETE FROM portal_schema_version WHERE version>=2; INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true)`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := pool.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=1`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var after, client string
	var revoked bool
	if err := pool.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=1`).Scan(&after); err != nil || before != after {
		t.Fatal("v1 checksum changed", err)
	}
	if err := pool.QueryRow(ctx, `SELECT client_id,revoked FROM portal_memberships WHERE subject='existing'`).Scan(&client, &revoked); err != nil || client != "client-existing" || !revoked {
		t.Fatal("migration changed existing authority", err)
	}
	var checksum string
	if err := pool.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=2`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET checksum='changed' WHERE version=2`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("changed v2 migration accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET checksum=$1 WHERE version=2`, checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM portal_schema_version WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("missing ledger prefix accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portal_schema_version VALUES(1,$1)`, before); err != nil {
		t.Fatal(err)
	}
}

// TestContactMigrationPreservesIdentityAndSessions starts at actual v2 and checks
// that v3 preserves identity/session bytes and detects changed contact schema.
func TestContactMigrationPreservesIdentityAndSessions(t *testing.T) {
	fixtureDB(t)
	pool := migrationDB(t)
	rewindContactVerificationFixture(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE portal_proposal_visibility; DROP TABLE portal_proposals; DROP TABLE portal_requests; DROP TABLE portal_contacts; DELETE FROM portal_schema_version WHERE version>=3; INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true); INSERT INTO portal_sessions VALUES('synthetic-session',decode('001122','hex'),now()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	var identityChecksum, sessionChecksum string
	if err := pool.QueryRow(ctx, "SELECT checksum FROM portal_schema_version WHERE version=1").Scan(&identityChecksum); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT checksum FROM portal_schema_version WHERE version=2").Scan(&sessionChecksum); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM portal_memberships WHERE subject='existing' AND client_id='client-existing' AND revoked) AND EXISTS(SELECT 1 FROM portal_sessions WHERE token='synthetic-session' AND data=decode('001122','hex')) AND EXISTS(SELECT 1 FROM portal_schema_version WHERE version=1 AND checksum=$1) AND EXISTS(SELECT 1 FROM portal_schema_version WHERE version=2 AND checksum=$2)`, identityChecksum, sessionChecksum).Scan(&preserved); err != nil || !preserved {
		t.Fatal("v3 migration changed existing identity/session data", err)
	}
	var checksum string
	if err := pool.QueryRow(ctx, "SELECT checksum FROM portal_schema_version WHERE version=3").Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE portal_schema_version SET checksum='changed' WHERE version=3"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("changed contact migration accepted")
	}
	if _, err := pool.Exec(ctx, "UPDATE portal_schema_version SET checksum=$1 WHERE version=3", checksum); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}

func TestRequestMigrationPreservesExistingVersionsAndContacts(t *testing.T) {
	fixtureDB(t)
	pool := migrationDB(t)
	rewindContactVerificationFixture(t, pool)
	ctx := context.Background()
	var before string
	if err := pool.QueryRow(ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=3`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE portal_proposal_visibility; DROP TABLE portal_proposals; DROP TABLE portal_requests; DELETE FROM portal_schema_version WHERE version>=4; INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES('synthetic-existing','Existing','existing@example.invalid','',2)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var after, name, checksum string
	if err := pool.QueryRow(ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=3`).Scan(&after); err != nil || before != after {
		t.Fatal("request migration changed earlier checksums", err)
	}
	if err := pool.QueryRow(ctx, `SELECT name FROM portal_contacts WHERE client_id='synthetic-existing' AND revision=2`).Scan(&name); err != nil || name != "Existing" {
		t.Fatal("request migration changed existing profile", err)
	}
	if err := pool.QueryRow(ctx, `SELECT checksum FROM portal_schema_version WHERE version=4`).Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET checksum='changed' WHERE version=4`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err == nil {
		t.Fatal("changed request migration accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET checksum=$1 WHERE version=4`, checksum); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
