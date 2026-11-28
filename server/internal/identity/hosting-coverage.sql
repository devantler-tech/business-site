-- Present only current period heads, scoped to the maintained project and
-- exact operator membership. The web role cannot read the ledgers directly.
CREATE FUNCTION public.portal_operator_hosting_coverage(
  project text, workspace text, issuer text, subject text, operator_incarnation uuid
) RETURNS TABLE(state text, scope text, proposal_version integer, starts_at timestamptz, ends_at timestamptz)
LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
WITH checked_at AS MATERIALIZED (SELECT clock_timestamp() AS value),
periods AS MATERIALIZED (
  SELECT r.scope,r.proposal_version,r.starts_at,r.ends_at,r.state,
    lag(r.ends_at) OVER (ORDER BY r.starts_at,r.period_id) AS previous_end
  FROM public.portal_hosting_period_heads h
  JOIN public.portal_hosting_period_revisions r USING(request_id,client_id,period_id,revision)
  WHERE h.request_id=portal_operator_hosting_coverage.project AND h.client_id=portal_operator_hosting_coverage.workspace
    AND EXISTS (SELECT 1 FROM public.portal_memberships m
      WHERE m.issuer=portal_operator_hosting_coverage.issuer AND m.subject=portal_operator_hosting_coverage.subject
        AND m.incarnation=portal_operator_hosting_coverage.operator_incarnation AND m.role='operator' AND NOT m.revoked)
    AND NOT EXISTS (SELECT 1 FROM public.portal_workspace_settings w
      WHERE w.workspace_id=portal_operator_hosting_coverage.workspace AND w.closed_at IS NOT NULL)
), selected AS (
  SELECT p.*, p.starts_at<=t.value AS governing
  FROM periods p CROSS JOIN checked_at t
  WHERE p.ends_at>t.value
  ORDER BY p.starts_at
  LIMIT 1
), coverage_state AS (
  SELECT CASE
    WHEN NOT EXISTS (SELECT 1 FROM periods) OR EXISTS (SELECT 1 FROM periods p WHERE p.starts_at<p.previous_end)
      THEN 'unavailable'
    WHEN NOT EXISTS (SELECT 1 FROM selected) THEN 'ended'
    WHEN NOT s.governing THEN 'future'
    WHEN s.state='agreed' THEN 'active'
    ELSE s.state
  END AS value
  FROM (SELECT 1) AS one LEFT JOIN selected s ON true
)
SELECT c.value,s.scope,s.proposal_version,s.starts_at,s.ends_at
FROM coverage_state c
LEFT JOIN selected s ON c.value IN ('active','future','cancelled','disputed')
$$;

REVOKE ALL ON FUNCTION public.portal_operator_hosting_coverage(text,text,text,text,uuid) FROM PUBLIC;
