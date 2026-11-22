-- ADR 0030: an accepted proposal becomes one order with exactly one collection
-- path, chosen by the client. One order per request; a later iteration is a new
-- request. Amounts are copied from the accepted proposal version.
CREATE TABLE portal_orders (
  id text PRIMARY KEY CHECK(id ~ '^[A-Z2-7]{26}$'),
  request_id text NOT NULL UNIQUE,
  client_id text NOT NULL,
  acceptance_id text NOT NULL UNIQUE REFERENCES portal_proposal_acceptances(id),
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  collection text NOT NULL CHECK(collection IN ('invoice','card')),
  one_time_minor bigint NOT NULL CHECK(one_time_minor BETWEEN 0 AND 1000000000),
  monthly_minor bigint NOT NULL CHECK(monthly_minor BETWEEN 0 AND 1000000000),
  monthly_from date NOT NULL CHECK(extract(day FROM monthly_from) = 1),
  placed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK(one_time_minor > 0 OR monthly_minor > 0),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id)
);

-- One funding record per order and billing period. The identity is recorded
-- before any provider call, sent as the idempotency key and written into the
-- invoice memo, so an unknown result can be found again instead of duplicated.
CREATE TABLE portal_order_charges (
  order_id text NOT NULL REFERENCES portal_orders(id),
  period text NOT NULL CHECK(period = 'acceptance' OR period ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
  identity text NOT NULL UNIQUE CHECK(identity = 'portal-' || order_id || '-' || period),
  gross_minor bigint NOT NULL CHECK(gross_minor > 0),
  net_minor bigint NOT NULL CHECK(net_minor > 0),
  vat_minor bigint NOT NULL CHECK(vat_minor >= 0 AND net_minor + vat_minor = gross_minor),
  state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','held','created','booked','settled')),
  provider_ref text CHECK(provider_ref IS NULL OR length(provider_ref) BETWEEN 1 AND 100),
  provider_number integer,
  sent boolean NOT NULL DEFAULT false,
  settled_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(order_id,period),
  CHECK(state IN ('pending','held') OR provider_ref IS NOT NULL),
  CHECK((state = 'settled') = (settled_at IS NOT NULL))
);

-- The client's Numina contact, created once and then only updated.
CREATE TABLE portal_numina_contacts (
  client_id text PRIMARY KEY CHECK(length(client_id) > 0),
  contact_id text NOT NULL UNIQUE CHECK(length(contact_id) BETWEEN 1 AND 100)
);
