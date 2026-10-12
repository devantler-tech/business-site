package identity

import (
	"context"
	"sync"
	"testing"
)

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
	ctx := context.Background()
	// Recreate the actual v1 starting point in the disposable fixture only.
	if _, err := pool.Exec(ctx, `DROP TABLE portal_requests; DROP TABLE portal_contacts; DROP TABLE portal_sessions; DELETE FROM portal_schema_version WHERE version>=2; INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true)`); err != nil {
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
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE portal_requests; DROP TABLE portal_contacts; DELETE FROM portal_schema_version WHERE version>=3; INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true); INSERT INTO portal_sessions VALUES('synthetic-session',decode('001122','hex'),now()+interval '1 hour')`); err != nil {
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
	ctx := context.Background()
	var before string
	if err := pool.QueryRow(ctx, `SELECT string_agg(version::text||':'||checksum,',' ORDER BY version) FROM portal_schema_version WHERE version<=3`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE portal_requests; DELETE FROM portal_schema_version WHERE version>=4; INSERT INTO portal_contacts(client_id,name,email,company,revision) VALUES('synthetic-existing','Existing','existing@example.invalid','',2)`); err != nil {
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
