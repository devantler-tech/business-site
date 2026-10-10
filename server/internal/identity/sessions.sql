-- Migration 2: maintained SCS pgxstore format; cookie tokens are hashed by SCS.
CREATE TABLE portal_sessions (
    token text PRIMARY KEY,
    data bytea NOT NULL,
    expiry timestamptz NOT NULL
);
CREATE INDEX portal_sessions_expiry ON portal_sessions(expiry);
