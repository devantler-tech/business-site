-- Danish-law check (#21): a client ends their own monthly billing in the portal,
-- where they ordered (Consumer Contracts Act, section 28 a). The end is the
-- same once-only last billed month, recorded with the client who chose it.
ALTER TABLE portal_order_ends
  ADD COLUMN client_issuer text,
  ADD COLUMN client_subject text,
  DROP CONSTRAINT portal_order_ends_source_check,
  ADD CONSTRAINT portal_order_end_source CHECK(source IN ('operator','stripe','client')),
  ADD CONSTRAINT portal_order_end_client CHECK((source = 'client') = (client_issuer IS NOT NULL AND client_subject IS NOT NULL));
