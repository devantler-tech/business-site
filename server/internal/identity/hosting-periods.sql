-- ADR 0024: one stable entitlement per workspace and maintained project.
-- This migration installs the storage without granting runtime access.
CREATE TABLE public.portal_hosting_entitlements (
  request_id text NOT NULL,
  client_id text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (request_id,client_id),
  FOREIGN KEY (request_id,client_id) REFERENCES public.portal_requests(id,client_id)
);

-- A revision is anchored to the exact typed software acceptance. Each current
-- period has its own pointer, so an early renewal never replaces an older
-- period's head.
CREATE TABLE public.portal_hosting_period_revisions (
  request_id text NOT NULL,
  client_id text NOT NULL,
  period_id public.portal_consent_key NOT NULL,
  revision integer NOT NULL CHECK (revision > 0),
  acceptance_id text NOT NULL,
  proposal_version integer NOT NULL,
  content_hash public.portal_consent_hash NOT NULL,
  service_kind text NOT NULL DEFAULT 'software' CHECK (service_kind='software'),
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  scope text NOT NULL CHECK (btrim(scope)<>'' AND char_length(scope) BETWEEN 1 AND 1200),
  state text NOT NULL DEFAULT 'agreed' CHECK (state IN ('agreed','cancelled','disputed')),
  operator_incarnation uuid NOT NULL REFERENCES public.portal_membership_transitions(incarnation),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (request_id,client_id,period_id,revision),
  FOREIGN KEY (request_id,client_id)
    REFERENCES public.portal_hosting_entitlements(request_id,client_id),
  FOREIGN KEY (acceptance_id,request_id,client_id,proposal_version,content_hash)
    REFERENCES public.portal_proposal_acceptances(id,request_id,client_id,proposal_version,content_hash),
  FOREIGN KEY (request_id,client_id,proposal_version,content_hash,service_kind)
    REFERENCES public.portal_proposals(request_id,client_id,version,content_sha256,service_kind),
  CHECK (isfinite(starts_at) AND isfinite(ends_at) AND starts_at < ends_at)
);

CREATE TABLE public.portal_hosting_period_heads (
  request_id text NOT NULL,
  client_id text NOT NULL,
  period_id public.portal_consent_key NOT NULL,
  revision integer NOT NULL,
  PRIMARY KEY (request_id,client_id,period_id),
  FOREIGN KEY (request_id,client_id,period_id,revision)
    REFERENCES public.portal_hosting_period_revisions(request_id,client_id,period_id,revision)
);

CREATE FUNCTION public.portal_guard_hosting_period_operator() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  PERFORM 1 FROM public.portal_memberships m
  JOIN public.portal_membership_transitions h ON h.incarnation=m.incarnation
    AND h.issuer=m.issuer AND h.subject=m.subject AND h.client_id=m.client_id
    AND h.role=m.role AND h.revoked=m.revoked
  WHERE m.incarnation=NEW.operator_incarnation AND m.role='operator' AND NOT m.revoked
    AND h.kind IN ('baseline','insert','update')
  FOR SHARE OF m;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'hosting period operator membership refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_hosting_period_operator() FROM PUBLIC;
CREATE TRIGGER portal_hosting_period_operator BEFORE INSERT ON public.portal_hosting_period_revisions
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_hosting_period_operator();

REVOKE ALL ON public.portal_hosting_entitlements,public.portal_hosting_period_revisions,public.portal_hosting_period_heads FROM PUBLIC;
