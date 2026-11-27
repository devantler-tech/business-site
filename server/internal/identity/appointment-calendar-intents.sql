-- One reserved identity for the first calendar create. The immutable revision
-- supplies the interval, and retention cascades from that same revision.
CREATE TABLE public.portal_appointment_calendar_intents (
  reservation_id public.portal_consent_key PRIMARY KEY,
  revision integer NOT NULL,
  calendar_binding public.portal_consent_hash NOT NULL,
  event_key public.portal_consent_hash NOT NULL UNIQUE,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK(event_key<>calendar_binding),
  FOREIGN KEY(reservation_id,revision)
    REFERENCES public.portal_appointment_revisions(reservation_id,revision) ON DELETE CASCADE
);

CREATE FUNCTION public.portal_guard_appointment_calendar_intent() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
  NEW.recorded_at := clock_timestamp();
  IF NOT EXISTS(
    SELECT 1 FROM public.portal_appointment_revisions v
    WHERE (v.reservation_id,v.revision)=(NEW.reservation_id,NEW.revision)
      AND date_trunc('second',v.starts_at)=v.starts_at
      AND date_trunc('second',v.ends_at)=v.ends_at
      AND (
        EXISTS(SELECT 1 FROM public.portal_appointment_introductions i
          WHERE (i.reservation_id,i.revision,i.client_id)=(v.reservation_id,v.revision,v.client_id)
            AND NOT EXISTS(SELECT 1 FROM public.portal_appointment_introduction_cancellations c
              WHERE c.reservation_id=i.reservation_id))
        OR EXISTS(SELECT 1 FROM public.portal_orders o
          JOIN public.portal_service_fulfillments f ON f.order_id=o.id
          JOIN public.portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind='appointment'
          WHERE (o.appointment_reservation_id,o.appointment_revision,o.client_id)=(v.reservation_id,v.revision,v.client_id)
            AND c.aggregate_id=v.reservation_id::text)
      )
  ) THEN
    RAISE EXCEPTION 'calendar intent requires a confirmed appointment revision' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_appointment_calendar_intent() FROM PUBLIC;
CREATE TRIGGER portal_appointment_calendar_intent BEFORE INSERT ON public.portal_appointment_calendar_intents
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_calendar_intent();
