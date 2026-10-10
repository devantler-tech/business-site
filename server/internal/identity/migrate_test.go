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

func TestSessionMigrationPreservesIdentity(t *testing.T) {
	fixtureDB(t)
	pool := migrationDB(t)
	ctx := context.Background()
	// Recreate the actual v1 starting point in the disposable fixture only.
	if _, err := pool.Exec(ctx, `DROP TABLE portal_sessions; DELETE FROM portal_schema_version WHERE version=2; INSERT INTO portal_memberships VALUES('https://fixture.invalid','existing','client-existing','client',true)`); err != nil {
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
