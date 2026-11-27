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

-- A stable client can book another introduction after cancelling the previous
-- one. Each confirmation keeps its original revision and cancellation terms.
CREATE TABLE public.portal_appointment_introductions (
  client_id text NOT NULL,
  sequence integer NOT NULL CHECK(sequence>0),
  reservation_id public.portal_consent_key NOT NULL UNIQUE,
  revision integer NOT NULL,
  cancellation_notice_minutes integer NOT NULL CHECK(cancellation_notice_minutes BETWEEN 0 AND 365*24*60),
  cancellation_terms_version text NOT NULL CHECK(char_length(btrim(cancellation_terms_version)) BETWEEN 1 AND 128),
  cancellable_until timestamptz NOT NULL,
  confirmed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(client_id,sequence),
  FOREIGN KEY(reservation_id,client_id) REFERENCES public.portal_appointment_reservations(id,client_id),
  FOREIGN KEY(reservation_id,revision) REFERENCES public.portal_appointment_revisions(reservation_id,revision)
);

CREATE TABLE public.portal_appointment_introduction_cancellations (
  reservation_id public.portal_consent_key PRIMARY KEY REFERENCES public.portal_appointment_introductions(reservation_id),
  cancelled_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION public.portal_guard_appointment_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM public.portal_appointment_introductions i WHERE i.reservation_id=NEW.reservation_id
  ) OR EXISTS (
    SELECT 1 FROM public.portal_orders o JOIN public.portal_service_fulfillments f ON f.order_id=o.id
    JOIN public.portal_service_consumers c ON c.fulfillment_id=f.id AND c.service_kind='appointment'
    WHERE o.appointment_reservation_id=NEW.reservation_id AND c.aggregate_id=NEW.reservation_id::text
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

CREATE FUNCTION public.portal_guard_paid_appointment_consumer() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF NEW.service_kind='appointment' AND NOT EXISTS(
    SELECT 1 FROM public.portal_service_fulfillments f
    JOIN public.portal_orders o ON o.id=f.order_id
    JOIN public.portal_appointment_revisions v ON v.reservation_id=o.appointment_reservation_id
      AND v.revision=o.appointment_revision AND v.client_id=o.client_id
    JOIN public.portal_mail_outbox m ON m.order_id=o.id AND m.kind='order-confirmation' AND m.producer_format=3
    JOIN public.portal_mail_deliveries d ON d.outbox_id=m.id
    WHERE f.id=NEW.fulfillment_id AND NEW.aggregate_id=v.reservation_id::text
      AND v.held_until>clock_timestamp()
      AND NOT EXISTS(SELECT 1 FROM public.portal_appointment_revisions later
        WHERE later.reservation_id=v.reservation_id AND later.revision>v.revision)) THEN
    RAISE EXCEPTION 'paid appointment consumer refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_paid_appointment_consumer() FROM PUBLIC;
CREATE TRIGGER portal_paid_appointment_consumer BEFORE INSERT ON public.portal_service_consumers
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_paid_appointment_consumer();
REVOKE ALL ON FUNCTION public.portal_guard_appointment_revision() FROM PUBLIC;
CREATE TRIGGER portal_appointment_revision BEFORE INSERT ON public.portal_appointment_revisions
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_revision();

CREATE FUNCTION public.portal_guard_appointment_introduction() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE slot_start timestamptz;
BEGIN
  NEW.confirmed_at := clock_timestamp();
  SELECT v.starts_at INTO slot_start FROM public.portal_appointment_revisions v
  JOIN public.portal_appointment_reservations r ON r.id=v.reservation_id AND r.client_id=v.client_id
    WHERE v.reservation_id=NEW.reservation_id AND v.client_id=NEW.client_id AND v.revision=NEW.revision
      AND r.quote_request_id IS NULL AND v.ends_at-v.starts_at=interval '30 minutes'
      AND v.held_until>NEW.confirmed_at AND NOT EXISTS (
        SELECT 1 FROM public.portal_appointment_revisions later
        WHERE later.reservation_id=v.reservation_id AND later.revision>v.revision);
  IF NOT FOUND OR (NEW.sequence>1 AND NOT EXISTS (
    SELECT 1 FROM public.portal_appointment_introductions i
    JOIN public.portal_appointment_introduction_cancellations c ON c.reservation_id=i.reservation_id
    WHERE i.client_id=NEW.client_id AND i.sequence=NEW.sequence-1
  )) THEN
    RAISE EXCEPTION 'appointment introduction refused' USING ERRCODE='23514';
  END IF;
  NEW.cancellable_until := slot_start - make_interval(mins=>NEW.cancellation_notice_minutes);
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_appointment_introduction() FROM PUBLIC;
CREATE TRIGGER portal_appointment_introduction BEFORE INSERT ON public.portal_appointment_introductions
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_introduction();

CREATE FUNCTION public.portal_guard_appointment_introduction_cancellation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  NEW.cancelled_at := clock_timestamp();
  IF NOT EXISTS (
    SELECT 1 FROM public.portal_appointment_introductions i
    WHERE i.reservation_id=NEW.reservation_id AND NEW.cancelled_at<=i.cancellable_until
  ) THEN
    RAISE EXCEPTION 'appointment introduction cancellation refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_appointment_introduction_cancellation() FROM PUBLIC;
CREATE TRIGGER portal_appointment_introduction_cancellation BEFORE INSERT ON public.portal_appointment_introduction_cancellations
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_appointment_introduction_cancellation();

-- Appointment offers use the same current human issuance evidence as software.
-- An appointment is a fixed, positive one-time quote.
CREATE OR REPLACE FUNCTION public.portal_guard_service_visibility() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE proposal public.portal_proposals%ROWTYPE;
BEGIN
  IF TG_RELID<>'public.portal_proposal_visibility'::regclass OR TG_OP<>'INSERT' THEN
    RAISE EXCEPTION 'invalid service visibility invocation';
  END IF;
  SELECT * INTO proposal FROM public.portal_proposals
    WHERE request_id=NEW.request_id AND client_id=NEW.client_id AND version=NEW.proposal_version;
  IF NOT FOUND THEN RAISE EXCEPTION 'service proposal binding refused' USING ERRCODE='23514'; END IF;
  IF proposal.content_format=2 AND NEW.action='share' THEN
    IF proposal.service_kind='appointment' AND (proposal.one_time_minor<=0 OR proposal.monthly_minor<>0) THEN
      RAISE EXCEPTION 'appointment price refused' USING ERRCODE='23514';
    END IF;
    IF NEW.operator_client IS NULL OR NEW.operator_incarnation IS NULL
      OR NEW.content_hash IS DISTINCT FROM proposal.content_sha256 THEN
      RAISE EXCEPTION 'service issuer provenance refused' USING ERRCODE='23514';
    END IF;
    PERFORM 1 FROM public.portal_memberships m
      WHERE m.issuer=NEW.operator_issuer AND m.subject=NEW.operator_subject FOR SHARE OF m;
    IF NOT EXISTS(SELECT 1 FROM public.portal_memberships m
      JOIN public.portal_membership_transitions h ON h.incarnation=m.incarnation
      WHERE m.issuer=NEW.operator_issuer AND m.subject=NEW.operator_subject
      AND m.client_id=NEW.operator_client AND m.role='operator' AND NOT m.revoked
      AND m.incarnation=NEW.operator_incarnation AND h.issuer=m.issuer AND h.subject=m.subject
      AND h.client_id=m.client_id AND h.role=m.role AND NOT h.revoked
      AND h.kind IN ('baseline','insert','update')) THEN
      RAISE EXCEPTION 'service issuer membership refused' USING ERRCODE='23514';
    END IF;
  ELSIF num_nonnulls(NEW.operator_client,NEW.operator_incarnation,NEW.content_hash)<>0 THEN
    RAISE EXCEPTION 'legacy or withdrawal provenance refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;

ALTER TABLE public.portal_orders
  ADD COLUMN appointment_reservation_id public.portal_consent_key,
  ADD COLUMN appointment_revision integer,
  ADD CONSTRAINT portal_order_appointment_slot FOREIGN KEY (appointment_reservation_id,appointment_revision)
    REFERENCES public.portal_appointment_revisions(reservation_id,revision) MATCH FULL,
  ADD CONSTRAINT portal_order_appointment_reservation UNIQUE(appointment_reservation_id);

CREATE OR REPLACE FUNCTION public.portal_guard_service_order() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  proposal public.portal_proposals%ROWTYPE;
  agreement public.portal_proposal_acceptances%ROWTYPE;
BEGIN
  SELECT * INTO agreement FROM public.portal_proposal_acceptances
    WHERE id=NEW.acceptance_id AND request_id=NEW.request_id AND client_id=NEW.client_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'service order acceptance refused' USING ERRCODE='23514'; END IF;
  SELECT * INTO proposal FROM public.portal_proposals WHERE request_id=NEW.request_id
    AND client_id=NEW.client_id AND version=agreement.proposal_version;
  IF NOT FOUND THEN RAISE EXCEPTION 'service order parent refused' USING ERRCODE='23514'; END IF;
  IF proposal.content_format=2 THEN
    IF NEW.service_content_format IS DISTINCT FROM 2 OR agreement.client_incarnation IS NULL
      OR agreement.content_hash IS DISTINCT FROM proposal.content_sha256 THEN
      RAISE EXCEPTION 'old or mismatched service order writer refused' USING ERRCODE='23514';
    END IF;
  ELSIF NEW.service_content_format IS NOT NULL THEN
    RAISE EXCEPTION 'legacy service order relabel refused' USING ERRCODE='23514';
  END IF;
  IF proposal.service_kind='appointment' THEN
    IF NEW.one_time_minor<=0 OR NEW.one_time_minor<>proposal.one_time_minor OR NEW.monthly_minor<>0
      OR proposal.monthly_minor<>0 OR NOT EXISTS(
        SELECT 1 FROM public.portal_appointment_reservations r
        JOIN public.portal_appointment_revisions v ON v.reservation_id=r.id AND v.client_id=r.client_id
        WHERE r.id=NEW.appointment_reservation_id AND r.client_id=NEW.client_id
          AND r.quote_request_id=NEW.request_id AND r.quote_version=proposal.version
          AND r.quote_sha256=proposal.content_sha256 AND v.revision=NEW.appointment_revision
          AND v.held_until>clock_timestamp()
          AND NOT EXISTS(SELECT 1 FROM public.portal_appointment_revisions later
            WHERE later.reservation_id=r.id AND later.revision>v.revision)) THEN
      RAISE EXCEPTION 'appointment order slot refused' USING ERRCODE='23514';
    END IF;
  ELSIF num_nonnulls(NEW.appointment_reservation_id,NEW.appointment_revision)<>0 THEN
    RAISE EXCEPTION 'non-appointment order slot refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;

ALTER TABLE public.portal_mail_outbox
  DROP CONSTRAINT portal_mail_outbox_producer_format_check,
  DROP CONSTRAINT portal_mail_complete_producer,
  ADD CONSTRAINT portal_mail_outbox_producer_format_check CHECK(producer_format IN (1,2,3)),
  ADD CONSTRAINT portal_mail_complete_producer CHECK(
    num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=0
    OR (num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=6
      AND producer_format IN (1,2,3) AND kind='order-confirmation'));

CREATE OR REPLACE FUNCTION public.portal_guard_service_confirmation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  proposal public.portal_proposals%ROWTYPE;
  agreement public.portal_proposal_acceptances%ROWTYPE;
  placed public.portal_orders%ROWTYPE;
  issuance public.portal_proposal_visibility%ROWTYPE;
  expected_producer integer;
BEGIN
  IF NEW.kind<>'order-confirmation' THEN RETURN NEW; END IF;
  SELECT * INTO placed FROM public.portal_orders WHERE id=NEW.order_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'confirmation order binding refused' USING ERRCODE='23514'; END IF;
  SELECT * INTO agreement FROM public.portal_proposal_acceptances WHERE id=placed.acceptance_id
    AND request_id=placed.request_id AND client_id=placed.client_id;
  IF NOT FOUND THEN RAISE EXCEPTION 'confirmation acceptance binding refused' USING ERRCODE='23514'; END IF;
  SELECT * INTO proposal FROM public.portal_proposals WHERE request_id=placed.request_id
    AND client_id=placed.client_id AND version=agreement.proposal_version;
  IF NOT FOUND THEN RAISE EXCEPTION 'confirmation parent binding refused' USING ERRCODE='23514'; END IF;
  IF proposal.content_format=1 THEN
    IF NEW.producer_format IS NOT NULL AND NEW.producer_format<>1 THEN
      RAISE EXCEPTION 'legacy producer relabel refused' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
  END IF;
  expected_producer := 2;
  IF proposal.service_kind='appointment' THEN
    IF placed.appointment_reservation_id IS NULL OR placed.appointment_revision IS NULL THEN
      RAISE EXCEPTION 'confirmation appointment slot refused' USING ERRCODE='23514';
    END IF;
    expected_producer := 3;
  END IF;
  SELECT * INTO issuance FROM public.portal_proposal_visibility WHERE request_id=placed.request_id
    AND client_id=placed.client_id AND event=agreement.visibility_event
    AND proposal_version=agreement.proposal_version AND action='share';
  IF NOT FOUND THEN RAISE EXCEPTION 'confirmation issuance binding refused' USING ERRCODE='23514'; END IF;
  IF NEW.producer_format IS DISTINCT FROM expected_producer OR placed.service_content_format IS DISTINCT FROM 2
    OR NEW.acceptance_id IS DISTINCT FROM agreement.id
    OR NEW.proposal_version IS DISTINCT FROM proposal.version
    OR NEW.proposal_sha256 IS DISTINCT FROM proposal.content_sha256
    OR agreement.content_hash IS DISTINCT FROM proposal.content_sha256
    OR issuance.content_hash IS DISTINCT FROM proposal.content_sha256
    OR agreement.client_incarnation IS NULL OR issuance.operator_client IS NULL
    OR issuance.operator_incarnation IS NULL THEN
    RAISE EXCEPTION 'complete typed confirmation evidence refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
