CREATE FUNCTION public.portal_require_active_work_lease() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF NEW.released_at IS NOT NULL THEN
    RAISE EXCEPTION 'new software lease must start active' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
END;
$$;

-- This trigger sorts before fence assignment and refuses invalid input
-- without taking the slot lock.
CREATE TRIGGER portal_admit_active_work_lease BEFORE INSERT ON public.portal_work_leases
  FOR EACH ROW EXECUTE FUNCTION public.portal_require_active_work_lease();
REVOKE ALL ON FUNCTION public.portal_require_active_work_lease() FROM PUBLIC;
