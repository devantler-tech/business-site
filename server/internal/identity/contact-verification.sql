ALTER TABLE portal_login_attempts
  ADD COLUMN purpose text NOT NULL DEFAULT 'sign-in' CHECK (purpose IN ('sign-in','contact')),
  ADD COLUMN verification_issuer text,
  ADD COLUMN verification_subject text,
  ADD COLUMN verification_client text,
  ADD COLUMN verification_email text,
  ADD COLUMN verification_revision bigint,
  ADD CONSTRAINT portal_contact_attempt_binding CHECK (
    (purpose='sign-in' AND verification_issuer IS NULL AND verification_subject IS NULL
      AND verification_client IS NULL AND verification_email IS NULL AND verification_revision IS NULL)
    OR (purpose='contact' AND length(verification_issuer)>0 AND length(verification_subject)>0
      AND length(verification_client)>0 AND length(verification_email) BETWEEN 3 AND 254
      AND verification_revision>0 AND verification_issuer IS NOT NULL AND verification_subject IS NOT NULL
      AND verification_client IS NOT NULL AND verification_email IS NOT NULL AND verification_revision IS NOT NULL)
  );

ALTER TABLE portal_contacts
  DROP CONSTRAINT portal_contacts_email_verified_check,
  ADD COLUMN email_verified_at timestamptz,
  ADD CONSTRAINT portal_contact_verification_provenance CHECK (
    (email_verified AND email_verified_at IS NOT NULL)
    OR (NOT email_verified AND email_verified_at IS NULL)
  );
