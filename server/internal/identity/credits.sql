-- Reserve an external transfer once, globally and case-sensitively. Invalidation
-- does not release its identity for allocation to another invoice or client.
ALTER TABLE portal_invoices ADD CONSTRAINT portal_invoice_credit_binding
  UNIQUE(request_id,client_id,sequence);
CREATE TABLE portal_credit_transfers (
  source text COLLATE "C" NOT NULL CHECK(source ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$'),
  reference text COLLATE "C" NOT NULL CHECK(reference ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,63}$'),
  request_id text NOT NULL,
  client_id text NOT NULL,
  invoice_sequence integer NOT NULL,
  ordinal integer NOT NULL CHECK(ordinal BETWEEN 1 AND 32),
  PRIMARY KEY(source,reference),
  UNIQUE(request_id,invoice_sequence,ordinal),
  UNIQUE(source,reference,request_id,client_id,invoice_sequence),
  FOREIGN KEY(request_id,client_id,invoice_sequence)
    REFERENCES portal_invoices(request_id,client_id,sequence)
);
CREATE TABLE portal_credit_revisions (
  source text COLLATE "C" NOT NULL,
  reference text COLLATE "C" NOT NULL,
  request_id text NOT NULL,
  client_id text NOT NULL,
  invoice_sequence integer NOT NULL,
  revision integer NOT NULL CHECK(revision BETWEEN 1 AND 16),
  expected_revision integer NOT NULL CHECK(expected_revision=revision-1),
  submission_key portal_consent_key NOT NULL,
  state text NOT NULL CHECK(state IN ('confirmed','unverified','disputed','invalidated')),
  amount_minor bigint NOT NULL,
  note text NOT NULL CHECK(char_length(note) BETWEEN 1 AND 300),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(source,reference,revision),
  UNIQUE(request_id,submission_key),
  CHECK((state='invalidated' AND amount_minor=0) OR
        (state<>'invalidated' AND amount_minor BETWEEN 1 AND 1000000000)),
  CHECK(revision<16 OR state='invalidated'),
  FOREIGN KEY(source,reference,request_id,client_id,invoice_sequence)
    REFERENCES portal_credit_transfers(source,reference,request_id,client_id,invoice_sequence)
);
