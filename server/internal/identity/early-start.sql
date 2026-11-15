-- Danish-law check (#21): the client's express request, given when ordering,
-- that work starts within the 14-day withdrawal period, with their
-- acknowledgement that a withdrawal then costs the work done so far and that
-- the right ends once the work is completely finished (Consumer Contracts Act,
-- sections 18(2)(2), 21 and 25). Without it, no work starts within the period.
ALTER TABLE portal_orders ADD COLUMN early_start_at timestamptz;

-- The operator's record that early-started work is completely finished; the
-- right of withdrawal then ends.
CREATE TABLE portal_order_completions (
  order_id text PRIMARY KEY REFERENCES portal_orders(id),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  completed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- The amount owed for the work done before a withdrawal that followed an
-- early start, recorded once by the operator; the rest of what was paid is
-- refunded.
CREATE TABLE portal_withdrawal_settlements (
  order_id text PRIMARY KEY REFERENCES portal_order_withdrawals(order_id),
  owed_minor bigint NOT NULL CHECK(owed_minor BETWEEN 0 AND 1000000000),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
