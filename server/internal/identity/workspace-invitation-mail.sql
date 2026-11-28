-- Deliver one portal notice for an existing account, independently of
-- native sign-in account setup. Remove the outbox with its invitation.
CREATE TABLE public.portal_workspace_invitation_notices (
  invitation_digest bytea PRIMARY KEY REFERENCES public.portal_invitations(token_digest) ON DELETE CASCADE,
  native_setup boolean NOT NULL,
  recipient text,
  language text NOT NULL CHECK (language IN ('da','en')),
  sign_in_url text,
  last_attempt_at timestamptz,
  CHECK ((native_setup AND recipient IS NULL AND sign_in_url IS NULL)
    OR (NOT native_setup AND recipient IS NOT NULL AND char_length(recipient) BETWEEN 3 AND 254
      AND sign_in_url IS NOT NULL AND char_length(sign_in_url) BETWEEN 10 AND 2048))
);

CREATE TABLE public.portal_workspace_invitation_deliveries (
  invitation_digest bytea PRIMARY KEY REFERENCES public.portal_workspace_invitation_notices(invitation_digest) ON DELETE CASCADE,
  delivered_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
