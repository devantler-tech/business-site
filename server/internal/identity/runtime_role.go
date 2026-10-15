package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRuntimeRole requires identity-table DML and SELECT/INSERT on immutable
// request, proposal and invoice history. It refuses table or column mutation, schema and
// database creation, public-table/database ownership and assumable privileged
// roles. It validates runtime access; it never applies migrations.
func CheckRuntimeRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication
 AND NOT has_schema_privilege(current_user,'public','CREATE')
 AND NOT has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relnamespace='public'::regnamespace)
 AND NOT EXISTS (SELECT 1 FROM pg_type t WHERE t.typowner=r.oid AND t.typnamespace='public'::regnamespace AND t.typtype='d')
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
       OR EXISTS (SELECT 1 FROM pg_type t WHERE t.typowner=privileged.oid AND t.typnamespace='public'::regnamespace AND t.typtype='d')
       OR EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=privileged.oid)
       OR has_any_column_privilege(privileged.oid,'portal_requests','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_requests','DELETE')
       OR has_table_privilege(privileged.oid,'portal_requests','TRUNCATE')
       OR has_any_column_privilege(privileged.oid,'portal_proposals','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_proposals','DELETE')
       OR has_table_privilege(privileged.oid,'portal_proposals','TRUNCATE')
       OR has_any_column_privilege(privileged.oid,'portal_proposal_visibility','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_proposal_visibility','DELETE')
       OR has_table_privilege(privileged.oid,'portal_proposal_visibility','TRUNCATE')
       OR has_any_column_privilege(privileged.oid,'portal_proposal_acceptances','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_proposal_acceptances','DELETE')
       OR has_table_privilege(privileged.oid,'portal_proposal_acceptances','TRUNCATE')
       OR has_any_column_privilege(privileged.oid,'portal_invoices','UPDATE')
       OR has_table_privilege(privileged.oid,'portal_invoices','DELETE')
       OR has_table_privilege(privileged.oid,'portal_invoices','TRUNCATE')
       OR has_table_privilege(privileged.oid,'portal_proposal_reviews','DELETE')
       OR has_table_privilege(privileged.oid,'portal_proposal_reviews','TRUNCATE')))
 AND (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
   FROM unnest(ARRAY['portal_login_attempts','portal_invitations','portal_memberships','portal_sessions','portal_contacts']) table_name
   CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege)
 AND has_table_privilege(current_user,'portal_requests','SELECT')
 AND has_table_privilege(current_user,'portal_requests','INSERT')
 AND NOT has_any_column_privilege(current_user,'portal_requests','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_requests','DELETE')
 AND NOT has_table_privilege(current_user,'portal_requests','TRUNCATE')
 AND has_table_privilege(current_user,'portal_proposals','SELECT')
 AND has_table_privilege(current_user,'portal_proposals','INSERT')
 AND NOT has_any_column_privilege(current_user,'portal_proposals','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_proposals','DELETE')
 AND NOT has_table_privilege(current_user,'portal_proposals','TRUNCATE')
 AND has_table_privilege(current_user,'portal_proposal_visibility','SELECT')
 AND has_table_privilege(current_user,'portal_proposal_visibility','INSERT')
 AND NOT has_any_column_privilege(current_user,'portal_proposal_visibility','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_proposal_visibility','DELETE')
 AND NOT has_table_privilege(current_user,'portal_proposal_visibility','TRUNCATE')
 AND has_table_privilege(current_user,'portal_proposal_acceptances','SELECT')
 AND has_table_privilege(current_user,'portal_proposal_acceptances','INSERT')
 AND NOT has_any_column_privilege(current_user,'portal_proposal_acceptances','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_proposal_acceptances','DELETE')
 AND NOT has_table_privilege(current_user,'portal_proposal_acceptances','TRUNCATE')
 AND has_table_privilege(current_user,'portal_proposal_reviews','SELECT')
 AND has_table_privilege(current_user,'portal_invoices','SELECT')
 AND has_table_privilege(current_user,'portal_invoices','INSERT')
 AND NOT has_any_column_privilege(current_user,'portal_invoices','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_invoices','DELETE')
 AND NOT has_table_privilege(current_user,'portal_invoices','TRUNCATE')
 AND has_table_privilege(current_user,'portal_proposal_reviews','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_reviews','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_proposal_reviews','DELETE')
 AND NOT has_table_privilege(current_user,'portal_proposal_reviews','TRUNCATE')
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}
