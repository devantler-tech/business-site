package runtimeapp

import (
	"context"
	"errors"
	"net/url"
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
	connection, err := readSecret(filename)
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
	var allowed bool
	err = pool.QueryRow(ctx, `SELECT current_user=session_user
 AND NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication
 AND has_schema_privilege(current_user,'public','CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_roles other WHERE other.oid<>r.oid AND other.rolname<>'pg_database_owner' AND pg_has_role(current_user,other.oid,'MEMBER'))
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&allowed)
	if err != nil || !allowed || identity.Migrate(ctx, pool) != nil {
		return ErrMigration
	}
	return nil
}

// migrationConfig admits a complete single-target URI before using pgx's TLS
// parser. Core identity/routing cannot come from environment or query overrides.
func migrationConfig(connection string) (*pgxpool.Config, error) {
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
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil || cfg.ConnConfig.Host != u.Hostname() || cfg.ConnConfig.Port != uint16(port) || cfg.ConnConfig.Database != database || cfg.ConnConfig.User != u.User.Username() || cfg.ConnConfig.Password != password || cfg.ConnConfig.TLSConfig == nil || cfg.ConnConfig.TLSConfig.InsecureSkipVerify || cfg.ConnConfig.TLSConfig.ServerName != u.Hostname() || len(cfg.ConnConfig.TLSConfig.Certificates) != 0 || len(cfg.ConnConfig.Fallbacks) != 0 {
		return nil, ErrMigration
	}
	return cfg, nil
}
