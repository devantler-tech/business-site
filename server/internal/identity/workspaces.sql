CREATE TABLE portal_workspaces (
  id text PRIMARY KEY CHECK (length(id)>0),
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO portal_workspaces(id)
SELECT client_id FROM portal_memberships
UNION
SELECT client_id FROM portal_invitations;
