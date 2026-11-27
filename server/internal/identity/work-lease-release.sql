CREATE FUNCTION public.portal_guard_work_lease_release() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF (NEW.fence,NEW.request_id,NEW.client_id,NEW.approval_id,NEW.approval_generation,
      NEW.queue_revision,NEW.actor_key,NEW.submission_key,NEW.acquired_at,NEW.expires_at)
    IS DISTINCT FROM
     (OLD.fence,OLD.request_id,OLD.client_id,OLD.approval_id,OLD.approval_generation,
      OLD.queue_revision,OLD.actor_key,OLD.submission_key,OLD.acquired_at,OLD.expires_at) THEN
    RAISE EXCEPTION 'software lease claim is immutable' USING ERRCODE='55000';
  END IF;

  IF NEW.released_at IS NOT DISTINCT FROM OLD.released_at THEN
    RETURN NEW;
  END IF;
  IF OLD.released_at IS NOT NULL THEN
    RAISE EXCEPTION 'software lease release is immutable' USING ERRCODE='55000';
  END IF;

  -- Only PostgreSQL can stamp the one transition out of an active window.
  NEW.released_at := clock_timestamp();
  IF NEW.released_at < OLD.acquired_at OR NEW.released_at >= OLD.expires_at THEN
    RAISE EXCEPTION 'software lease is no longer active' USING ERRCODE='55000';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER portal_guard_work_lease_release BEFORE UPDATE ON public.portal_work_leases
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_work_lease_release();
REVOKE ALL ON FUNCTION public.portal_guard_work_lease_release() FROM PUBLIC;
