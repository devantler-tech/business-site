-- Append-only authority evidence, never a queue or execution grant.
ALTER TABLE portal_invoices ADD CONSTRAINT portal_invoice_work_binding UNIQUE
  (request_id,client_id,sequence,acceptance_id,proposal_version,visibility_event,content_hash,net_minor,vat_minor,amount_minor);

CREATE TABLE portal_work_approvals (
  id portal_consent_key PRIMARY KEY,
  request_id text NOT NULL,
  client_id text NOT NULL,
  submission_key portal_consent_key NOT NULL,
  expected_generation bigint NOT NULL CHECK(expected_generation >= 0),
  acceptance_id text NOT NULL,
  invoice_sequence integer NOT NULL,
  proposal_version integer NOT NULL,
  visibility_event portal_consent_event NOT NULL,
  content_hash portal_consent_hash NOT NULL,
  net_minor bigint NOT NULL CHECK(net_minor BETWEEN 0 AND 1000000000),
  vat_minor bigint NOT NULL CHECK(vat_minor BETWEEN 0 AND 1000000000),
  amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000 AND net_minor+vat_minor=amount_minor),
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  client_incarnation uuid NOT NULL REFERENCES portal_membership_transitions(incarnation),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  operator_client text NOT NULL,
  operator_incarnation uuid NOT NULL REFERENCES portal_membership_transitions(incarnation),
  evidence bytea NOT NULL CHECK(octet_length(evidence) BETWEEN 1 AND 2097152),
  evidence_hash portal_consent_hash NOT NULL CHECK(evidence_hash=encode(sha256(evidence),'hex')),
  decided_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK(expires_at=decided_at+interval '168 hours'),
  UNIQUE(request_id,submission_key),
  UNIQUE(id,request_id,client_id),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id),
  FOREIGN KEY(request_id,client_id,invoice_sequence,acceptance_id,proposal_version,visibility_event,content_hash,net_minor,vat_minor,amount_minor)
    REFERENCES portal_invoices(request_id,client_id,sequence,acceptance_id,proposal_version,visibility_event,content_hash,net_minor,vat_minor,amount_minor)
);

CREATE TABLE portal_work_revocations (
  approval_id portal_consent_key PRIMARY KEY,
  request_id text NOT NULL,
  client_id text NOT NULL,
  submission_key portal_consent_key NOT NULL,
  expected_generation bigint NOT NULL CHECK(expected_generation >= 0),
  reason text NOT NULL CHECK(reason IN ('revoked','superseded')),
  operator_issuer text NOT NULL,
  operator_subject text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(request_id,submission_key),
  FOREIGN KEY(approval_id,request_id,client_id) REFERENCES portal_work_approvals(id,request_id,client_id)
);

-- Only the pointer and its monotonic compare token may be updated. No DELETE
-- grant is required: a NULL selection records closure without an ABA reset.
CREATE TABLE portal_work_current (
  request_id text PRIMARY KEY,
  client_id text NOT NULL,
  approval_id portal_consent_key,
  generation bigint NOT NULL CHECK(generation > 0),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id),
  FOREIGN KEY(approval_id,request_id,client_id) REFERENCES portal_work_approvals(id,request_id,client_id)
);
