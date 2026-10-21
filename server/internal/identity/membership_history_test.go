package identity

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Removing the database-owned recorder must fail this actual-storage control,
// even when a direct writer restores all of the original membership values.
func TestMembershipTransitionsSurviveRestoration(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	var recorded bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.portal_membership_transitions') IS NOT NULL`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if !recorded {
		t.Fatal("membership changes have no durable transition evidence")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES('https://membership.example.invalid','member','client-a','client')`); err != nil {
		t.Fatal(err)
	}
	original := membershipStamp(t, pool)
	for _, statement := range []string{
		`UPDATE portal_memberships SET revoked=true`,
		`UPDATE portal_memberships SET revoked=false`,
		`UPDATE portal_memberships SET client_id='client-b',role='operator'`,
		`UPDATE portal_memberships SET client_id='client-a',role='client'`,
		`UPDATE portal_memberships SET subject='renamed'`,
		`UPDATE portal_memberships SET subject='member'`,
	} {
		before := membershipStamp(t, pool)
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
		if after := membershipStamp(t, pool); after == before || after == original {
			t.Fatal("a membership transition reused an earlier incarnation")
		}
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_membership_transitions`).Scan(&events); err != nil || events != 7 {
		t.Fatal("direct mutations did not retain exactly seven transition events", events, err)
	}
	var linked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_membership_transitions t JOIN portal_membership_transitions prior ON prior.incarnation=t.previous_incarnation WHERE t.kind='update'`).Scan(&linked); err != nil || linked != 6 {
		t.Fatal("transition evidence lost its prior state", linked, err)
	}
}

func TestMembershipConflictAndDeletionEvidence(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	const insert = `INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES('https://membership.example.invalid','member','client-a','client')`
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	original := membershipStamp(t, pool)
	if _, err := pool.Exec(ctx, insert+` ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if membershipStamp(t, pool) != original || membershipEventCount(t, pool) != 1 {
		t.Fatal("conflict-ignore created a phantom membership event")
	}
	if _, err := pool.Exec(ctx, insert+` ON CONFLICT(issuer,subject) DO UPDATE SET revoked=false`); err != nil {
		t.Fatal(err)
	}
	if membershipStamp(t, pool) == original || membershipEventCount(t, pool) != 2 {
		t.Fatal("upsert did not record exactly its actual update")
	}
	if _, err := pool.Exec(ctx, `UPDATE portal_memberships SET incarnation=$1`, original); err != nil {
		t.Fatal(err)
	}
	if membershipStamp(t, pool) == original || membershipEventCount(t, pool) != 3 {
		t.Fatal("direct writer restored a supplied old incarnation")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM portal_memberships`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insert); err != nil {
		t.Fatal(err)
	}
	if membershipStamp(t, pool) == original || membershipEventCount(t, pool) != 5 {
		t.Fatal("delete and re-enrollment lost or reused transition evidence")
	}
	var deleted int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portal_membership_transitions WHERE kind='delete' AND previous_incarnation IS NOT NULL`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatal("deletion did not retain its final state", deleted, err)
	}
	pool.Reset()
	if membershipEventCount(t, pool) != 5 {
		t.Fatal("new database connections lost committed transition evidence")
	}
}

