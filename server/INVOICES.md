# Private external invoice records

The operator can record an invoice issued outside the portal on a request with
current client acceptance. This is private bookkeeping metadata, not invoice
generation, a legal-validity check, payment confirmation or permission to start
work. It changes no agreed price, tax treatment or payment deadline.

## Record and revisit

With all prerequisite portal gates and the independent `portal-invoices` decision
enabled, open an operator request and use **Record external invoice**. Supply the
external reference, invoiced purpose or period, and the invoice's explicit net,
VAT and payable total in DKK. Net plus VAT must equal the positive payable total.
Each component supports up to two decimal places, using a dot or comma without
thousands separators, and is stored exactly in integer minor units. It is never
computed from the proposal's one-time and monthly prices. The maximum is
DKK 10,000,000 per record. Net and VAT may explicitly be zero, but the total
must be positive. Zero VAT records an external fact; it does not verify exemption,
reverse-charge eligibility or a tax rate. History uses a decimal comma in Danish and a decimal
point in English, without thousands grouping.

References use up to 64 ASCII letters, digits, dots, slashes, hyphens or
underscores, beginning with a letter or digit. Surrounding whitespace is removed
and letters normalized to uppercase before checking the 64-character limit. The
native form leaves these checks to the bounded server parser so pasted whitespace
does not reject an otherwise valid reference. A reference can identify only one stored
invoice across the application; allocating one invoice across multiple requests
is not supported. The purpose/period permits up to 300 characters. Do not paste
bank details or supporting documents into this field.

The server derives the client and request, checks current operator membership
and exact current acceptance under the request's shared writer lock, then stores
the acceptance ID, proposal version, share event, content binding, actor and
database time. Each request has a complete newest-first history of at most 50
immutable records. Concurrent stale forms conflict instead of overwriting a row.
An identical retry by the same actor retains its original receipt; altered content
or another actor cannot reuse that key.
The retry binding includes both recorded tax components. An old receipt with no
tax breakdown remains **tax breakdown not recorded**, never estimated or zero.
Only an exact original retry with both component fields absent can return such
a receipt; it cannot create a new invoice or add a breakdown retrospectively.
Blank, partial, malformed or duplicate components are refused. A changed split
conflicts even when its payable total stays the same.

New drafts, sharing changes and withdrawal prevent new records against old
acceptance. Original identical retries can still return their historical record;
they never attach it to replacement scope. History labels the accepted version
as current or historical. These labels do not verify that an invoice is valid or
paid. Separately default-off [bank-credit evidence](CREDITS.md) records manual
observations, partial funds, disputes and append-only corrections against these
invoices. It is not invoice/credit-note generation or payment processing. Do not use these records as
work-queue eligibility, monthly entitlement or worker authority.

## Access, storage and release

Clients and agents have no invoice read or write route. Operator writes retain
the native POST, CSRF/origin, single-value field and body-size checks. Reads and
writes recheck their independent SDK gates, expiry and cancellation; membership
stays locked through the transaction. A failed database read returns unavailable,
not an empty history. Private responses are non-cacheable and HTML-escaped.

Migration v10 appends the invoice table and its exact acceptance foreign-key
binding after unchanged v1–v9. Migration v12 appends nullable historical net/VAT
columns after unchanged v1–v11 without rewriting existing receipts. Its exact-sum
constraint retains the amount bounds; a separate non-NULL constraint is explicitly
`NOT VALID` for historical rows but enforced on every new SQL insert. This is
compatibility for unknown old facts, not permission for new incomplete records.
The existing amount remains the payable total used by bank-credit reconciliation.
The runtime needs SELECT/INSERT only; table- and
column-level mutation, triggers, references, maintenance, grant options, ownership
and assumable broader privileges are refused for every immutable request, proposal,
sharing, acceptance and invoice table. A migration rollback must
not erase the ledger. See [the migration procedure](MIGRATIONS.md).

The release decision defaults false and is evaluated on every operation. Missing,
false, wrong-type or erroneous evaluation denies; no environment activation
mapping exists. Review its retirement in [#117](https://github.com/devantler-tech/business-site/issues/117)
by 15 November 2026. Merged source and synthetic tests do not activate collection
of real invoice or customer data. Deployment, identity, retention/correction and
legal/consumer prerequisites remain separate release conditions. There is no
payment collection or agent execution endpoint; bank-credit evidence has its own
independent release gate and operator-only routes.
