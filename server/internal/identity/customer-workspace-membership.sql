ALTER TABLE public.portal_memberships
  DROP CONSTRAINT portal_memberships_pkey,
  ADD PRIMARY KEY (issuer,subject,client_id);

CREATE UNIQUE INDEX portal_one_operator_membership
  ON public.portal_memberships (issuer,subject)
  WHERE role='operator';

CREATE INDEX portal_pending_invitation_subject
  ON public.portal_invitations (issuer,subject,client_id)
  WHERE NOT revoked;
