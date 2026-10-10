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
	for _, query := range []string{`UPDATE portal_schema_version SET checksum='changed'`, `UPDATE portal_schema_version SET version=999`} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
		if err := Migrate(ctx, pool); err == nil {
			t.Error("unknown or changed migration accepted")
		}
		if _, err := pool.Exec(ctx, `UPDATE portal_schema_version SET version=1,checksum=$1`, original); err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal("version checks changed durable schema", err)
	}
}
