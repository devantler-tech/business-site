package identity

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

// Migrate serializes the first schema migration across processes and records its
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
	sum := sha256.Sum256([]byte(schema))
	expected := hex.EncodeToString(sum[:])
	var version int
	var checksum string
	err = tx.QueryRow(ctx, `SELECT version,checksum FROM portal_schema_version ORDER BY version DESC LIMIT 1`).Scan(&version, &checksum)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err = tx.Exec(ctx, schema); err != nil {
			return fmt.Errorf("apply identity schema: %w", err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO portal_schema_version VALUES(1,$1)`, expected); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read migration: %w", err)
	} else if version != 1 || checksum != expected {
		return errors.New("unsupported or changed identity schema")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}
