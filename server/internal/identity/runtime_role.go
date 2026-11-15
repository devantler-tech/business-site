package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// CheckRuntimeRole requires identity-table DML and SELECT/INSERT on immutable
// request, proposal and invoice history. It refuses mutation or delegated grants, schema and
// database creation, public-table/database ownership and assumable privileged
// roles. It validates runtime access; it never applies migrations.
func CheckRuntimeRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT NOT r.rolsuper AND NOT r.rolbypassrls AND NOT r.rolcreatedb AND NOT r.rolcreaterole AND NOT r.rolreplication
 AND NOT has_schema_privilege(current_user,'public','CREATE')
 AND NOT has_database_privilege(current_user,current_database(),'CREATE')
 AND NOT has_parameter_privilege(current_user,'session_replication_role','SET')
 AND NOT has_table_privilege(current_user,'portal_memberships','TRUNCATE,TRIGGER,MAINTAIN')
 AND has_table_privilege(current_user,'portal_membership_transitions','SELECT')
 AND NOT has_any_column_privilege(current_user,'portal_membership_transitions','INSERT,UPDATE,REFERENCES,SELECT WITH GRANT OPTION')
 AND NOT has_table_privilege(current_user,'portal_membership_transitions','DELETE,TRUNCATE,TRIGGER,MAINTAIN')
 AND NOT has_function_privilege(current_user,'public.portal_record_membership_transition()','EXECUTE')
 AND NOT has_function_privilege(current_user,'public.portal_assign_membership_incarnation()','EXECUTE')
 AND NOT EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=r.oid AND c.relnamespace='public'::regnamespace)
 AND NOT EXISTS (SELECT 1 FROM pg_type t WHERE t.typowner=r.oid AND t.typnamespace='public'::regnamespace AND t.typtype='d')
 AND NOT EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=r.oid)
 AND NOT EXISTS (SELECT 1 FROM pg_roles predefined WHERE starts_with(predefined.rolname,'pg_') AND pg_has_role(current_user,predefined.oid,'MEMBER'))
 AND NOT EXISTS (
   SELECT 1 FROM pg_roles privileged
   WHERE privileged.rolname <> current_user
     AND pg_has_role(current_user,privileged.oid,'MEMBER')
     AND (privileged.rolsuper OR privileged.rolbypassrls OR privileged.rolcreatedb OR privileged.rolcreaterole OR privileged.rolreplication
       OR has_parameter_privilege(privileged.oid,'session_replication_role','SET')
       OR has_table_privilege(privileged.oid,'portal_memberships','TRUNCATE,TRIGGER,MAINTAIN')
       OR has_any_column_privilege(privileged.oid,'portal_membership_transitions','INSERT,UPDATE,REFERENCES,SELECT WITH GRANT OPTION')
       OR has_table_privilege(privileged.oid,'portal_membership_transitions','DELETE,TRUNCATE,TRIGGER,MAINTAIN')
       OR has_function_privilege(privileged.oid,'public.portal_record_membership_transition()','EXECUTE')
       OR has_function_privilege(privileged.oid,'public.portal_assign_membership_incarnation()','EXECUTE')
       OR has_schema_privilege(privileged.oid,'public','CREATE')
       OR has_database_privilege(privileged.oid,current_database(),'CREATE')
       OR EXISTS (SELECT 1 FROM pg_class c WHERE c.relowner=privileged.oid AND c.relnamespace='public'::regnamespace)
       OR EXISTS (SELECT 1 FROM pg_type t WHERE t.typowner=privileged.oid AND t.typnamespace='public'::regnamespace AND t.typtype='d')
       OR EXISTS (SELECT 1 FROM pg_database d WHERE d.datname=current_database() AND d.datdba=privileged.oid)
       OR EXISTS (
         SELECT 1 FROM unnest(ARRAY['portal_requests','portal_proposals','portal_proposal_visibility','portal_proposal_acceptances','portal_invoices','portal_credit_transfers','portal_credit_revisions']) AS immutable(table_name)
         WHERE has_any_column_privilege(privileged.oid,immutable.table_name,'UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION')
            OR has_table_privilege(privileged.oid,immutable.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))
       OR has_table_privilege(privileged.oid,'portal_proposal_reviews','DELETE')
       OR has_table_privilege(privileged.oid,'portal_proposal_reviews','TRUNCATE')))
 AND (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
   FROM unnest(ARRAY['portal_login_attempts','portal_invitations','portal_memberships','portal_sessions','portal_contacts']) table_name
   CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE']) privilege)
 AND NOT EXISTS (
   SELECT 1 FROM unnest(ARRAY['portal_requests','portal_proposals','portal_proposal_visibility','portal_proposal_acceptances','portal_invoices','portal_credit_transfers','portal_credit_revisions']) AS immutable(table_name)
   WHERE has_any_column_privilege(current_user,immutable.table_name,'UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION')
      OR has_table_privilege(current_user,immutable.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))
 AND has_table_privilege(current_user,'portal_requests','SELECT')
 AND has_table_privilege(current_user,'portal_requests','INSERT')
 AND has_table_privilege(current_user,'portal_proposals','SELECT')
 AND has_table_privilege(current_user,'portal_proposals','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_visibility','SELECT')
 AND has_table_privilege(current_user,'portal_proposal_visibility','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_acceptances','SELECT')
 AND has_table_privilege(current_user,'portal_proposal_acceptances','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_reviews','SELECT')
 AND has_table_privilege(current_user,'portal_invoices','SELECT')
 AND has_table_privilege(current_user,'portal_invoices','INSERT')
 AND has_table_privilege(current_user,'portal_credit_transfers','SELECT')
 AND has_table_privilege(current_user,'portal_credit_transfers','INSERT')
 AND has_table_privilege(current_user,'portal_credit_revisions','SELECT')
 AND has_table_privilege(current_user,'portal_credit_revisions','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_reviews','INSERT')
 AND has_table_privilege(current_user,'portal_proposal_reviews','UPDATE')
 AND NOT has_table_privilege(current_user,'portal_proposal_reviews','DELETE')
 AND NOT has_table_privilege(current_user,'portal_proposal_reviews','TRUNCATE')
 FROM pg_roles r WHERE r.rolname=current_user`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	if err := CheckMembershipRecorder(ctx, pool); err != nil {
		return err
	}
	if err := checkWorkApprovalRole(ctx, pool); err != nil {
		return err
	}
	return checkBillingRole(ctx, pool)
}

// Billing tables the web runtime only reads and appends to, and the two state
// tables it also updates. Every billing table refuses deletion and delegation.
var (
	billingFixedTables = []string{"portal_orders", "portal_numina_contacts", "portal_stripe_events", "portal_order_ends", "portal_card_cancellations", "portal_charge_reversals", "portal_order_withdrawals", "portal_withdrawal_cancellations", "portal_order_completions", "portal_withdrawal_settlements", "portal_mail_outbox", "portal_mail_deliveries", "portal_launch_places"}
	billingStateTables = []string{"portal_order_charges", "portal_card_checkouts"}
)

// checkBillingRole requires SELECT/INSERT on every billing table and UPDATE on
// the state tables only. Fixed tables are never changed, and nothing is
// deleted, directly, by delegation or assumable role.
func checkBillingRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT
 (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
  FROM unnest($1::text[]||$2::text[]) table_name
  CROSS JOIN unnest(ARRAY['SELECT','INSERT']) privilege)
 AND (SELECT bool_and(has_table_privilege(current_user,table_name,'UPDATE')) FROM unnest($2::text[]) table_name)
 AND NOT EXISTS (
  SELECT 1 FROM pg_roles role WHERE (role.rolname=current_user OR pg_has_role(current_user,role.oid,'MEMBER')) AND (
   EXISTS (SELECT 1 FROM unnest($1::text[]) fixed(table_name)
    WHERE has_any_column_privilege(role.oid,fixed.table_name,'UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION'))
   OR EXISTS (SELECT 1 FROM unnest($2::text[]) state(table_name)
    WHERE has_any_column_privilege(role.oid,state.table_name,'REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION'))
   OR EXISTS (SELECT 1 FROM unnest($1::text[]||$2::text[]) billing(table_name)
    WHERE has_table_privilege(role.oid,billing.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))))`, billingFixedTables, billingStateTables).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}

