package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRuntimeRole requires identity-table DML and immutable-request SELECT/INSERT,
// without request mutation, schema creation, table/database ownership or an assumable privileged role. It runs
// no migration. Keeping these fixture controls in this package serializes them
// with other tests touching the same disposable identity tables.
func CheckRuntimeRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication
 AND NOT has_schema_privilege(current_user,'public','CREATE')
 AND NOT has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relnamespace='public'::regnamespace)
 AND NOT EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=r.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_roles predefined WHERE starts_with(predefined.rolname,'pg_') AND pg_has_role(current_user,predefined.oid,'MEMBER'))
 AND NOT EXISTS (
   SELECT 1 FROM pg_roles privileged
   WHERE privileged.rolname <> current_user
     AND pg_has_role(current_user,privileged.oid,'MEMBER')
     AND (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreatedb OR privileged.rolcreaterole OR privileged.rolreplication
       OR has_schema_privilege(privileged.oid,'public','CREATE')
       OR has_database_privilege(privileged.oid,current_database(),'CREATE')
       OR EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=privileged.oid AND c.relnamespace='public'::regnamespace)
       OR EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=privileged.oid)
       OR has_table_privilege(privileged.oid,'portal_requests','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_requests','DELETE')
       OR has_table_privilege(privileged.oid,'portal_requests','TRUNCATE')))
 AND (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
   FROM unnest(ARRAY['portal_login_attempts','portal_invitations','portal_memberships','portal_sessions','portal_contacts']) table_name
   CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege)
 AND has_table_privilege(current_user,'portal_requests','SELECT')
 AND has_table_privilege(current_user,'portal_requests','INSERT')
 AND NOT has_table_privilege(current_user,'portal_requests','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_requests','DELETE')
 AND NOT has_table_privilege(current_user,'portal_requests','TRUNCATE')
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}
