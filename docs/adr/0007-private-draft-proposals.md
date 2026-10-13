# Private draft project proposals

## Context

The operator can review immutable project requests but cannot prepare proposed
scope and pricing in the portal. A single editable quote would erase the exact
snapshot needed by a later agreement. This decision implements business-site#64;
it does not dispatch an offer or authorize paid work.

## Decision

Reuse the operator request detail for native English/Danish preparation and history.
Append proposal versions under an independent false-default `portal-proposals`
OpenFeature gate, in addition to identity, requests and triage. Derive request,
client, operator and time from server-owned records; client sessions cannot read
or write proposals. Keep the original submission order unchanged.

Serialize writes per request with a transaction advisory lock. Hold current operator
membership through commit; check the gates and expiry again before committing.
An identical request-scoped retry key returns its original snapshot, including after
newer revisions. Changed content or a stale expected version conflicts. Earlier
versions are never overwritten. Append checksummed migration v6 and grant the
runtime SELECT/INSERT only; it must not own or mutate the immutable history.

Bound a request to 50 proposal versions, all shown in its history. Scope and
deliverables each allow 1,200 Unicode code points; one-time and monthly terms each
allow 600. For this first preparation slice, prices are explicit DKK whole kroner,
stored as integer minor units, from zero to 10,000,000 kroner. This is a draft
pricing snapshot, not tax calculation or an invoice. Final tax and contractual
terms require the separately tracked legal review. No floating-point money is used.

## Consequences

The preparation view clearly labels drafts as not accepted or approved for work.
The bounded history is complete, not a silently truncated list. At its limit,
preparation refuses another revision; changing that policy needs a reviewed migration.
No customer data enters static Pages output or public publication receipts.

Client acceptance, offer delivery, invoicing, credited funds, worker eligibility,
API/MCP execution and real deployment remain later work. Source publication of a
latent binary is not activation, federation proof or legal clearance.
