package identity

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

//go:embed sessions.sql
var sessionsSchema string

// Migrate serializes ordered schema migrations across processes and records each
// checksum in the same transaction. Unknown/drifted versions fail, never reset
// data. Run with a separate migration role before constructing the runtime.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("migration database is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820517)`); err != nil {
		return fmt.Errorf("lock migration: %w", err)
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS portal_schema_version (version integer PRIMARY KEY, checksum text NOT NULL)`); err != nil {
		return fmt.Errorf("migration ledger: %w", err)
	}
	migrations := []string{schema, sessionsSchema}
	rows, err := tx.Query(ctx, `SELECT version,checksum FROM portal_schema_version ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	applied := 0
	for rows.Next() {
		var version int
		var checksum string
		if err = rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("read migration: %w", err)
		}
		if version != applied+1 || version > len(migrations) {
			rows.Close()
			return errors.New("unsupported identity schema version")
		}
		sum := sha256.Sum256([]byte(migrations[applied]))
		if checksum != hex.EncodeToString(sum[:]) {
			rows.Close()
			return errors.New("changed identity schema")
		}
		applied++
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return fmt.Errorf("read migration: %w", err)
	}
	for i := applied; i < len(migrations); i++ {
		if _, err = tx.Exec(ctx, migrations[i]); err != nil {
			return fmt.Errorf("apply identity schema: %w", err)
		}
		sum := sha256.Sum256([]byte(migrations[i]))
		if _, err = tx.Exec(ctx, `INSERT INTO portal_schema_version VALUES($1,$2)`, i+1, hex.EncodeToString(sum[:])); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
