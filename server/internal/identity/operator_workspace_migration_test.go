package identity

import "testing"

// TestWorkspaceMigrationPreservesExistingCustomers catches a migration that
// loses workspaces whose only record is a pending invitation or a membership.
func TestWorkspaceMigrationPreservesExistingCustomers(t *testing.T) {
	pool := fixtureDB(t)
	migrator := migrationDB(t)
	if _, err := migrator.Exec(t.Context(), `DROP TABLE portal_workspaces;
 DELETE FROM portal_schema_version WHERE version=29;
 INSERT INTO portal_memberships(issuer,subject,client_id,role) VALUES('https://fixture.example.invalid','existing-customer','existing-workspace','client');
 INSERT INTO portal_invitations(token_digest,issuer,subject,client_id,role,expires_at)
 VALUES(repeat('a',32)::bytea,'https://fixture.example.invalid','invited-customer','invited-workspace','client',now()+interval '1 day')`); err != nil {
		t.Fatal("prepare previous schema", err)
	}
	if err := Migrate(t.Context(), migrator); err != nil {
		t.Fatal("upgrade workspaces", err)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM portal_workspaces WHERE id IN ('existing-workspace','invited-workspace')`).Scan(&count); err != nil || count != 2 {
		t.Fatal("workspace migration lost existing customer IDs", count, err)
	}
	if err := Migrate(t.Context(), migrator); err != nil {
		t.Fatal("repeat migration", err)
	}
}
