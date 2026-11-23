-- New complete-agreement confirmations carry producer evidence atomically with
-- their frozen body. Legacy mail remains unproven: never backfill or rewrite it.
ALTER TABLE portal_orders ADD CONSTRAINT portal_order_confirmation_binding UNIQUE(id,acceptance_id);
ALTER TABLE portal_proposal_acceptances ADD CONSTRAINT portal_acceptance_confirmation_binding UNIQUE(id,proposal_version,content_hash);
ALTER TABLE portal_mail_outbox
  ADD COLUMN producer_format integer CHECK(producer_format=1),
  ADD COLUMN acceptance_id portal_consent_key,
  ADD COLUMN proposal_version integer CHECK(proposal_version BETWEEN 1 AND 50),
  ADD COLUMN proposal_sha256 portal_consent_hash,
  ADD COLUMN terms_sha256 portal_consent_hash,
  ADD COLUMN body_sha256 portal_consent_hash,
  ADD CONSTRAINT portal_mail_complete_producer CHECK(
    num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=0 OR
    (num_nonnulls(producer_format,acceptance_id,proposal_version,proposal_sha256,terms_sha256,body_sha256)=6 AND
      producer_format=1 AND kind='order-confirmation')),
  ADD CONSTRAINT portal_mail_body_digest CHECK(body_sha256=encode(sha256(convert_to(body,'UTF8')),'hex')),
  ADD CONSTRAINT portal_mail_original_order FOREIGN KEY(order_id,acceptance_id) REFERENCES portal_orders(id,acceptance_id),
  ADD CONSTRAINT portal_mail_original_acceptance FOREIGN KEY(acceptance_id,proposal_version,proposal_sha256)
    REFERENCES portal_proposal_acceptances(id,proposal_version,content_hash);
