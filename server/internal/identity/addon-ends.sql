-- A client's end of one monthly add-on where they ordered it (Consumer Contracts
-- Act, section 28 a): it is billed through last_month and no longer after. The
-- order's monthly price still includes it; billing leaves ended add-ons out of
-- each later month.
CREATE TABLE portal_addon_ends (
  order_id text NOT NULL,
  addon_id text NOT NULL,
  last_month date NOT NULL CHECK(extract(day FROM last_month) = 1),
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(order_id, addon_id),
  FOREIGN KEY(order_id, addon_id) REFERENCES portal_order_addons(order_id, addon_id)
);

-- Stripe's confirmation that a card subscription no longer charges an ended
-- add-on. An end without one is asked again until Stripe confirms.
CREATE TABLE portal_addon_removals (
  order_id text NOT NULL,
  addon_id text NOT NULL,
  removed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(order_id, addon_id),
  FOREIGN KEY(order_id, addon_id) REFERENCES portal_addon_ends(order_id, addon_id)
);
