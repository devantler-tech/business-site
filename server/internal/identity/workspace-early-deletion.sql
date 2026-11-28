-- Return a coarse eligibility state. Inspect protected records without granting
-- the runtime direct access to the hosting ledger.
CREATE FUNCTION public.portal_workspace_purge_state(workspace text) RETURNS text
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
  SELECT CASE
    WHEN workspace IN ('devantler-tech','preview-demo') OR NOT EXISTS (
      SELECT 1 FROM public.portal_workspace_settings w WHERE w.workspace_id=workspace AND w.closed_at IS NOT NULL
    ) THEN 'unavailable'
    WHEN EXISTS (SELECT 1 FROM public.portal_memberships WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_invitations
        WHERE client_id=workspace AND (role<>'client' OR (NOT revoked AND expires_at>statement_timestamp())))
      OR EXISTS (SELECT 1 FROM public.portal_membership_transitions WHERE client_id=workspace AND role<>'client')
    THEN 'access'
    WHEN EXISTS (SELECT 1 FROM public.portal_orders WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_invoices WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_credit_transfers WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_credit_revisions WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_numina_contacts WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_launch_places WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_proposal_acceptances WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_work_approvals WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_work_revocations WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_work_current WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_appointment_reservations WHERE client_id=workspace)
      OR EXISTS (SELECT 1 FROM public.portal_hosting_entitlements WHERE client_id=workspace)
    THEN 'records'
    ELSE 'eligible'
  END
$$;

-- Early deletion has a narrower record scope than the scheduled retention job.
-- Keep the separate account-closure timeline and any linked membership history.
CREATE FUNCTION public.portal_purge_closed_workspace(
  workspace text, expected_closure timestamptz, issuer text, subject text, reason text
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF workspace IS NULL OR workspace IN ('devantler-tech','preview-demo')
    OR expected_closure IS NULL OR reason IS NULL OR reason NOT IN ('test','request') THEN
    RETURN false;
  END IF;
  PERFORM pg_advisory_xact_lock(1869820525,hashtext(workspace));
  -- Match the order used by scheduled retention and invitation redemption.
  LOCK TABLE public.portal_invitations, public.portal_memberships IN SHARE ROW EXCLUSIVE MODE;
  PERFORM 1 FROM public.portal_workspaces w JOIN public.portal_workspace_settings s ON s.workspace_id=w.id
    WHERE w.id=workspace AND s.closed_at=expected_closure FOR UPDATE OF w,s;
  IF NOT FOUND OR public.portal_workspace_purge_state(workspace)<>'eligible' OR NOT EXISTS (
    SELECT 1 FROM public.portal_memberships m
    WHERE m.issuer=portal_purge_closed_workspace.issuer AND m.subject=portal_purge_closed_workspace.subject
      AND m.client_id='devantler-tech' AND m.role='operator' AND NOT m.revoked
  ) THEN
    RETURN false;
  END IF;

  -- Lock remaining requests against new referencing agreements, then check
  -- eligibility again after any wait on those rows.
  PERFORM 1 FROM public.portal_requests WHERE client_id=workspace FOR UPDATE;
  IF public.portal_workspace_purge_state(workspace)<>'eligible' THEN
    RETURN false;
  END IF;
  DELETE FROM public.portal_proposal_reviews WHERE client_id=workspace;
  DELETE FROM public.portal_proposal_visibility WHERE client_id=workspace;
  DELETE FROM public.portal_proposals WHERE client_id=workspace;
  DELETE FROM public.portal_request_closures WHERE client_id=workspace;
  DELETE FROM public.portal_requests WHERE client_id=workspace;
  DELETE FROM public.portal_contacts WHERE client_id=workspace;
  DELETE FROM public.portal_invitations WHERE client_id=workspace;
  DELETE FROM public.portal_workspaces WHERE id=workspace;

  -- Write the result atomically. The existing operator endpoint also records
  -- the authenticated attempt and final outcome.
  INSERT INTO public.portal_operator_audit(actor_issuer,actor_subject,actor_kind,action,target,outcome)
    VALUES(issuer,subject,'operator','workspaces.purge-'||reason,workspace,'allowed');
  RETURN true;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_workspace_purge_state(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.portal_purge_closed_workspace(text,timestamptz,text,text,text) FROM PUBLIC;
