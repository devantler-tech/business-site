package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRuntimeRole requires every identity-table DML privilege, without schema
// creation, table/database ownership or an assumable privileged role. It runs
// no migration. Keeping these fixture controls in this package serializes them
// with other tests touching the same disposable identity tables.
func CheckRuntimeRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole
 AND NOT has_schema_privilege(current_user,'public','CREATE')
 AND NOT has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relnamespace='public'::regnamespace)
 AND NOT EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=r.oid)
 AND NOT EXISTS (
   SELECT 1 FROM pg_roles privileged
   WHERE privileged.rolname <> current_user
     AND pg_has_role(current_user,privileged.oid,'MEMBER')
     AND (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreatedb OR privileged.rolcreaterole
       OR has_schema_privilege(privileged.oid,'public','CREATE')
       OR has_database_privilege(privileged.oid,current_database(),'CREATE')
       OR EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=privileged.oid AND c.relnamespace='public'::regnamespace)
       OR EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=privileged.oid)))
 AND (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
   FROM unnest(ARRAY['portal_login_attempts','portal_invitations','portal_memberships','portal_sessions']) table_name
   CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege)
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}
