-- One row per operator API call: who, what, when and how it ended (#640). It
-- holds no request bodies: the target is an ID, never customer details. The web
-- runtime only reads and appends; the migration job's retention purge removes
-- rows older than the retention period.
CREATE TABLE portal_operator_audit (
  id uuid PRIMARY KEY DEFAULT pg_catalog.gen_random_uuid(),
  at timestamptz NOT NULL DEFAULT clock_timestamp(),
  actor_issuer text NOT NULL,
  actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 512),
  actor_kind text NOT NULL CHECK (actor_kind IN ('operator','agent','other')),
  action text NOT NULL CHECK (action ~ '^[a-z][a-z.-]{0,63}$'),
  target text NOT NULL DEFAULT '' CHECK (length(target) <= 200),
  outcome text NOT NULL CHECK (outcome IN ('allowed','refused','failed'))
);
CREATE INDEX portal_operator_audit_at ON portal_operator_audit(at);
