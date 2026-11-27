-- Record both kinds of permanent removal against their exact deletion event.
CREATE OR REPLACE FUNCTION public.portal_guard_customer_account_closure() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_membership_transitions h
    WHERE h.incarnation=NEW.membership_event AND h.role='client' AND h.kind='delete'
      AND h.recorded_at=NEW.closed_at AND h.xmin=pg_current_xact_id()::xid
  ) THEN
    RAISE EXCEPTION 'account closure requires a client deletion in this transaction' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
