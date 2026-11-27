CREATE FUNCTION public.portal_validate_work_lease_window() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_work_approvals a
    WHERE (a.id,a.request_id,a.client_id)=(NEW.approval_id,NEW.request_id,NEW.client_id)
      AND NEW.expires_at <= a.expires_at
  ) THEN
    RAISE EXCEPTION 'software lease ends after its approval' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
END;
$$;

-- The selection trigger runs first and holds the queue revision lock.
CREATE TRIGGER portal_validate_work_lease_window BEFORE INSERT ON public.portal_work_leases
  FOR EACH ROW EXECUTE FUNCTION public.portal_validate_work_lease_window();
REVOKE ALL ON FUNCTION public.portal_validate_work_lease_window() FROM PUBLIC;