func TestMembershipBulkWritesAndRollback(t *testing.T) {
	pool := fixtureDB(t)
	ctx := t.Context()
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"portal_memberships"}, []string{"issuer", "subject", "client_id", "role"}, pgx.CopyFromRows([][]any{
		{"https://membership.example.invalid", "member", "client-a", "client"},
		{"https://membership.example.invalid", "other", "client-b", "operator"},
	})); err != nil {
		t.Fatal(err)
	}
	if membershipEventCount(t, pool) != 2 {
		t.Fatal("COPY bypassed membership evidence")
	}
	if _, err := pool.Exec(ctx, `MERGE INTO portal_memberships m USING (VALUES ('member'),('third')) AS source(subject) ON m.subject=source.subject WHEN MATCHED THEN UPDATE SET revoked=true WHEN NOT MATCHED THEN INSERT(issuer,subject,client_id,role) VALUES('https://membership.example.invalid',source.subject,'client-a','client')`); err != nil {
		t.Fatal(err)
	}
	if membershipEventCount(t, pool) != 4 {
		t.Fatal("MERGE bypassed an actual membership operation")
	}
	tx := beginFixtureTransaction(ctx, t, pool)
	if _, err := tx.Exec(ctx, `UPDATE portal_memberships SET revoked=false`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if membershipEventCount(t, pool) != 4 {
		t.Fatal("rolled-back membership writes left committed evidence")
	}
	for _, query := range []string{
		`INSERT INTO portal_membership_transitions SELECT * FROM portal_membership_transitions LIMIT 1`,
		`UPDATE portal_membership_transitions SET revoked=false`,
		`DELETE FROM portal_membership_transitions`,
		`TRUNCATE portal_membership_transitions`,
		`TRUNCATE portal_memberships`,
		`ALTER TABLE portal_memberships DISABLE TRIGGER portal_membership_transition`,
		`SET session_replication_role=replica`,
		`SELECT portal_record_membership_transition()`,
	} {
		if _, err := pool.Exec(ctx, query); err == nil {
			t.Fatal("restricted writer could bypass or tamper with transition evidence")
		}
	}
}

func membershipEventCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_membership_transitions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestMembershipMigrationRecordsOnlyObservedBaseline(t *testing.T) {
	fixtureDB(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	rewindMembershipHistoryFixture(t, migrator)
	var before string
	if err := migrator.QueryRow(ctx, `SELECT string_agg(checksum,',' ORDER BY version) FROM portal_schema_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Exec(ctx, `INSERT INTO portal_memberships VALUES('https://membership.example.invalid','old-member','client-a','client',true)`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, migrator); err != nil {
		t.Fatal(err)
	}
	var after string
	var baseline bool
	if err := migrator.QueryRow(ctx, `SELECT string_agg(checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=12`).Scan(&after); err != nil || before != after {
		t.Fatal("membership migration rewrote existing migration history", err)
	}
	if err := migrator.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(kind='baseline' AND previous_incarnation IS NULL AND revoked AND subject='old-member') FROM portal_membership_transitions`).Scan(&baseline); err != nil || !baseline {
		t.Fatal("migration invented pre-baseline history or changed current authority", err)
	}
}

func TestMembershipRecorderRejectsForeignTriggerInvocation(t *testing.T) {
	pool := fixtureDB(t)
	migrator := migrationDB(t)
	ctx := t.Context()
	// Even an accidental execute grant must not let a fake relation use the
	// definer's authority to manufacture evidence for an unrelated membership.
	if _, err := migrator.Exec(ctx, `GRANT EXECUTE ON FUNCTION portal_record_membership_transition() TO portal_runtime`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := migrator.Exec(context.Background(), `REVOKE EXECUTE ON FUNCTION portal_record_membership_transition() FROM portal_runtime`); err != nil {
			t.Error("fixture function grant cleanup failed")
		}
	})
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, `CREATE TEMP TABLE fake_membership (LIKE public.portal_memberships INCLUDING DEFAULTS);
 CREATE TRIGGER fake_evidence AFTER INSERT ON fake_membership FOR EACH ROW EXECUTE FUNCTION public.portal_record_membership_transition()`); err != nil {
		t.Fatal("fake-table control setup failed", err)
	}
	if _, err := connection.Exec(ctx, `INSERT INTO fake_membership(issuer,subject,client_id,role) VALUES('https://membership.example.invalid','forged','client-a','client')`); err == nil {
		t.Fatal("recorder accepted a trigger from an unrelated relation")
	}
	if membershipEventCount(t, pool) != 0 {
		t.Fatal("foreign trigger manufactured transition evidence")
	}
}

func membershipStamp(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var incarnation string
	if err := pool.QueryRow(context.Background(), `SELECT incarnation::text FROM portal_memberships`).Scan(&incarnation); err != nil || incarnation == "" {
		t.Fatal("current membership has no incarnation", err)
	}
	return incarnation
}
