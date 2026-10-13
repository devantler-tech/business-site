# Private operator request inbox

Date: 2026-10-12

## Decision

The operator reviews immutable project requests inside the existing portal. A
read-only list and detail view require current server-owned operator membership
in the actual database query. Identity, requests and triage each use independent
false-default OpenFeature gates; no gate grants quoting or execution authority.

The list uses twenty records per page, ordered by original submission timestamp
and opaque request ID. A versioned timestamp/ID keyset cursor retains PostgreSQL
microsecond precision and a fixed text collation. Language changes preserve the
cursor. Newer insertions do not shift previously read pages. This is a live
inbox, not a snapshot: new submissions may appear on later pages.

## Boundaries

Client receipt authorization remains separate. The operator inbox does not join
contact profiles or expose internal customer identifiers. Briefs render only as
escaped inert text on the detail page; list pages show title, offering and original
submission time. Responses are private and non-cacheable. Storage errors produce
a sanitized unavailable response, never an apparently empty successful inbox.

The request table stays immutable to the runtime. No approval/status field,
quote, agreement, funds, agent action, GitHub mirror or real provider activation
is introduced. Static Pages does not deploy this backend. Production activation
remains subject to the parent identity, runtime and privacy prerequisites.

## Alternatives

A GitHub issue mirror duplicates the private state and does not provide portal
authorization. Direct database administration is not an operator product flow.
Offset pagination shifts tied records when pages change; a timestamp-only cursor
loses records sharing a submission time.
