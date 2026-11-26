-- Preserve the slot submitted at each revision of a client's reservation.
-- Service authority and the active choice are decided by the booking transaction.
CREATE TABLE public.portal_appointment_reservations (
  id public.portal_consent_key PRIMARY KEY,
  client_id text NOT NULL REFERENCES public.portal_workspaces(id),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(id,client_id)
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
