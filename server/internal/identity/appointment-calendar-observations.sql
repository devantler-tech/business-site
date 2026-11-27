CREATE TABLE public.portal_appointment_calendar_observations (
  reservation_id public.portal_consent_key PRIMARY KEY
    REFERENCES public.portal_appointment_calendar_intents(reservation_id) ON DELETE CASCADE,
  outcome text NOT NULL,
  provider_tag text,
  observed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK (
    outcome='created' AND provider_tag IS NOT NULL AND provider_tag COLLATE "C" ~ '^"[!#-~]{1,128}"$'
    OR outcome='conflict' AND provider_tag IS NULL
  )
);
