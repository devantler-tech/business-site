package runtimeapp

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/business-site/server/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMigration is the entire public diagnostic: private paths, connection values
// and database errors never become command output.
var ErrMigration = errors.New("portal schema migration failed")

// MigrateDatabase runs the existing embedded transaction under a separate,
// constrained role. This explicit job neither serves HTTP nor enables identity.
func MigrateDatabase(ctx context.Context, filename string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil || !filepath.IsAbs(filename) {
		return ErrMigration
	}
	connection, err := migrationConnection(filename)
	if err != nil {
		return ErrMigration
	}
	cfg, err := migrationConfig(connection)
	if err != nil {
		return ErrMigration
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// Do not inherit service/PGOPTIONS startup parameters from the operator host.
	cfg.ConnConfig.RuntimeParams = map[string]string{"search_path": "public", "statement_timeout": "5000", "lock_timeout": "1000", "application_name": "business-site-migration"}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return ErrMigration
	}
	defer pool.Close()
	if !migrationRoleAllowed(ctx, pool, false) || identity.Migrate(ctx, pool) != nil || !migrationRoleAllowed(ctx, pool, true) {
		return ErrMigration
	}
	return nil
}

// migrationRoleAllowed checks authority before DDL and a complete owned schema
// afterward. SELECT grants and a current ledger cannot substitute for ownership;
// the portal_ namespace also covers managed indexes and later managed relations.
func migrationRoleAllowed(ctx context.Context, pool *pgxpool.Pool, complete bool) bool {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT current_user=session_user
 AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication
 AND has_schema_privilege(current_user,'public','CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_roles other WHERE other.oid<>r.oid AND other.rolname<>'pg_database_owner' AND pg_has_role(current_user,other.oid,'MEMBER'))
 AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.relnamespace='public'::regnamespace AND starts_with(c.relname,'portal_') AND c.relowner<>r.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_type t WHERE t.typnamespace='public'::regnamespace AND starts_with(t.typname,'portal_') AND t.typtype='d' AND t.typowner<>r.oid)
 AND (NOT $1::boolean OR (
   SELECT bool_and(c.oid IS NOT NULL AND c.relowner=r.oid AND c.relkind IN ('r','p'))
   FROM unnest(ARRAY['portal_schema_version','portal_login_attempts','portal_invitations','portal_memberships','portal_sessions','portal_contacts','portal_requests','portal_proposals','portal_proposal_visibility','portal_proposal_reviews','portal_proposal_acceptances','portal_invoices','portal_credit_transfers','portal_credit_revisions']) managed(name)
   LEFT JOIN pg_class c ON c.relname=managed.name AND c.relnamespace='public'::regnamespace))
 AND (NOT $1::boolean OR (
   SELECT bool_and(t.oid IS NOT NULL AND t.typtype='d' AND t.typowner=r.oid AND t.typbasetype=managed.base::regtype)
   FROM (VALUES ('portal_consent_key','text'),('portal_consent_event','integer'),('portal_consent_hash','text')) managed(name,base)
   LEFT JOIN pg_type t ON t.typname=managed.name AND t.typnamespace='public'::regnamespace))
 FROM pg_roles r WHERE r.rolname=current_user`, complete).Scan(&allowed)
	return err == nil && allowed
}

// migrationConfig admits a complete single-target URI before using pgx's TLS
// parser. Core identity/routing cannot come from environment or query overrides.
func migrationConfig(connection string) (*pgxpool.Config, error) {
	// The command clears ambient settings. Direct internal callers fail closed
	// rather than letting the library read an environment-selected service file.
	if os.Getenv("PGSERVICE") != "" || os.Getenv("PGSERVICEFILE") != "" {
		return nil, ErrMigration
	}
	u, err := url.Parse(connection)
	if err != nil || u.Scheme != "postgres" && u.Scheme != "postgresql" || u.User == nil || u.User.Username() == "" || u.Hostname() == "" || strings.Contains(u.Host, ",") || u.Fragment != "" || strings.Count(u.Path, "/") != 1 || len(u.Path) < 2 {
		return nil, ErrMigration
	}
	password, present := u.User.Password()
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if !present || password == "" || err != nil || port == 0 {
		return nil, ErrMigration
	}
	database := strings.TrimPrefix(u.Path, "/")
	if database == "postgres" || database == "template0" || database == "template1" {
		return nil, ErrMigration
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || query.Get("sslmode") != "verify-full" || !filepath.IsAbs(query.Get("sslrootcert")) {
		return nil, ErrMigration
	}
	for key, values := range query {
		if key != "sslmode" && key != "sslrootcert" || len(values) != 1 {
			return nil, ErrMigration
		}
	}
	certificate, err := migrationCertificate(query.Get("sslrootcert"))
	if err != nil {
		return nil, ErrMigration
	}
	// pgx still owns TLS parsing. Give it our bounded regular-file snapshot,
	// never the projected path again or implicit home-directory client keys.
	ca, err := os.CreateTemp("", "business-site-migration-ca-")
	if err != nil {
		return nil, ErrMigration
	}
	defer func() {
		// This contains only the operator's trust material, no connection values.
		// Best-effort cleanup cannot change the parsed TLS or migration verdict.
		_ = ca.Close()
		_ = os.Remove(ca.Name())
	}()
	if _, err := ca.Write(certificate); err != nil || ca.Close() != nil {
		return nil, ErrMigration
	}
	if _, err := trustedMigrationPath(ca.Name()); err != nil {
		return nil, ErrMigration
	}
	query.Set("sslrootcert", ca.Name())
	query.Set("sslcert", "")
	query.Set("sslkey", "")
	query.Set("sslpassword", "")
	query.Set("target_session_attrs", "any")
	query.Set("sslnegotiation", "postgres")
	u.RawQuery = query.Encode()
	cfg, err := pgxpool.ParseConfig(u.String())
	if err != nil || cfg.ConnConfig.Host != u.Hostname() || cfg.ConnConfig.Port != uint16(port) || cfg.ConnConfig.Database != database || cfg.ConnConfig.User != u.User.Username() || cfg.ConnConfig.Password != password || cfg.ConnConfig.TLSConfig == nil || cfg.ConnConfig.TLSConfig.InsecureSkipVerify || cfg.ConnConfig.TLSConfig.ServerName != u.Hostname() || len(cfg.ConnConfig.TLSConfig.Certificates) != 0 || len(cfg.ConnConfig.Fallbacks) != 0 {
		return nil, ErrMigration
	}
	return cfg, nil
}

// migrationCertificate reads public trust material, not a credential. Preserve
// the secret reader's same-descriptor/nonblocking confinement while permitting
// ordinary readable certificate permissions and a bounded 64 KiB CA bundle.
func migrationCertificate(filename string) ([]byte, error) {
	return readMigrationInput(filename, 65536, 0022)
}
