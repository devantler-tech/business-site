CREATE TABLE public.portal_appointment_calendar_removals (
  reservation_id public.portal_consent_key PRIMARY KEY
    REFERENCES public.portal_appointment_introduction_cancellations(reservation_id) ON DELETE CASCADE
    REFERENCES public.portal_appointment_calendar_observations(reservation_id) ON DELETE CASCADE,
  outcome text NOT NULL CHECK (outcome IN ('removed','conflict')),
  observed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
