-- The launch offer (offerings.json launchDiscount): the first customers who
-- order with hosting get a percentage off the order's one-time price. Each
-- place belongs to one client and the one order that got the discount, and it
-- is never reused: a withdrawn or ended order keeps its place. The order's
-- one_time_minor is already the discounted price.
CREATE TABLE portal_launch_places (
  place integer PRIMARY KEY CHECK(place > 0),
  client_id text UNIQUE CHECK(length(client_id) > 0),
  order_id text UNIQUE REFERENCES portal_orders(id),
  percent integer NOT NULL CHECK(percent BETWEEN 1 AND 99),
  list_minor bigint NOT NULL CHECK(list_minor BETWEEN 1 AND 1000000000),
  discount_minor bigint NOT NULL CHECK(discount_minor BETWEEN 1 AND list_minor - 1),
  taken_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK((client_id IS NULL) = (order_id IS NULL))
);
