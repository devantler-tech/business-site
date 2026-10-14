-- Shared binding constraints have one definition. Null refusal remains on each
-- column: a domain alone cannot provide the same table-level null guarantee.
CREATE DOMAIN portal_consent_key AS text CHECK(VALUE ~ '^[A-Z2-7]{26}$');
CREATE DOMAIN portal_consent_event AS integer CHECK(VALUE BETWEEN 1 AND 100);
CREATE DOMAIN portal_consent_hash AS text CHECK(VALUE ~ '^[0-9a-f]{64}$');

-- Review contexts are disposable presentation state, never consent authority alone.
CREATE TABLE portal_proposal_reviews (
  request_id text NOT NULL,
  client_id text NOT NULL,
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  context_key portal_consent_key NOT NULL,
  proposal_version integer NOT NULL,
  visibility_event portal_consent_event NOT NULL,
  content_hash portal_consent_hash NOT NULL,
  expires_at timestamptz NOT NULL,
  PRIMARY KEY(request_id,client_id,client_issuer,client_subject),
  UNIQUE(request_id,context_key),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id),
  FOREIGN KEY(request_id,proposal_version) REFERENCES portal_proposals(request_id,version)
);

-- Never reference the mutable context: rotation cannot change historical consent.
CREATE TABLE portal_proposal_acceptances (
  id text PRIMARY KEY CHECK(id ~ '^[A-Z2-7]{26}$'),
  request_id text NOT NULL,
  client_id text NOT NULL,
  client_issuer text NOT NULL,
  client_subject text NOT NULL,
  context_key portal_consent_key NOT NULL,
  proposal_version integer NOT NULL,
  visibility_event portal_consent_event NOT NULL,
  content_hash portal_consent_hash NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(request_id,client_id,proposal_version),
  UNIQUE(request_id,context_key),
  FOREIGN KEY(request_id,client_id) REFERENCES portal_requests(id,client_id),
  FOREIGN KEY(request_id,proposal_version) REFERENCES portal_proposals(request_id,version)
);
