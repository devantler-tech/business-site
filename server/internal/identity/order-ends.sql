-- #591: ending and reversing orders. An order's monthly billing ends after one
-- last billed month, recorded once: by an operator, or from Stripe when the
-- card subscription ends on its own. Nothing is billed after that month.
CREATE TABLE portal_order_ends (
  order_id text PRIMARY KEY REFERENCES portal_orders(id),
  last_month date NOT NULL CHECK(extract(day FROM last_month) = 1),
  source text NOT NULL CHECK(source IN ('operator','stripe')),
  operator_issuer text,
  operator_subject text,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK((source = 'operator') = (operator_issuer IS NOT NULL AND operator_subject IS NOT NULL))
);

-- The card subscription's end once Stripe has confirmed it.
CREATE TABLE portal_card_cancellations (
  subscription_id text PRIMARY KEY CHECK(subscription_id ~ '^sub_[A-Za-z0-9_]{1,250}$'),
  order_id text NOT NULL REFERENCES portal_order_ends(order_id),
  cancel_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- A funded charge that no longer funds its order: a Stripe refund or dispute.
-- The charge row keeps its settlement; this record is the reversal.
CREATE TABLE portal_charge_reversals (
  order_id text NOT NULL,
  period text NOT NULL,
  reason text NOT NULL CHECK(reason IN ('refunded','disputed')),
  reference text NOT NULL CHECK(reference ~ '^(ch|py|du)_[A-Za-z0-9_]{1,250}$'),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(order_id, period, reason),
  FOREIGN KEY(order_id, period) REFERENCES portal_order_charges(order_id, period)
);
