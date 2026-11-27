CREATE TABLE public.portal_customer_account_closures (
  membership_event uuid PRIMARY KEY
    REFERENCES public.portal_membership_transitions(incarnation) ON DELETE CASCADE,
  closed_at timestamptz NOT NULL,
  remove_by timestamptz NOT NULL,
  CHECK (remove_by = closed_at + interval '720 hours')
);

CREATE FUNCTION public.portal_guard_customer_account_closure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_membership_transitions h
    JOIN public.portal_workspace_settings w ON w.workspace_id=h.client_id
    WHERE h.incarnation=NEW.membership_event AND h.role='client' AND h.kind='delete'
      AND w.closed_at=NEW.closed_at
  ) THEN
    RAISE EXCEPTION 'account closure requires a permanently closed workspace' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_customer_account_closure() FROM PUBLIC;
CREATE TRIGGER portal_customer_account_closure BEFORE INSERT ON public.portal_customer_account_closures
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_customer_account_closure();
