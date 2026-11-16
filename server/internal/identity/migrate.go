package identity

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

//go:embed sessions.sql
var sessionsSchema string

//go:embed contacts.sql
var contactsSchema string

//go:embed requests.sql
var requestsSchema string

//go:embed triage-index.sql
var triageIndexSchema string

//go:embed proposals.sql
var proposalsSchema string

//go:embed proposal-visibility.sql
var proposalVisibilitySchema string

//go:embed contact-verification.sql
var contactVerificationSchema string

//go:embed proposal-acceptance.sql
var proposalAcceptanceSchema string

//go:embed invoices.sql
var invoicesSchema string

//go:embed credits.sql
var creditsSchema string

//go:embed invoice-tax.sql
var invoiceTaxSchema string

//go:embed membership-history.sql
var membershipHistorySchema string

//go:embed work-approvals.sql
var workApprovalsSchema string

//go:embed other-requests.sql
var otherRequestsSchema string

//go:embed orders.sql
var ordersSchema string

//go:embed cards.sql
var cardsSchema string

//go:embed order-ends.sql
var orderEndsSchema string

//go:embed work-funding.sql
var workFundingSchema string

//go:embed client-order-ends.sql
var clientOrderEndsSchema string

//go:embed order-withdrawals.sql
var orderWithdrawalsSchema string

//go:embed early-start.sql
var earlyStartSchema string

//go:embed mail.sql
var mailSchema string

//go:embed launch-places.sql
var launchPlacesSchema string

//go:embed order-addons.sql
var orderAddonsSchema string

// runtimeGrants is the web role's complete table privilege set after the
// migrations above (v1–v25), as MIGRATIONS.md documents. A migration that adds
// or changes runtime access updates this list in the same change; web startup's
// CheckRuntimeRole independently refuses an incomplete or broader web role.
var runtimeGrants = []string{
	"SELECT,INSERT,UPDATE,DELETE ON portal_login_attempts,portal_invitations,portal_memberships,portal_sessions,portal_contacts",
	"SELECT,INSERT ON portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions",
	"SELECT,INSERT,UPDATE ON portal_proposal_reviews",
	"SELECT ON portal_membership_transitions",
	"SELECT,INSERT ON portal_work_approvals,portal_work_revocations,portal_work_current",
	"UPDATE(approval_id,generation) ON portal_work_current",
	"SELECT,INSERT ON portal_orders,portal_numina_contacts",
	"SELECT,INSERT,UPDATE ON portal_order_charges",
	"SELECT,INSERT,UPDATE ON portal_card_checkouts",
	"SELECT,INSERT ON portal_stripe_events",
	"SELECT,INSERT ON portal_order_ends,portal_card_cancellations,portal_charge_reversals,portal_order_withdrawals,portal_withdrawal_cancellations,portal_order_completions,portal_withdrawal_settlements,portal_mail_outbox,portal_mail_deliveries,portal_launch_places,portal_order_addons",
	"SELECT ON portal_schema_version",
}

// managedTables are every relation the migrations own. Granting first revokes
// the migration role's earlier grants on all of them, so re-runs converge.
const managedTables = "portal_schema_version,portal_login_attempts,portal_invitations,portal_memberships,portal_membership_transitions,portal_sessions,portal_contacts,portal_requests,portal_proposals,portal_proposal_visibility,portal_proposal_reviews,portal_proposal_acceptances,portal_invoices,portal_credit_transfers,portal_credit_revisions,portal_work_approvals,portal_work_revocations,portal_work_current,portal_orders,portal_order_charges,portal_numina_contacts,portal_card_checkouts,portal_stripe_events,portal_order_ends,portal_card_cancellations,portal_charge_reversals,portal_order_withdrawals,portal_withdrawal_cancellations,portal_order_completions,portal_withdrawal_settlements,portal_mail_outbox,portal_mail_deliveries,portal_launch_places,portal_order_addons"

// errRuntimeRole hides which eligibility condition a named web role failed.
var errRuntimeRole = errors.New("runtime role is not eligible")

var runtimeRoleName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// ValidRuntimeRoleName accepts one lowercase unquoted SQL identifier that cannot
// name PUBLIC or a predefined role. Eligibility is checked again in SQL.
func ValidRuntimeRoleName(name string) bool {
	return runtimeRoleName.MatchString(name) && name != "public" && !strings.HasPrefix(name, "pg_")
}

// Migrate serializes ordered schema migrations across processes and records each
// checksum in the same transaction. Unknown/drifted versions fail, never reset
// data. Run with a separate migration role before constructing the runtime.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, "")
}

