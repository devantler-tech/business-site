-- The operator's record that a request is finished or will not go ahead. A client
-- with hosting has one request in progress at a time (fair use, BILLING.md); a
-- closed request no longer counts.
CREATE TABLE portal_request_closures (
  request_id text PRIMARY KEY,
  client_id text NOT NULL,
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  closed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id)
);
