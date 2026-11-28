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
 AND NOT EXISTS (
   SELECT 1 FROM pg_roles candidate
   WHERE (candidate.rolname=current_user OR pg_has_role(current_user,candidate.oid,'MEMBER'))
     AND (has_function_privilege(candidate.oid,'public.portal_guard_hosting_period_operator()','EXECUTE')
       OR EXISTS (
         SELECT 1 FROM (VALUES ('public.portal_hosting_entitlements'),('public.portal_hosting_period_revisions'),
                               ('public.portal_hosting_period_heads')) AS hosting(table_name)
         WHERE has_any_column_privilege(candidate.oid,hosting.table_name,'SELECT,INSERT,UPDATE,REFERENCES')
            OR has_table_privilege(candidate.oid,hosting.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))))
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
	if err := checkWorkspaceRole(ctx, pool); err != nil {
		return err
	}
	return checkBillingRole(ctx, pool)
}

func checkWorkspaceRole(ctx context.Context, pool queryRower) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT
 has_table_privilege(current_user,'portal_workspace_settings','SELECT')
 AND has_table_privilege(current_user,'portal_workspace_settings','INSERT')
 AND has_column_privilege(current_user,'portal_workspace_settings','display_name','UPDATE')
 AND has_column_privilege(current_user,'portal_workspace_settings','closed_at','UPDATE')
 AND (SELECT count(*)=2
  FROM pg_proc p JOIN pg_class w ON w.oid='public.portal_workspaces'::regclass
  WHERE p.oid IN ('public.portal_workspace_purge_state(text)'::regprocedure,
                 'public.portal_purge_closed_workspace(text,timestamptz,text,text,text)'::regprocedure)
    AND has_function_privilege(current_user,p.oid,'EXECUTE')
    AND p.proowner=w.relowner AND p.prokind='f' AND p.prosecdef
    AND p.proconfig=ARRAY['search_path=pg_catalog, pg_temp']
    AND NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl
                    WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
    AND NOT EXISTS (SELECT 1 FROM pg_roles r
     WHERE (r.rolname=current_user OR pg_has_role(current_user,r.oid,'MEMBER'))
       AND has_function_privilege(r.oid,p.oid,'EXECUTE WITH GRANT OPTION')))
 AND NOT EXISTS (
  SELECT 1 FROM pg_roles r WHERE (r.rolname=current_user OR pg_has_role(current_user,r.oid,'MEMBER')) AND (
   has_column_privilege(r.oid,'portal_workspace_settings','workspace_id','UPDATE')
   OR has_any_column_privilege(r.oid,'portal_workspace_settings','REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION')
   OR has_table_privilege(r.oid,'portal_workspace_settings','DELETE,TRUNCATE,TRIGGER,MAINTAIN')))`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}

// Service, billing and audit tables the web runtime only reads and appends to,
// and the two billing state tables it also updates. None allows deletion or
// delegation.
var (
	billingFixedTables = []string{"portal_orders", "portal_numina_contacts", "portal_stripe_events", "portal_order_ends", "portal_card_cancellations", "portal_charge_reversals", "portal_order_withdrawals", "portal_withdrawal_cancellations", "portal_order_completions", "portal_withdrawal_settlements", "portal_mail_outbox", "portal_mail_deliveries", "portal_launch_places", "portal_order_addons", "portal_request_closures", "portal_addon_ends", "portal_addon_removals", "portal_operator_audit", "portal_workspaces", "portal_service_fulfillments", "portal_service_consumers", "portal_appointment_reservations", "portal_appointment_revisions", "portal_appointment_introductions", "portal_appointment_introduction_cancellations", "portal_appointment_calendar_intents", "portal_appointment_calendar_observations", "portal_customer_account_closures", "portal_appointment_calendar_attempts", "portal_appointment_calendar_removals"}
	billingStateTables = []string{"portal_order_charges", "portal_card_checkouts"}
)

// CheckPreviewReceiptRole confines append-only receipts to the separately
// configured preview login and database. It checks assumable roles as well.
func CheckPreviewReceiptRole(ctx context.Context, pool queryRower, preview bool) error {
	var allowed bool
	err := pool.QueryRow(ctx, `SELECT
 (CASE WHEN $1::boolean THEN current_user='portal_preview_web' AND current_database()='portal_preview'
    AND has_table_privilege(current_user,'portal_preview_receipts','SELECT')
    AND has_table_privilege(current_user,'portal_preview_receipts','INSERT')
  ELSE NOT EXISTS (SELECT 1 FROM pg_roles r
    WHERE (r.rolname=current_user OR pg_has_role(current_user,r.oid,'MEMBER'))
      AND has_any_column_privilege(r.oid,'portal_preview_receipts','SELECT,INSERT'))
  END)
 AND NOT EXISTS (SELECT 1 FROM pg_roles r
    WHERE (r.rolname=current_user OR pg_has_role(current_user,r.oid,'MEMBER')) AND
      (has_any_column_privilege(r.oid,'portal_preview_receipts','UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION')
       OR has_table_privilege(r.oid,'portal_preview_receipts','DELETE,TRUNCATE,TRIGGER,MAINTAIN')))`, preview).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}

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
 AND has_table_privilege(current_user,'portal_work_queue_revision','SELECT')
 AND has_function_privilege(current_user,'public.portal_lock_work_queue_revision(bigint)','EXECUTE')
 AND EXISTS (
  SELECT 1 FROM pg_proc p JOIN pg_class revision ON revision.oid='public.portal_work_queue_revision'::regclass
  WHERE p.oid='public.portal_lock_work_queue_revision(bigint)'::regprocedure
   AND p.proowner=revision.relowner AND p.prorettype='boolean'::regtype
   AND p.prokind='f' AND p.prosecdef AND p.provolatile='v'
   AND p.proconfig=ARRAY['search_path=pg_catalog, pg_temp']
   AND NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl
                   WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE'))
 AND NOT EXISTS (
  SELECT 1 FROM pg_roles role WHERE (role.rolname=current_user OR pg_has_role(current_user,role.oid,'MEMBER')) AND (
   EXISTS (SELECT 1 FROM unnest(ARRAY['portal_work_approvals','portal_work_revocations']) immutable(table_name)
    WHERE has_any_column_privilege(role.oid,immutable.table_name,'UPDATE,REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION')
     OR has_table_privilege(role.oid,immutable.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))
   OR has_any_column_privilege(role.oid,'portal_work_current','REFERENCES,SELECT WITH GRANT OPTION,INSERT WITH GRANT OPTION,UPDATE WITH GRANT OPTION')
   OR has_column_privilege(role.oid,'portal_work_current','request_id','UPDATE')
   OR has_column_privilege(role.oid,'portal_work_current','client_id','UPDATE')
   OR has_table_privilege(role.oid,'portal_work_current','DELETE,TRUNCATE,TRIGGER,MAINTAIN')
   OR has_any_column_privilege(role.oid,'portal_work_queue_revision','INSERT,UPDATE,REFERENCES,SELECT WITH GRANT OPTION')
   OR has_table_privilege(role.oid,'portal_work_queue_revision','DELETE,TRUNCATE,TRIGGER,MAINTAIN')
   OR EXISTS (SELECT 1 FROM unnest(ARRAY['portal_work_lease_slot','portal_work_leases']) lease(table_name)
    WHERE has_any_column_privilege(role.oid,lease.table_name,'SELECT,INSERT,UPDATE,REFERENCES')
       OR has_table_privilege(role.oid,lease.table_name,'DELETE,TRUNCATE,TRIGGER,MAINTAIN'))
   OR has_function_privilege(role.oid,'public.portal_assign_work_lease_fence()','EXECUTE')
   OR has_function_privilege(role.oid,'public.portal_require_active_work_lease()','EXECUTE')
   OR has_function_privilege(role.oid,'public.portal_validate_work_lease_window()','EXECUTE')
   OR has_function_privilege(role.oid,'public.portal_lock_work_queue_revision(bigint)','EXECUTE WITH GRANT OPTION')
   OR has_function_privilege(role.oid,'public.portal_record_work_queue_revision()','EXECUTE')))`).Scan(&allowed)
	if err != nil || !allowed {
		return ErrDenied
	}
	return nil
}
