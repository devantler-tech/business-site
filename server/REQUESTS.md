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

The unique `(client_id, submission_key)` pair is the retry boundary. Identical
normalized content returns the original ID and database-assigned timestamp, even
after a process reconnect or concurrent submissions. Different content with the
same key conflicts without overwriting the original. Correcting an invalid form
retains safe bounded fields; a conflicting submitted form requires a new form.

A request is only a brief. It provides **no paid eligibility, queue claim,
agreement, payment confirmation, automated execution or completion verdict**.
Operator quoting and the order lifecycle remain monorepo#3937; versioned worker
API and a thin MCP adapter remain business-site#20. UI and future adapters must
share this same server-owned authority rather than trust GitHub issue state.

## Migration and verification

Before constructing the configured runtime, the separate migration role applies
the checksummed v4 migration. Existing v1–v3 bytes and ledger history are preserved.
Give the restricted runtime SELECT and INSERT on `portal_requests`; it needs no
UPDATE, DELETE or schema ownership for immutable receipts. Startup rejects UPDATE,
DELETE and TRUNCATE held directly, inherited or through an assumable role. Do not grant TRUNCATE
outside the disposable test fixture. This is explicit application/query scoping,
not database RLS or a production privacy certification.

Use Node 24 alongside the exact Go toolchain. Run the [complete disposable database suite](README.md#actual-database-tests),
not only the new tests. It exercises actual TLS handlers and PostgreSQL: durable
receipts, retries/conflicts, concurrent duplicates, two-client isolation,
revocation, operator denial, escaped text, native CSRF/origin rejection, malformed
and oversized forms, Danish character limits, safe draft recovery, flag closure,
migration checksums and least-privilege startup. The suite invokes six controls
against the actual served browser script with Node 24, not a Go translation.
All accounts/briefs are fictional.
The `portalvisual` fixture additionally turns on this gate for synthetic browser
checks; it is never real broker, customer, payment or production ingress proof.
