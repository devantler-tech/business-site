ALTER TABLE portal_requests ADD CONSTRAINT portal_request_client UNIQUE(id,client_id);
CREATE TABLE portal_proposals (
  request_id text NOT NULL,
  client_id text NOT NULL,
  version integer NOT NULL CHECK(version BETWEEN 1 AND 50),
  expected_version integer NOT NULL CHECK(expected_version = version - 1),
  submission_key text NOT NULL CHECK(submission_key ~ '^[A-Z2-7]{26}$'),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  scope text NOT NULL CHECK(char_length(scope) BETWEEN 1 AND 1200),
  deliverables text NOT NULL CHECK(char_length(deliverables) BETWEEN 1 AND 1200),
  currency text NOT NULL CHECK(currency = 'DKK'),
  one_time_minor bigint NOT NULL CHECK(one_time_minor BETWEEN 0 AND 1000000000 AND one_time_minor % 100 = 0),
  monthly_minor bigint NOT NULL CHECK(monthly_minor BETWEEN 0 AND 1000000000 AND monthly_minor % 100 = 0),
  one_time_terms text NOT NULL CHECK(char_length(one_time_terms) BETWEEN 1 AND 600),
  monthly_terms text NOT NULL CHECK(char_length(monthly_terms) BETWEEN 1 AND 600),
  prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(request_id,version),
  UNIQUE(request_id,submission_key),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id)
);