// MigrateWithRuntimeRole migrates, then grants the existing web login exactly
// runtimeGrants and schema usage in the same transaction: a refused role or
// grant leaves neither schema changes nor privileges behind.
func MigrateWithRuntimeRole(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !ValidRuntimeRoleName(role) {
		return errRuntimeRole
	}
	return migrate(ctx, pool, role)
}

func migrate(ctx context.Context, pool *pgxpool.Pool, runtimeRole string) error {
	if pool == nil {
		return errors.New("migration database is required")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer func() {
		// Commit decides success. A committed transaction is already closed;
		// pgx discards a failed rollback's connection and returns it to the pool.
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(1869820517)`); err != nil {
		return fmt.Errorf("lock migration: %w", err)
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS portal_schema_version (version integer PRIMARY KEY, checksum text NOT NULL)`); err != nil {
		return fmt.Errorf("migration ledger: %w", err)
	}
	migrations := orderedMigrations()
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
		if checksum != migrationChecksum(migrations[applied]) {
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
		if _, err = tx.Exec(ctx, `INSERT INTO portal_schema_version VALUES($1,$2)`, i+1, migrationChecksum(migrations[i])); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}
	}
	if runtimeRole != "" {
		if err = grantRuntime(ctx, tx, runtimeRole); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// orderedMigrations lists the embedded migrations; version N is element N-1.
func orderedMigrations() []string {
	return []string{schema, sessionsSchema, contactsSchema, requestsSchema, triageIndexSchema, proposalsSchema, proposalVisibilitySchema, contactVerificationSchema, proposalAcceptanceSchema, invoicesSchema, creditsSchema, invoiceTaxSchema, membershipHistorySchema, workApprovalsSchema, otherRequestsSchema, ordersSchema, cardsSchema, orderEndsSchema, workFundingSchema, clientOrderEndsSchema, orderWithdrawalsSchema, earlyStartSchema, mailSchema, launchPlacesSchema, orderAddonsSchema}
}

// migrationChecksum is the ledger's record of one migration's exact bytes.
func migrationChecksum(migration string) string {
	sum := sha256.Sum256([]byte(migration))
	return hex.EncodeToString(sum[:])
}

// CheckSchemaCurrent refuses a store whose ledger does not yet hold every
// migration this release embeds, with unchanged checksums. The migration job and
// a new web revision start together, so this keeps the new revision from serving
// the portal until its job has migrated the store. A ledger with later versions
// is accepted: migrations only add, so an older release keeps serving while a
// newer one rolls out. It reads the ledger only and changes nothing.
func CheckSchemaCurrent(ctx context.Context, pool queryRower) error {
	migrations := orderedMigrations()
	checksums := make([]string, len(migrations))
	for i, migration := range migrations {
		checksums[i] = migrationChecksum(migration)
	}
	var current bool
	err := pool.QueryRow(ctx, `SELECT count(*) = cardinality($1::text[]) FROM portal_schema_version v
 WHERE v.version BETWEEN 1 AND cardinality($1::text[]) AND v.checksum = ($1::text[])[v.version]`, checksums).Scan(&current)
	if err != nil || !current {
		return ErrDenied
	}
	return nil
}

// grantRuntime refuses a missing, privileged or migration-assumable web login,
// then resets and grants its privileges. GRANT without grant authority can only
// warn, so schema usage is confirmed afterward; the migration role owns tables.
func grantRuntime(ctx context.Context, tx pgx.Tx, role string) error {
	var eligible bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles r WHERE r.rolname=$1 AND r.rolcanlogin
 AND NOT (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication)
 AND r.rolname<>current_user AND r.rolname<>session_user
 AND NOT pg_has_role(r.oid,current_user,'MEMBER'))`, role).Scan(&eligible)
	if err != nil || !eligible {
		return errRuntimeRole
	}
	grantee := pgx.Identifier{role}.Sanitize()
	statements := []string{"REVOKE ALL ON " + managedTables + " FROM " + grantee, "GRANT USAGE ON SCHEMA public TO " + grantee}
	for _, grant := range runtimeGrants {
		statements = append(statements, "GRANT "+grant+" TO "+grantee)
	}
	for _, statement := range statements {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("grant runtime privileges: %w", err)
		}
	}
	var usable bool
	if err = tx.QueryRow(ctx, `SELECT has_schema_privilege($1,'public','USAGE')`, role).Scan(&usable); err != nil || !usable {
		return errRuntimeRole
	}
	return nil
}
