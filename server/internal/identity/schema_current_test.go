package identity

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The web role serves only once the ledger holds every migration this release
// embeds. Each ledger change runs in a migrator transaction that rolls back, so
// the shared disposable store keeps its complete ledger.
func TestCheckSchemaCurrent(t *testing.T) {
	runtime := fixtureDB(t)
	if err := CheckSchemaCurrent(t.Context(), runtime); err != nil {
		t.Fatal("the web role refused a store migrated by this release")
	}
	migrator := migrationDB(t)
	for _, tc := range []struct {
		name, change string
		current      bool
	}{
		{"the latest migration is missing", `DELETE FROM portal_schema_version WHERE version=(SELECT max(version) FROM portal_schema_version)`, false},
		{"an earlier migration is missing", `DELETE FROM portal_schema_version WHERE version=5`, false},
		{"a migration changed", `UPDATE portal_schema_version SET checksum='changed' WHERE version=12`, false},
		{"the ledger is empty", `DELETE FROM portal_schema_version`, false},
		{"a later release migrated further", `INSERT INTO portal_schema_version SELECT max(version)+1, 'later' FROM portal_schema_version`, true},
	} {
		err := pgx.BeginFunc(t.Context(), migrator, func(tx pgx.Tx) error {
			if _, err := tx.Exec(t.Context(), tc.change); err != nil {
				t.Fatalf("ledger fixture %q failed", tc.name)
			}
			if err := CheckSchemaCurrent(t.Context(), tx); (err == nil) != tc.current || err != nil && !errors.Is(err, ErrDenied) {
				t.Fatalf("schema check was wrong when %s", tc.name)
			}
			return errRollbackLedger
		})
		if !errors.Is(err, errRollbackLedger) {
			t.Fatal("ledger fixture did not roll back")
		}
	}
	if err := CheckSchemaCurrent(t.Context(), runtime); err != nil {
		t.Fatal("a rolled-back ledger fixture changed the shared store")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if CheckSchemaCurrent(canceled, runtime) == nil {
		t.Fatal("an unreadable ledger passed the schema check")
	}
}

var errRollbackLedger = errors.New("roll back the ledger fixture")
