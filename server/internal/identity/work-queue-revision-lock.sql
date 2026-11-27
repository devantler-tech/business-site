-- Hold only the singleton revision the caller already observed. The web role
-- can keep this read lock until commit without acquiring table UPDATE rights.
CREATE FUNCTION public.portal_lock_work_queue_revision(expected bigint) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  PERFORM 1 FROM public.portal_work_queue_revision
    WHERE singleton AND revision=expected
    FOR SHARE;
  RETURN FOUND;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_lock_work_queue_revision(bigint) FROM PUBLIC;
