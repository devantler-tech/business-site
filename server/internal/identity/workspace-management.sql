CREATE TABLE portal_workspace_settings (
  workspace_id text PRIMARY KEY REFERENCES portal_workspaces(id) ON DELETE CASCADE,
  display_name text CHECK (char_length(display_name) BETWEEN 1 AND 100),
  closed_at timestamptz
);
