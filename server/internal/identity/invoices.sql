ALTER TABLE portal_proposal_acceptances ADD CONSTRAINT portal_acceptance_invoice_binding
  UNIQUE(id,request_id,client_id,proposal_version,visibility_event,content_hash);

-- External invoice metadata, not payment or execution authority. Existing rows
-- are never rewritten; the runtime receives SELECT/INSERT only.
CREATE TABLE portal_invoices (
  request_id text NOT NULL,
  client_id text NOT NULL,
  sequence integer NOT NULL CHECK(sequence BETWEEN 1 AND 50),
  expected_sequence integer NOT NULL CHECK(expected_sequence = sequence - 1),
  submission_key portal_consent_key NOT NULL,
  acceptance_id text NOT NULL,
  proposal_version integer NOT NULL,
  visibility_event portal_consent_event NOT NULL,
  content_hash portal_consent_hash NOT NULL,
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  reference text NOT NULL CHECK(reference ~ '^[A-Z0-9][A-Z0-9._/-]{0,63}$'),
  purpose text NOT NULL CHECK(char_length(purpose) BETWEEN 1 AND 300),
  currency text NOT NULL CHECK(currency = 'DKK'),
  amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000),
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(request_id,sequence),
  UNIQUE(request_id,submission_key),
  UNIQUE(reference),
  FOREIGN KEY(acceptance_id,request_id,client_id,proposal_version,visibility_event,content_hash)
    REFERENCES portal_proposal_acceptances(id,request_id,client_id,proposal_version,visibility_event,content_hash)
);
