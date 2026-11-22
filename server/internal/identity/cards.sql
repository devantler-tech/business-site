-- ADR 0030 card path: every Stripe Checkout Session the portal created for a
-- card order, kept so a delayed or replaced session still resolves to its
-- order. A one-off amount and a monthly subscription are separate sessions
-- (Stripe refuses one-time prices in a subscription without proration).
CREATE TABLE portal_card_checkouts (
  session_id text PRIMARY KEY CHECK(session_id ~ '^cs_[A-Za-z0-9_]{1,250}$'),
  order_id text NOT NULL REFERENCES portal_orders(id),
  purpose text NOT NULL CHECK(purpose IN ('one-time','monthly')),
  attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 1000),
  url text NOT NULL CHECK(url LIKE 'https://%' AND length(url) <= 2048),
  expires_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'open' CHECK(state IN ('open','processing','paid','closed')),
  subscription_id text UNIQUE CHECK(subscription_id IS NULL OR subscription_id ~ '^sub_[A-Za-z0-9_]{1,250}$'),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(order_id,purpose,attempt),
  CHECK(subscription_id IS NULL OR purpose = 'monthly')
);

-- Each signature-verified Stripe event is applied exactly once.
CREATE TABLE portal_stripe_events (
  id text PRIMARY KEY CHECK(id ~ '^evt_[A-Za-z0-9_]{1,250}$'),
  type text NOT NULL CHECK(length(type) BETWEEN 1 AND 100),
  received_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
