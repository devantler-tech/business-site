-- Existing receipts retain unknown tax components. The not-yet-validated
-- non-NULL constraint preserves those rows while enforcing explicit components
-- on every new INSERT; no historical amount or acceptance is rewritten.
ALTER TABLE portal_invoices
  ADD COLUMN net_minor bigint,
  ADD COLUMN vat_minor bigint,
  ADD CONSTRAINT portal_invoice_tax_consistency CHECK (
    (net_minor IS NULL AND vat_minor IS NULL) OR
    (net_minor IS NOT NULL AND vat_minor IS NOT NULL AND
      CASE WHEN net_minor BETWEEN 0 AND 1000000000
                 AND vat_minor BETWEEN 0 AND 1000000000
        THEN net_minor + vat_minor = amount_minor
        ELSE false
      END)
  );

ALTER TABLE portal_invoices ADD CONSTRAINT portal_invoice_tax_required
  CHECK (net_minor IS NOT NULL AND vat_minor IS NOT NULL) NOT VALID;
