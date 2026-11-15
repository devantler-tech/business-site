-- Danish-law check (#21): a consumer withdraws from an order in the portal
-- within 14 days of placing it (Consumer Contracts Act, sections 18 and 20 a).
-- The withdrawal is recorded once, with the client who made it, and is never
-- changed. It stops all further billing; refunds stay with the operator.
CREATE TABLE portal_order_withdrawals (
  order_id text PRIMARY KEY REFERENCES portal_orders(id),
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  withdrawn_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Stripe's confirmed end of a withdrawn order's card subscription: before it
-- first charges, or at once. It takes precedence over any end scheduled
-- earlier, which stays recorded as it was.
CREATE TABLE portal_withdrawal_cancellations (
  subscription_id text PRIMARY KEY CHECK(subscription_id ~ '^sub_[A-Za-z0-9_]{1,250}$'),
  order_id text NOT NULL REFERENCES portal_order_withdrawals(order_id),
  cancel_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
