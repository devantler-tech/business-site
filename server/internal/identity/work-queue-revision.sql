-- A single revision orders commits that can change software-work eligibility.
CREATE TABLE public.portal_work_queue_revision (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  revision bigint NOT NULL CHECK (revision >= 0),
  writer xid8,
  CHECK ((revision = 0) = (writer IS NULL))
);

INSERT INTO public.portal_work_queue_revision(revision) VALUES (0);

-- Deferred row triggers acquire the revision lock after ordinary evidence writes.
-- A transaction touching several sources advances the revision only once.
CREATE FUNCTION public.portal_record_work_queue_revision() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE
  transaction_id xid8 := pg_current_xact_id();
BEGIN
  UPDATE public.portal_work_queue_revision
    SET revision=revision+1,writer=transaction_id
    WHERE singleton AND writer IS DISTINCT FROM transaction_id;
  IF NOT FOUND AND NOT EXISTS (SELECT 1 FROM public.portal_work_queue_revision WHERE singleton) THEN
    RAISE EXCEPTION 'software queue revision is unavailable' USING ERRCODE='55000';
  END IF;
  RETURN NULL;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_record_work_queue_revision() FROM PUBLIC;

DO $$
DECLARE
  source_table text;
BEGIN
  FOREACH source_table IN ARRAY ARRAY[
    'portal_requests', 'portal_memberships', 'portal_membership_transitions',
    'portal_proposals', 'portal_proposal_visibility', 'portal_proposal_acceptances',
    'portal_work_current', 'portal_work_approvals', 'portal_work_revocations',
    'portal_orders', 'portal_order_charges', 'portal_charge_reversals', 'portal_order_withdrawals',
    'portal_order_addons', 'portal_addon_ends', 'portal_addon_removals',
    'portal_mail_outbox', 'portal_mail_deliveries', 'portal_request_closures', 'portal_order_completions'
  ] LOOP
    EXECUTE format('CREATE CONSTRAINT TRIGGER portal_work_queue_revision_changed
      AFTER INSERT OR UPDATE OR DELETE ON public.%I DEFERRABLE INITIALLY DEFERRED
      FOR EACH ROW EXECUTE FUNCTION public.portal_record_work_queue_revision()', source_table);
  END LOOP;
END;
$$;
