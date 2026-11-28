-- Only the runtime's scoped operator action can append a new period. No
-- runtime role receives direct read/write access to the hosting ledgers.
CREATE FUNCTION public.portal_record_hosting_period(
  project text, actor_issuer text, actor_subject text, actor_incarnation uuid,
  period text, acceptance text, bound_start timestamptz, bound_end timestamptz
) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE
  workspace text;
  evidence record;
BEGIN
  IF current_setting('transaction_isolation')<>'read committed' OR
    project IS NULL OR actor_incarnation IS NULL OR period IS NULL OR acceptance IS NULL OR
    bound_start IS NULL OR bound_end IS NULL OR
    NOT isfinite(bound_start) OR NOT isfinite(bound_end) OR bound_start>=bound_end THEN
    RETURN 'conflict';
  END IF;

  SELECT q.client_id INTO workspace FROM public.portal_requests q WHERE q.id=project;
  IF NOT FOUND THEN RETURN 'missing'; END IF;

  -- The same workspace lock precedes closure and retention. Recheck with a
  -- fresh snapshot after any wait and before taking membership/ledger locks.
  PERFORM pg_advisory_xact_lock(1869820525,hashtext(workspace));
  IF NOT EXISTS (SELECT 1 FROM public.portal_requests q WHERE q.id=project AND q.client_id=workspace)
    OR EXISTS (SELECT 1 FROM public.portal_workspace_settings w WHERE w.workspace_id=workspace AND w.closed_at IS NOT NULL) THEN
    RETURN 'missing';
  END IF;
  PERFORM 1 FROM public.portal_memberships m
    WHERE m.issuer=actor_issuer AND m.subject=actor_subject AND m.incarnation=actor_incarnation
      AND m.role='operator' AND NOT m.revoked FOR SHARE OF m;
  IF NOT FOUND THEN RETURN 'denied'; END IF;

  SELECT a.id,a.proposal_version,a.content_hash,p.monthly_terms INTO evidence
    FROM public.portal_proposal_acceptances a
    JOIN public.portal_proposals p ON p.request_id=a.request_id AND p.client_id=a.client_id
      AND p.version=a.proposal_version AND p.content_sha256=a.content_hash
    WHERE a.id=acceptance AND a.request_id=project AND a.client_id=workspace
      AND p.content_format=2 AND p.service_kind='software' AND btrim(p.monthly_terms)<>''
      AND a.client_incarnation IS NOT NULL;
  IF NOT FOUND THEN RETURN 'conflict'; END IF;

  INSERT INTO public.portal_hosting_entitlements(request_id,client_id) VALUES(project,workspace)
    ON CONFLICT DO NOTHING;
  PERFORM 1 FROM public.portal_hosting_entitlements e WHERE e.request_id=project AND e.client_id=workspace FOR UPDATE;
  IF EXISTS (SELECT 1 FROM public.portal_hosting_period_heads h
    WHERE h.request_id=project AND h.client_id=workspace AND h.period_id=period) THEN
    IF EXISTS (
      SELECT 1 FROM public.portal_hosting_period_heads h
      JOIN public.portal_hosting_period_revisions r USING(request_id,client_id,period_id,revision)
      WHERE h.request_id=project AND h.client_id=workspace AND h.period_id=period AND h.revision=1
        AND r.acceptance_id=evidence.id AND r.starts_at=bound_start AND r.ends_at=bound_end
        AND r.operator_incarnation=actor_incarnation AND r.state='agreed'
    ) THEN RETURN 'recorded'; END IF;
    RETURN 'conflict';
  END IF;
  IF EXISTS (
    SELECT 1 FROM public.portal_hosting_period_heads h
    JOIN public.portal_hosting_period_revisions r USING(request_id,client_id,period_id,revision)
    WHERE h.request_id=project AND h.client_id=workspace AND r.starts_at<bound_end AND bound_start<r.ends_at
  ) THEN RETURN 'conflict'; END IF;

  INSERT INTO public.portal_hosting_period_revisions(
    request_id,client_id,period_id,revision,acceptance_id,proposal_version,content_hash,starts_at,ends_at,scope,operator_incarnation
  ) VALUES(project,workspace,period,1,evidence.id,evidence.proposal_version,evidence.content_hash,bound_start,bound_end,evidence.monthly_terms,actor_incarnation);
  INSERT INTO public.portal_hosting_period_heads(request_id,client_id,period_id,revision)
    VALUES(project,workspace,period,1);
  RETURN 'recorded';
END;
$$;

REVOKE ALL ON FUNCTION public.portal_record_hosting_period(text,text,text,uuid,text,text,timestamptz,timestamptz) FROM PUBLIC;
