CREATE TABLE public.portal_membership_transitions (
  incarnation uuid PRIMARY KEY,
  previous_incarnation uuid REFERENCES public.portal_membership_transitions(incarnation),
  kind text NOT NULL CHECK (kind IN ('baseline','insert','update','delete')),
  issuer text NOT NULL,
  subject text NOT NULL,
  client_id text NOT NULL CHECK (length(client_id)>0),
  role text NOT NULL CHECK (role IN ('client','operator')),
  revoked boolean NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind IN ('baseline','insert')) = (previous_incarnation IS NULL))
);

ALTER TABLE public.portal_memberships
  ADD COLUMN incarnation uuid NOT NULL DEFAULT pg_catalog.gen_random_uuid();

INSERT INTO public.portal_membership_transitions
  (incarnation,kind,issuer,subject,client_id,role,revoked)
  SELECT incarnation,'baseline',issuer,subject,client_id,role,revoked
  FROM public.portal_memberships;

CREATE FUNCTION public.portal_assign_membership_incarnation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, pg_temp AS $$
BEGIN
  IF TG_RELID <> 'public.portal_memberships'::regclass OR TG_WHEN <> 'BEFORE'
     OR TG_LEVEL <> 'ROW' OR TG_OP NOT IN ('INSERT','UPDATE') OR TG_NARGS <> 0 THEN
    RAISE EXCEPTION 'invalid membership transition invocation';
  END IF;
  NEW.incarnation := pg_catalog.gen_random_uuid();
  RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_assign_membership_incarnation() FROM PUBLIC;

CREATE FUNCTION public.portal_record_membership_transition() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
  snapshot public.portal_memberships%ROWTYPE;
  event_id uuid;
  prior uuid;
BEGIN
  IF TG_RELID <> 'public.portal_memberships'::regclass OR TG_WHEN <> 'AFTER'
     OR TG_LEVEL <> 'ROW' OR TG_OP NOT IN ('INSERT','UPDATE','DELETE') OR TG_NARGS <> 0 THEN
    RAISE EXCEPTION 'invalid membership transition invocation';
  END IF;
  IF TG_OP = 'DELETE' THEN
    snapshot := OLD;
    event_id := pg_catalog.gen_random_uuid();
  ELSE
    snapshot := NEW;
    event_id := NEW.incarnation;
  END IF;
  IF TG_OP IN ('UPDATE','DELETE') THEN
    prior := OLD.incarnation;
  END IF;
  INSERT INTO public.portal_membership_transitions
    (incarnation,previous_incarnation,kind,issuer,subject,client_id,role,revoked)
    VALUES (event_id,prior,pg_catalog.lower(TG_OP),snapshot.issuer,snapshot.subject,
            snapshot.client_id,snapshot.role,snapshot.revoked);
  RETURN NULL;
END;
$$;
REVOKE ALL ON FUNCTION public.portal_record_membership_transition() FROM PUBLIC;

CREATE TRIGGER portal_membership_incarnation
  BEFORE INSERT OR UPDATE ON public.portal_memberships
  FOR EACH ROW EXECUTE FUNCTION public.portal_assign_membership_incarnation();
CREATE TRIGGER portal_membership_transition
  AFTER INSERT OR UPDATE OR DELETE ON public.portal_memberships
  FOR EACH ROW EXECUTE FUNCTION public.portal_record_membership_transition();
