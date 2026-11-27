CREATE FUNCTION public.portal_validate_work_lease_selection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
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

-- PostgreSQL orders BEFORE INSERT triggers by name. Lock the slot in
-- portal_assign_work_lease_fence before taking the queue revision lock.
CREATE TRIGGER portal_validate_work_lease_selection BEFORE INSERT ON public.portal_work_leases
  FOR EACH ROW EXECUTE FUNCTION public.portal_validate_work_lease_selection();
REVOKE ALL ON FUNCTION public.portal_validate_work_lease_selection() FROM PUBLIC;
