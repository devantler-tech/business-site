-- Preserve the slot submitted at each revision of a client's reservation.
-- Service authority and the active choice are decided by the booking transaction.
CREATE TABLE public.portal_appointment_reservations (
  id public.portal_consent_key PRIMARY KEY,
  client_id text NOT NULL REFERENCES public.portal_workspaces(id),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  quote_request_id text,
  quote_version integer,
  quote_sha256 public.portal_consent_hash,
  quote_kind text GENERATED ALWAYS AS (CASE WHEN quote_request_id IS NOT NULL THEN 'appointment'::text END) STORED,
  UNIQUE(id,client_id),
  CHECK(num_nonnulls(quote_request_id,quote_version,quote_sha256) IN (0,3)),
  FOREIGN KEY(quote_request_id,client_id,quote_version,quote_sha256,quote_kind)
    REFERENCES public.portal_proposals(request_id,client_id,version,content_sha256,service_kind)
);

CREATE INDEX portal_appointment_client ON public.portal_appointment_reservations(client_id);

CREATE TABLE public.portal_appointment_revisions (
  reservation_id public.portal_consent_key NOT NULL,
  client_id text NOT NULL,
  revision integer NOT NULL CHECK(revision>0),
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  held_until timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(reservation_id,revision),
  FOREIGN KEY(reservation_id,client_id) REFERENCES public.portal_appointment_reservations(id,client_id) ON DELETE CASCADE,
  CHECK(ends_at>starts_at AND ends_at-starts_at<=interval '24 hours'),
  CHECK(extract(epoch FROM ends_at-starts_at)%60=0),
  CHECK(held_until>recorded_at AND held_until<=starts_at)
);

-- The stable client owns at most one introduction. It commits a particular
-- revision, so the interval remains occupied after the provisional hold expires.
CREATE TABLE public.portal_appointment_introductions (
  client_id text PRIMARY KEY,
  reservation_id public.portal_consent_key NOT NULL UNIQUE,
  revision integer NOT NULL,
  confirmed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(reservation_id,client_id) REFERENCES public.portal_appointment_reservations(id,client_id),
  FOREIGN KEY(reservation_id,revision) REFERENCES public.portal_appointment_revisions(reservation_id,revision)
);

CREATE FUNCTION public.portal_guard_appointment_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM public.portal_appointment_introductions i WHERE i.reservation_id=NEW.reservation_id
  ) THEN
    RAISE EXCEPTION 'confirmed appointment revision refused' USING ERRCODE='23514';
  END IF;
  IF EXISTS (
    SELECT 1 FROM public.portal_appointment_reservations r
    JOIN public.portal_proposals p ON p.request_id=r.quote_request_id AND p.client_id=r.client_id
      AND p.version=r.quote_version AND p.content_sha256=r.quote_sha256 AND p.service_kind=r.quote_kind
    WHERE r.id=NEW.reservation_id AND r.client_id=NEW.client_id
      AND extract(epoch FROM NEW.ends_at-NEW.starts_at) <> p.duration_minutes * 60
  ) THEN
    RAISE EXCEPTION 'appointment quote duration refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_appointment_revision() FROM PUBLIC;
CREATE TRIGGER portal_appointment_revision BEFORE INSERT ON public.portal_appointment_revisions
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_revision();

CREATE FUNCTION public.portal_guard_appointment_introduction() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  NEW.confirmed_at := clock_timestamp();
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_appointment_revisions v
    JOIN public.portal_appointment_reservations r ON r.id=v.reservation_id AND r.client_id=v.client_id
    WHERE v.reservation_id=NEW.reservation_id AND v.client_id=NEW.client_id AND v.revision=NEW.revision
      AND r.quote_request_id IS NULL AND v.ends_at-v.starts_at=interval '30 minutes'
      AND v.held_until>NEW.confirmed_at AND NOT EXISTS (
        SELECT 1 FROM public.portal_appointment_revisions later
        WHERE later.reservation_id=v.reservation_id AND later.revision>v.revision)
  ) THEN
    RAISE EXCEPTION 'appointment introduction refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_appointment_introduction() FROM PUBLIC;
CREATE TRIGGER portal_appointment_introduction BEFORE INSERT ON public.portal_appointment_introductions
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_introduction();
