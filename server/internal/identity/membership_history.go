package identity

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// readMembershipIncarnation leaves transaction ownership and prior request
// authorization with its caller; it does not itself authorize any work.
func readMembershipIncarnation(ctx context.Context, tx pgx.Tx, issuer, subject, client, role string) (string, error) {
	var incarnation string
	err := tx.QueryRow(ctx, `SELECT m.incarnation::text
 FROM public.portal_memberships m JOIN public.portal_membership_transitions h ON h.incarnation=m.incarnation
 WHERE m.issuer=$1 AND m.subject=$2 AND m.client_id=$3 AND m.role=$4 AND NOT m.revoked
 AND h.kind IN ('baseline','insert','update') AND h.issuer=m.issuer AND h.subject=m.subject
 AND h.client_id=m.client_id AND h.role=m.role AND h.revoked=m.revoked
 FOR SHARE OF m`, issuer, subject, client, role).Scan(&incarnation)
	if err != nil || ctx.Err() != nil || incarnation == "" {
		return "", ErrDenied
	}
	return incarnation, nil
}

// CheckMembershipRecorder checks the installed trigger boundary, independently
// of the migration ledger. It reads metadata only and grants no membership.
func CheckMembershipRecorder(ctx context.Context, pool queryRower) error {
	var intact bool
	err := pool.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid='public.portal_memberships'::regclass
   AND a.attname='incarnation' AND a.atttypid='uuid'::regtype AND a.attnotnull AND NOT a.attisdropped)
 AND (SELECT bool_and(p.oid IS NOT NULL AND t.oid IS NOT NULL
   AND p.proowner=m.relowner AND p.prorettype='trigger'::regtype AND p.pronargs=0
   AND p.prokind='f' AND p.prosecdef=managed.definer
   AND p.proconfig=ARRAY['search_path=pg_catalog, pg_temp']
   AND NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl
                   WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
   AND NOT t.tgisinternal AND t.tgtype=managed.kind AND t.tgnargs=0
   AND t.tgqual IS NULL AND t.tgattr=''::int2vector AND t.tgenabled IN ('O','A'))
 FROM (VALUES ('portal_membership_incarnation','portal_assign_membership_incarnation',23,false),
              ('portal_membership_transition','portal_record_membership_transition',29,true)) managed(trigger_name,function_name,kind,definer)
 JOIN pg_class m ON m.oid='public.portal_memberships'::regclass
 LEFT JOIN pg_proc p ON p.pronamespace='public'::regnamespace AND p.proname=managed.function_name AND p.pronargs=0
 LEFT JOIN pg_trigger t ON t.tgrelid=m.oid AND t.tgname=managed.trigger_name AND t.tgfoid=p.oid)
 AND EXISTS (SELECT 1 FROM pg_class h JOIN pg_class m ON m.oid='public.portal_memberships'::regclass
   WHERE h.oid='public.portal_membership_transitions'::regclass AND h.relkind='r' AND h.relowner=m.relowner)`).Scan(&intact)
	if err != nil || !intact {
		return ErrDenied
	}
	return nil
}
