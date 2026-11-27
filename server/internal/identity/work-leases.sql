-- The slot survives disposal of the customer records that once used it.
CREATE TABLE public.portal_work_lease_slot (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  issued_fence bigint NOT NULL CHECK (issued_fence >= 0)
);
INSERT INTO public.portal_work_lease_slot(issued_fence) VALUES (0);

CREATE TABLE public.portal_work_leases (
  fence bigint PRIMARY KEY CHECK (fence > 0),
  request_id text NOT NULL,
  client_id text NOT NULL,
  approval_id public.portal_consent_key NOT NULL,
  approval_generation bigint NOT NULL CHECK (approval_generation > 0),
  queue_revision bigint NOT NULL CHECK (queue_revision >= 0),
  actor_key public.portal_consent_hash NOT NULL,
  submission_key public.portal_consent_key NOT NULL,
  acquired_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  released_at timestamptz,
  FOREIGN KEY (approval_id,request_id,client_id)
    REFERENCES public.portal_work_approvals(id,request_id,client_id) ON DELETE CASCADE,
  UNIQUE (actor_key,submission_key),
  CHECK (isfinite(acquired_at) AND isfinite(expires_at) AND expires_at > acquired_at),
  CHECK (released_at IS NULL OR released_at BETWEEN acquired_at AND expires_at),
  CONSTRAINT portal_work_lease_windows_exclusion EXCLUDE USING gist
    (tstzrange(acquired_at,COALESCE(released_at,expires_at),'[)') WITH &&)
);
CREATE INDEX portal_work_leases_approval ON public.portal_work_leases(approval_id);

CREATE FUNCTION public.portal_assign_work_lease_fence() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  UPDATE public.portal_work_lease_slot SET issued_fence=issued_fence+1
    WHERE singleton RETURNING issued_fence INTO NEW.fence;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'software lease slot is unavailable' USING ERRCODE='55000';
  END IF;
  NEW.acquired_at := clock_timestamp();
  RETURN NEW;
END;
$$;

CREATE TRIGGER portal_assign_work_lease_fence BEFORE INSERT ON public.portal_work_leases
  FOR EACH ROW EXECUTE FUNCTION public.portal_assign_work_lease_fence();

REVOKE ALL ON public.portal_work_lease_slot,public.portal_work_leases FROM PUBLIC;
REVOKE ALL ON FUNCTION public.portal_assign_work_lease_fence() FROM PUBLIC;
