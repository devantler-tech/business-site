# Private project requests

An invited client can send a free website, web-app or service brief, revisit its
receipt and see up to 50 recent requests. Older receipt links remain usable.
This is source functionality behind **two closed release gates**, not live
customer intake. The public website and static Pages output contain no briefs.

`portal-identity` and `portal-requests` are OpenFeature booleans, evaluated with
false defaults on every request operation. Missing flags, wrong types and errors
deny access. The production executable's no-op provider enables neither; there
is no environment-variable activation path. Real-data collection remains held by
business-site#19/#21 and monorepo#3936/#3939. The request release switch is reviewed
by 12 November 2026 under [#57](https://github.com/devantler-tech/business-site/issues/57).

## Client path and authority

The workspace links to `/portal/requests` only for a client while the request gate
is on. `/portal/requests/new` renders an EN/DA native form with a fresh opaque
submission key. POST `/portal/requests` accepts one product, a title of up to 120
Unicode code points and a brief of up to 1,200, before trimming outer whitespace.
Native textarea line endings are canonicalized to LF before counting and retry
comparison, so form-encoded CRLF does not consume an extra character.
The browser's native constraint message uses the same count; its UTF-16 hard
bound leaves room for supplementary characters rather than cutting emoji in half.
Server validation remains mandatory without JavaScript. The intake-only encoded body bound
is 20,000 bytes; other private forms retain their existing 8,192-byte bound.
No browser field can choose an owner, role, timestamp, agreement or payment state.

Ownership comes from current issuer/subject membership. A membership row lock
keeps revocation and a submission commit ordered. Each data query scopes the
actual record to that client's current membership; operator membership is not a
client impersonation permission. The receipt route uses an opaque server-created
ID, escapes client text and carries `no-store`. IDs are not access credentials.

List and receipt reads evaluate independent release gates before a final current
membership check. Revocation, a changed role or a different client binding denies
the result with no private rows. Membership SQL completion also checks identity
expiry and cancellation; a successful query alone does not authorize release.
This is a final authorization checkpoint, not an atomic guarantee against a
change after that checkpoint or a way to retract content already received.

The unique `(client_id, submission_key)` pair is the retry boundary. Identical
normalized content returns the original ID and database-assigned timestamp, even
after a process reconnect or concurrent submissions. Different content with the
same key conflicts without overwriting the original. Correcting an invalid form
retains safe bounded fields; a conflicting submitted form requires a new form.

A request is only a brief. It provides **no paid eligibility, queue claim,
agreement, payment confirmation, automated execution or completion verdict**.
Operator-only [draft preparation](PROPOSALS.md) does not deliver or accept an offer.
Client agreement and the order lifecycle remain monorepo#3937; versioned worker
API and a thin MCP adapter remain business-site#20. UI and future adapters must
share this same server-owned authority rather than trust GitHub issue state.

## Operator review

The operator workspace links to `/portal/operator/requests` only when all three
false-default OpenFeature gates are enabled: `portal-identity`, `portal-requests`
and `portal-triage`. Every list and detail operation rechecks current operator
membership, including in its data query and after reading rows. Client sessions,
revoked or downgraded memberships and expired sessions cannot read the inbox.

The list shows twenty titles, offering types and original UTC submission times
per page, oldest first. Timestamp/opaque-ID keyset pagination uses microsecond
precision and fixed `C` text collation. Switching EN/DA retains the page position.
This is a live list, not a frozen snapshot; newer requests can appear on later
pages without shifting records already read. Detail pages display escaped inert
briefs, never contact profiles or internal client identifiers. A missing record
returns 404 only to an authorized operator; other sessions receive the same
denial regardless of whether a record exists. Storage failure returns a sanitized
503, not an empty successful list. Every response is `no-store`.

Reading a request does not approve work. Draft preparation is independently gated;
no client acceptance, order, payment, status change or agent execution endpoint exists.
The triage gate remains off in the production
no-op provider; there is no environment activation path. Its retirement review is
tracked in [#62](https://github.com/devantler-tech/business-site/issues/62) for
12 November 2026, and real intake retains the parent launch prerequisites.
See [the inbox decision](../docs/adr/0006-private-operator-request-inbox.md).

## Migration and verification

Before constructing the configured runtime, the separate migration role applies
the checksummed migrations through v6. The triage migration adds a global
timestamp/ID index only; existing v1–v4 bytes, records and ledger history are preserved.
Give the restricted runtime SELECT and INSERT on `portal_requests`; it needs no
UPDATE, DELETE or schema ownership for immutable receipts. Startup rejects UPDATE,
DELETE and TRUNCATE held directly, inherited or through an assumable role. Do not grant TRUNCATE
outside the disposable test fixture. This is explicit application/query scoping,
not database RLS or a production privacy certification.

Use the exact Go toolchain for the [complete disposable database suite](README.md#actual-database-tests),
not only the new tests. It exercises actual TLS handlers and PostgreSQL: durable
receipts, retries/conflicts, concurrent duplicates, two-client isolation,
revocation, operator denial, escaped text, native CSRF/origin rejection, malformed
and oversized forms, Danish character limits, safe draft recovery, flag closure,
migration checksums and least-privilege startup. Separately, run
`node --test scripts/portal-request-limits.test.mjs` from the repository root with
Node 24. Required source CI runs these six actual served browser-script controls
unconditionally; the Go database suite does not depend on Node.
Operator controls additionally exercise current role/expiry/revocation, all three
independent gates, two-client aggregate review, inert detail, reconnect durability,
bounded tied pagination, malformed cursors and actual denied database reads.
All accounts/briefs are fictional.
The `portalvisual` fixture additionally turns on all three gates for synthetic browser
checks; it is never real broker, customer, payment or production ingress proof.
