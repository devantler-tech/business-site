package runtimeapp

import (
	"testing"
)

// TestActualExecutableRetentionPurge runs the exclusive purge as the migration
// role against a migrated database: a former client whose memberships ended
// more than three years ago goes, a current one stays, and anything but its
// two arguments is refused before connecting.
func TestActualExecutableRetentionPurge(t *testing.T) {
	file, pool, _ := migrationCommandFixture(t)
	binary := buildMigrationCommand(t)
	if output, err := runMigrationCommand(t, binary, "--retention-purge", "--migration-database-file", file); err == nil || output != "portal retention purge failed\n" {
		t.Fatalf("a schema not yet migrated was purged: %q", output)
	}
	if output, err := runMigrationCommand(t, binary, "--migrate", "--migration-database-file", file); err != nil || output != "portal schema migration completed\n" {
		t.Fatal("fixture migration failed")
	}
	for _, statement := range []string{
		"INSERT INTO portal_memberships VALUES('https://fixture.invalid','former','client-former','client',true)",
		"INSERT INTO portal_memberships VALUES('https://fixture.invalid','current','client-current','client',false)",
		"INSERT INTO portal_contacts(client_id,name,email,revision) VALUES('client-former','Fictional former client','former@example.invalid',1)",
		"UPDATE portal_membership_transitions SET recorded_at=now()-interval '3 years 1 day'",
	} {
		if _, err := pool.Exec(t.Context(), statement); err != nil {
			t.Fatal("retention fixture write failed", err)
		}
	}
	for _, args := range [][]string{
		{"--retention-purge"},
		{"--retention-purge", "--migration-database-file", file, "--migrate"},
		{"--retention-purge", "--migration-database-file", file, "--runtime-role", "portal_web"},
		{"--retention-purge", "--retention-purge", "--migration-database-file", file},
		{"--retention-purge", "--migration-database-file", "relative"},
	} {
		if output, err := runMigrationCommand(t, binary, args...); err == nil || output != "portal retention purge failed\n" {
			t.Errorf("%v was admitted: %q", args, output)
		}
	}
	output, err := runMigrationCommand(t, binary, "--retention-purge", "--migration-database-file", file)
	if err != nil || output != "portal retention purge completed: 1 former clients\n" {
		t.Fatalf("purge did not complete: %q %v", output, err)
	}
	var former, current int
	if err := pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM portal_contacts WHERE client_id='client-former')+(SELECT count(*) FROM portal_memberships WHERE client_id='client-former'),(SELECT count(*) FROM portal_memberships WHERE client_id='client-current')").Scan(&former, &current); err != nil || former != 0 || current != 1 {
		t.Fatalf("purge kept the former client or removed the current one: %d %d %v", former, current, err)
	}
}
