-- Standard add-ons (offerings.json addons) the client chose when ordering. The
-- order's one_time_minor and monthly_minor already include them; these rows name
-- them on invoices and payments, at the unit price of the time of the order.
CREATE TABLE portal_order_addons (
  order_id text NOT NULL REFERENCES portal_orders(id),
  addon_id text NOT NULL CHECK(addon_id ~ '^[a-z][a-z-]{1,30}$'),
  kind text NOT NULL CHECK(kind IN ('one-time','monthly')),
  quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 10),
  unit_minor bigint NOT NULL CHECK(unit_minor BETWEEN 100 AND 10000000 AND unit_minor % 100 = 0),
  PRIMARY KEY(order_id, addon_id)
);
