CREATE TABLE portal_preview_receipts (
  acceptance_id text PRIMARY KEY REFERENCES portal_proposal_acceptances(id),
  request_id text NOT NULL,
  client_id text NOT NULL CHECK(client_id='preview-demo'),
  one_time_minor bigint NOT NULL CHECK(one_time_minor>=0),
  monthly_minor bigint NOT NULL CHECK(monthly_minor>=0),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id)
);
