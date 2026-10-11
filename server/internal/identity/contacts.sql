CREATE TABLE portal_contacts (
  client_id text PRIMARY KEY CHECK (length(client_id)>0),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
  company text NOT NULL DEFAULT '' CHECK (length(company)<=160),
  email_verified boolean NOT NULL DEFAULT false CHECK (NOT email_verified),
  revision bigint NOT NULL CHECK (revision>0),
  updated_at timestamptz NOT NULL DEFAULT now()
);
