CREATE OR REPLACE FUNCTION public.portal_assign_work_lease_fence() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  UPDATE public.portal_work_lease_slot SET issued_fence=issued_fence+1
    WHERE singleton RETURNING issued_fence INTO NEW.fence;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'software lease slot is unavailable' USING ERRCODE='55000';
  END IF;

  -- Keep the observed revision stable through commit. Source writers update
  -- it at commit, after acquiring their ordinary evidence locks.
  IF NOT public.portal_lock_work_queue_revision(NEW.queue_revision) THEN
    RAISE EXCEPTION 'software lease selection is no longer current' USING ERRCODE='55000';
  END IF;

  NEW.acquired_at := clock_timestamp();
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_work_current c
    JOIN public.portal_work_approvals a
      ON (a.id,a.request_id,a.client_id)=(c.approval_id,c.request_id,c.client_id)
    WHERE (c.request_id,c.client_id,c.approval_id,c.generation)=
      (NEW.request_id,NEW.client_id,NEW.approval_id,NEW.approval_generation)
    AND NEW.acquired_at < a.expires_at
    AND NOT EXISTS (SELECT 1 FROM public.portal_work_revocations r WHERE r.approval_id=a.id)
  ) THEN
    RAISE EXCEPTION 'software lease approval is no longer current' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
END;
$$;