// checkWorkApprovalRole distinguishes immutable evidence from the two mutable
// pointer columns. Assumable or delegated authority cannot bypass this boundary.
func checkWorkApprovalRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT
 (SELECT bool_and(has_table_privilege(current_user,table_name,privilege))
  FROM unnest(ARRAY['portal_work_approvals','portal_work_revocations','portal_work_current']) table_name
  CROSS JOIN unnest(ARRAY['SELECT','INSERT']) privilege)
 AND has_column_privilege(current_user,'portal_work_current','approval_id','UPDATE')
 AND has_column_privilege(current_user,'portal_work_current','generation','UPDATE')
 AND NOT EXISTS (
  SELECT 1 FROM pg_roles role WHERE (role.rolname=current_user OR pg_has_role(current_user,role.oid,'MEMBER')) AND (
   EXISTS (SELECT 1 FROM unnest(ARRAY['portal_work_approvals','portal_work_revocations']) immutable(table_name)
    WHERE has_any_column_privilege(role.oid,immutable.table_name,'UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION')
     OR has_table_privilege(role.oid,immutable.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))
   OR has_any_column_privilege(role.oid,'portal_work_current','REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION')
   OR has_column_privilege(role.oid,'portal_work_current','request_id','UPDATE')
   OR has_column_privilege(role.oid,'portal_work_current','client_id','UPDATE')
   OR has_table_privilege(role.oid,'portal_work_current','DELETE,TRUNCATE,TRIGGER,MAINTAIN')))`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}
