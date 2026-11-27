CREATE TABLE public.portal_appointment_calendar_attempts (
  reservation_id public.portal_consent_key PRIMARY KEY
    REFERENCES public.portal_appointment_calendar_intents(reservation_id) ON DELETE CASCADE,
  attempted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
