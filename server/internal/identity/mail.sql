-- Danish-law check (#21): the portal's own emails. An order's confirmation
-- (Consumer Contracts Act, section 13) and a withdrawal's receipt (section
-- 20 a) are queued with the order or withdrawal that causes them, and sent
-- through the site's mail sender (platform#5400).
CREATE TABLE portal_mail_outbox (
  id text PRIMARY KEY CHECK(id ~ '^[A-Z2-7]{26}$'),
  order_id text NOT NULL REFERENCES portal_orders(id),
  kind text NOT NULL CHECK(kind IN ('order-confirmation','withdrawal-receipt')),
  recipient text NOT NULL CHECK(length(recipient) BETWEEN 3 AND 254),
  subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
  body text NOT NULL CHECK(length(body) BETWEEN 1 AND 20000),
  queued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(order_id, kind)
);

-- One record per email the sender accepted for delivery.
CREATE TABLE portal_mail_deliveries (
  outbox_id text PRIMARY KEY REFERENCES portal_mail_outbox(id),
  delivered_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
