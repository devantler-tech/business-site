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
