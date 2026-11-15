-- ADR 0030 card path: each card order's current Stripe Checkout Session. A new
-- session replaces an expired one; a completed session is never replaced.
CREATE TABLE portal_card_checkouts (
  order_id text PRIMARY KEY REFERENCES portal_orders(id),
  session_id text NOT NULL UNIQUE CHECK(session_id ~ '^cs_[A-Za-z0-9_]{1,250}$'),
  url text NOT NULL CHECK(url LIKE 'https://%' AND length(url) <= 2048),
  expires_at timestamptz NOT NULL,
  attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 1000),
  completed boolean NOT NULL DEFAULT false,
  subscription_id text UNIQUE CHECK(subscription_id IS NULL OR subscription_id ~ '^sub_[A-Za-z0-9_]{1,250}$'),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Each signature-verified Stripe event is applied exactly once.
CREATE TABLE portal_stripe_events (
  id text PRIMARY KEY CHECK(id ~ '^evt_[A-Za-z0-9_]{1,250}$'),
  type text NOT NULL CHECK(length(type) BETWEEN 1 AND 100),
  received_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
