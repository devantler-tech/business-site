-- Preserve old quote and receipt encodings. Format 1 is unclassified historical
-- content; adding that marker never certifies a service purpose or provenance.
CREATE FUNCTION public.portal_service_json_text(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog, pg_temp AS $$
  SELECT replace(replace(replace(replace(replace(to_json(value)::text,
    '<', '\u003c'), '>', '\u003e'), '&', '\u0026'), chr(8232), '\u2028'), chr(8233), '\u2029')
$$;

CREATE FUNCTION public.portal_service_terms_hash(english text, danish text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT SET search_path = pg_catalog, pg_temp AS $$
  SELECT encode(sha256(convert_to('{"English":'||public.portal_service_json_text(english)||
    ',"Danish":'||public.portal_service_json_text(danish)||'}','UTF8')),'hex')
$$;

CREATE FUNCTION public.portal_service_quote_hash(
  scope_text text, deliverables_text text, currency_text text, once_terms text,
  monthly_terms_text text, once_amount bigint, monthly_amount bigint,
  kind_text text, minutes integer, english text, danish text, terms_hash text
) RETURNS text LANGUAGE sql IMMUTABLE SET search_path = pg_catalog, pg_temp AS $$
  SELECT encode(sha256(convert_to('{"ContentFormat":2,"Scope":'||public.portal_service_json_text(scope_text)||
    ',"Deliverables":'||public.portal_service_json_text(deliverables_text)||
    ',"Currency":'||public.portal_service_json_text(currency_text)||
    ',"OneTimeTerms":'||public.portal_service_json_text(once_terms)||
    ',"MonthlyTerms":'||public.portal_service_json_text(monthly_terms_text)||
    ',"OneTimeMinor":'||once_amount::text||',"MonthlyMinor":'||monthly_amount::text||
    ',"Service":{"Kind":'||public.portal_service_json_text(kind_text)||
    ',"DurationMinutes":'||coalesce(minutes,0)::text||
    ',"AppointmentTermsEN":'||public.portal_service_json_text(coalesce(english,''))||
    ',"AppointmentTermsDA":'||public.portal_service_json_text(coalesce(danish,''))||
    ',"AppointmentTermsSHA256":'||public.portal_service_json_text(coalesce(terms_hash,''))||'}}','UTF8')),'hex')
$$;

ALTER TABLE public.portal_proposals
  ADD COLUMN content_format integer NOT NULL DEFAULT 1,
  ADD COLUMN service_kind text,
  ADD COLUMN duration_minutes integer,
  ADD COLUMN appointment_terms_en text,
  ADD COLUMN appointment_terms_da text,
  ADD COLUMN appointment_terms_sha256 public.portal_consent_hash,
  ADD COLUMN content_sha256 public.portal_consent_hash,
  ADD CONSTRAINT portal_proposal_service_content CHECK (
    (content_format=1 AND num_nonnulls(service_kind,duration_minutes,appointment_terms_en,
      appointment_terms_da,appointment_terms_sha256,content_sha256)=0)
    OR (content_format=2 AND service_kind IS NOT NULL AND content_sha256 IS NOT NULL
      AND ((service_kind='software' AND duration_minutes=0
        AND num_nonnulls(appointment_terms_en,appointment_terms_da,appointment_terms_sha256)=0)
      OR (service_kind='appointment' AND duration_minutes BETWEEN 1 AND 1440
        AND num_nonnulls(appointment_terms_en,appointment_terms_da,appointment_terms_sha256)=3
        AND char_length(appointment_terms_en) BETWEEN 1 AND 2000
        AND char_length(appointment_terms_da) BETWEEN 1 AND 2000
        AND btrim(appointment_terms_en)<>'' AND btrim(appointment_terms_da)<>''
        AND appointment_terms_sha256=public.portal_service_terms_hash(appointment_terms_en,appointment_terms_da)))
      AND duration_minutes IS NOT NULL
      AND content_sha256=public.portal_service_quote_hash(scope,deliverables,currency,one_time_terms,
        monthly_terms,one_time_minor,monthly_minor,service_kind,duration_minutes,
        appointment_terms_en,appointment_terms_da,appointment_terms_sha256))),
  ADD CONSTRAINT portal_proposal_service_binding UNIQUE(request_id,client_id,version,content_sha256,service_kind);

ALTER TABLE public.portal_proposal_visibility
  ADD COLUMN operator_client text,
  ADD COLUMN operator_incarnation uuid REFERENCES public.portal_membership_transitions(incarnation),
  ADD COLUMN content_hash public.portal_consent_hash,
  ADD CONSTRAINT portal_visibility_service_provenance CHECK(
    num_nonnulls(operator_client,operator_incarnation,content_hash) IN (0,3));

ALTER TABLE public.portal_proposal_acceptances
  ADD COLUMN client_incarnation uuid REFERENCES public.portal_membership_transitions(incarnation),
  ADD CONSTRAINT portal_acceptance_service_binding UNIQUE(id,request_id,client_id,proposal_version,content_hash);

-- The marker is server-computed lineage, not an authority supplied by a browser.
-- Old order INSERTs cannot silently consume a newly classified acceptance.
ALTER TABLE public.portal_orders
  ADD COLUMN service_content_format integer,
  ADD CONSTRAINT portal_order_service_marker CHECK(service_content_format IS NULL OR service_content_format=2),
  ADD CONSTRAINT portal_order_service_binding UNIQUE(id,request_id,client_id,acceptance_id);

CREATE FUNCTION public.portal_guard_service_visibility() RETURNS trigger
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
    IF proposal.service_kind='appointment' THEN
      RAISE EXCEPTION 'appointment issuance unavailable' USING ERRCODE='23514';
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
REVOKE ALL ON FUNCTION public.portal_guard_service_visibility() FROM PUBLIC;
CREATE TRIGGER portal_service_visibility BEFORE INSERT ON public.portal_proposal_visibility
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_service_visibility();

CREATE FUNCTION public.portal_guard_service_acceptance() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  proposal public.portal_proposals%ROWTYPE;
  issuance public.portal_proposal_visibility%ROWTYPE;
BEGIN
  SELECT * INTO proposal FROM public.portal_proposals
    WHERE request_id=NEW.request_id AND client_id=NEW.client_id AND version=NEW.proposal_version;
  IF NOT FOUND THEN RAISE EXCEPTION 'service proposal binding refused' USING ERRCODE='23514'; END IF;
  IF proposal.content_format=1 THEN
    IF NEW.client_incarnation IS NOT NULL THEN
      RAISE EXCEPTION 'legacy acceptance relabel refused' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
  END IF;
  SELECT * INTO issuance FROM public.portal_proposal_visibility
    WHERE request_id=NEW.request_id AND client_id=NEW.client_id AND event=NEW.visibility_event
      AND proposal_version=NEW.proposal_version AND action='share';
  IF NOT FOUND THEN RAISE EXCEPTION 'service issuance binding refused' USING ERRCODE='23514'; END IF;
  IF NEW.client_incarnation IS NULL OR NEW.content_hash IS DISTINCT FROM proposal.content_sha256
    OR issuance.content_hash IS DISTINCT FROM proposal.content_sha256
    OR issuance.operator_client IS NULL OR issuance.operator_incarnation IS NULL THEN
    RAISE EXCEPTION 'service acceptance provenance refused' USING ERRCODE='23514';
  END IF;
  -- Every new writer and its database guard take the complete real union in
  -- the same identity order after the request fence, never client first.
  PERFORM 1 FROM public.portal_memberships m
    WHERE (m.issuer=issuance.operator_issuer AND m.subject=issuance.operator_subject)
       OR (m.issuer=NEW.client_issuer AND m.subject=NEW.client_subject)
    ORDER BY m.issuer,m.subject FOR SHARE OF m;
  IF NOT EXISTS(SELECT 1 FROM public.portal_memberships m
    JOIN public.portal_membership_transitions h ON h.incarnation=m.incarnation
    WHERE m.issuer=issuance.operator_issuer AND m.subject=issuance.operator_subject
      AND m.client_id=issuance.operator_client AND m.role='operator' AND NOT m.revoked
      AND m.incarnation=issuance.operator_incarnation AND h.issuer=m.issuer AND h.subject=m.subject
      AND h.client_id=m.client_id AND h.role=m.role AND NOT h.revoked
      AND h.kind IN ('baseline','insert','update'))
    OR NOT EXISTS(SELECT 1 FROM public.portal_memberships m
    JOIN public.portal_membership_transitions h ON h.incarnation=m.incarnation
    WHERE m.issuer=NEW.client_issuer AND m.subject=NEW.client_subject
      AND m.client_id=NEW.client_id AND m.role='client' AND NOT m.revoked
      AND m.incarnation=NEW.client_incarnation AND h.issuer=m.issuer AND h.subject=m.subject
      AND h.client_id=m.client_id AND h.role=m.role AND NOT h.revoked
      AND h.kind IN ('baseline','insert','update')) THEN
    RAISE EXCEPTION 'service accepting membership refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_service_acceptance() FROM PUBLIC;
CREATE TRIGGER portal_service_acceptance BEFORE INSERT ON public.portal_proposal_acceptances
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_service_acceptance();

CREATE FUNCTION public.portal_guard_service_order() RETURNS trigger
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
    IF proposal.service_kind='appointment' THEN
      RAISE EXCEPTION 'appointment prepayment unavailable' USING ERRCODE='23514';
    END IF;
    IF NEW.service_content_format IS DISTINCT FROM 2 OR agreement.client_incarnation IS NULL
      OR agreement.content_hash IS DISTINCT FROM proposal.content_sha256 THEN
      RAISE EXCEPTION 'old or mismatched service order writer refused' USING ERRCODE='23514';
    END IF;
  ELSIF NEW.service_content_format IS NOT NULL THEN
    RAISE EXCEPTION 'legacy service order relabel refused' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_guard_service_order() FROM PUBLIC;
CREATE TRIGGER portal_service_order BEFORE INSERT ON public.portal_orders
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_service_order();

ALTER TABLE public.portal_mail_outbox
  DROP CONSTRAINT portal_mail_outbox_producer_format_check,
  DROP CONSTRAINT portal_mail_complete_producer,
  ADD CONSTRAINT portal_mail_outbox_producer_format_check CHECK(producer_format IN (1,2)),
  ADD CONSTRAINT portal_mail_complete_producer CHECK(
    num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=0
    OR (num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=6
      AND producer_format IN (1,2) AND kind='order-confirmation'));

CREATE FUNCTION public.portal_guard_service_confirmation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  proposal public.portal_proposals%ROWTYPE;
  agreement public.portal_proposal_acceptances%ROWTYPE;
  placed public.portal_orders%ROWTYPE;
  issuance public.portal_proposal_visibility%ROWTYPE;
BEGIN
  -- Withdrawal rights retain their independent all-NULL metadata path.
  IF NEW.kind<>'order-confirmation' THEN RETURN NEW; END IF;
  -- The outbox references the unique order; the order supplies the client.
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
  SELECT * INTO issuance FROM public.portal_proposal_visibility WHERE request_id=placed.request_id
    AND client_id=placed.client_id AND event=agreement.visibility_event
    AND proposal_version=agreement.proposal_version AND action='share';
  IF NOT FOUND THEN RAISE EXCEPTION 'confirmation issuance binding refused' USING ERRCODE='23514'; END IF;
  IF NEW.producer_format IS DISTINCT FROM 2 OR placed.service_content_format IS DISTINCT FROM 2
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
REVOKE ALL ON FUNCTION public.portal_guard_service_confirmation() FROM PUBLIC;
CREATE TRIGGER portal_service_confirmation BEFORE INSERT OR UPDATE ON public.portal_mail_outbox
  FOR EACH ROW EXECUTE FUNCTION public.portal_guard_service_confirmation();

-- One permanent root per accepted paid order. Consumer identity is append-only
-- and stable across new leases, approval receipts, retries and reschedules.
CREATE TABLE public.portal_service_fulfillments (
  id public.portal_consent_key PRIMARY KEY,
  request_id text NOT NULL,
  client_id text NOT NULL,
  order_id text NOT NULL UNIQUE,
  acceptance_id text NOT NULL UNIQUE,
  proposal_version integer NOT NULL,
  proposal_sha256 public.portal_consent_hash NOT NULL,
  service_kind text NOT NULL CHECK(service_kind IN ('software','appointment')),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(id,service_kind),
  FOREIGN KEY(order_id,request_id,client_id,acceptance_id)
    REFERENCES public.portal_orders(id,request_id,client_id,acceptance_id),
  FOREIGN KEY(acceptance_id,request_id,client_id,proposal_version,proposal_sha256)
    REFERENCES public.portal_proposal_acceptances(id,request_id,client_id,proposal_version,content_hash),
  FOREIGN KEY(request_id,client_id,proposal_version,proposal_sha256,service_kind)
    REFERENCES public.portal_proposals(request_id,client_id,version,content_sha256,service_kind)
);
CREATE TABLE public.portal_service_consumers (
  fulfillment_id public.portal_consent_key PRIMARY KEY,
  service_kind text NOT NULL CHECK(service_kind IN ('software','appointment')),
  aggregate_id text NOT NULL CHECK(char_length(aggregate_id) BETWEEN 1 AND 200),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(service_kind,aggregate_id),
  FOREIGN KEY(fulfillment_id,service_kind) REFERENCES public.portal_service_fulfillments(id,service_kind)
);
