CREATE TABLE portal_login_attempts (
  state_digest bytea PRIMARY KEY CHECK (octet_length(state_digest)=32),
  browser_digest bytea NOT NULL CHECK (octet_length(browser_digest)=32),
  nonce text NOT NULL CHECK (length(nonce)>0),
  pkce_verifier text NOT NULL CHECK (length(pkce_verifier) BETWEEN 43 AND 128),
  expires_at timestamptz NOT NULL
);
CREATE INDEX portal_login_attempts_expiry ON portal_login_attempts(expires_at);

CREATE TABLE portal_invitations (
  token_digest bytea PRIMARY KEY CHECK (octet_length(token_digest)=32),
  issuer text NOT NULL CHECK (length(issuer)>0),
  subject text NOT NULL CHECK (length(subject)>0),
  client_id text NOT NULL CHECK (length(client_id)>0),
  role text NOT NULL CHECK (role IN ('client','operator')),
  expires_at timestamptz NOT NULL,
  revoked boolean NOT NULL DEFAULT false
);
CREATE INDEX portal_invitations_expiry ON portal_invitations(expires_at);

CREATE TABLE portal_memberships (
  issuer text NOT NULL,
  subject text NOT NULL,
  client_id text NOT NULL CHECK (length(client_id)>0),
  role text NOT NULL CHECK (role IN ('client','operator')),
  revoked boolean NOT NULL DEFAULT false,
  PRIMARY KEY(issuer,subject)
);
