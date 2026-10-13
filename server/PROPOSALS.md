# Private draft proposals

An authorized operator can prepare immutable scope and price versions for an
existing project request. The private request-detail page shows complete history
and a native English/Danish form. A draft is **not accepted or approved for work**;
this slice does not deliver offers to clients.

## Release boundary

The OpenFeature gates `portal-identity`, `portal-requests`, `portal-triage` and
independently false-default `portal-proposals` must be on. Missing, wrong-type
or error evaluations deny preparation. Closing the proposal flag hides history
and its form, without replacing the inbox's own gates. The production no-op
provider activates none of them. Review the temporary proposal flag by
12 November 2026 under [#65](https://github.com/devantler-tech/business-site/issues/65).

## Prepare and compare

In an already configured synthetic fixture, sign in as its operator, open the
inbox and choose a request. Enter scope and deliverables (1,200 Unicode code points
each), whole-DKK one-time and monthly prices, and separate terms (600 code points
each). Each price allows zero to 10,000,000 DKK and persists as integer minor
units, never floating point. Final tax and contractual terms require separate review.

Saving returns to that request with every prior snapshot unchanged, latest first.
Its original submission time and inbox position do not change. The complete
history holds at most 50 versions; preparation closes at that limit. Conflicts
direct the operator to reopen the request and compare versions, never overwrite
them. Storage failure returns unavailable, not an empty successful history.

## Persistence and authority

POST `/portal/operator/requests/{id}/proposals` is the only new mutation. Current
server-owned operator membership authorizes the actual request/client. Browser
roles, owners, email and groups grant no authority. Native CSRF, fixed Host/Origin,
session expiry and no-store apply. Only this form receives a 60,000-byte encoded
body bound; other forms retain their bounds. Text renders inert and escaped.

Transactions serialize per request without UPDATE authority on immutable intake.
Expected version rejects stale writes. A request-scoped opaque retry key binds
normalized content and the original operator: an identical retry returns its
original version after newer revisions or process reconnect. Changed content
conflicts. Membership stays locked through commit; gates and expiry are rechecked.

A separate migration role applies checksummed v6 after unchanged v1–v5. The runtime
requires SELECT/INSERT on `portal_proposals`, but no direct, inherited or assumable
UPDATE/DELETE/TRUNCATE. Fixture-only TRUNCATE is not a production grant. Explicit
query scoping is not a database RLS claim.

## Verification and exclusions

Use the complete [disposable database suite](README.md#actual-database-tests)
and native quality/package checks. Controls cover history/reconnect, concurrency,
retries, client denial, gate and authority closure, storage errors, hostile text,
migrations and restricted startup. The bounded `portalvisual` fixture enables
four gates only for fictional local interaction.

Client acceptance, offer dispatch, tax calculation, invoices, funds, worker claims,
agent execution and API/MCP endpoints are absent. No client-orders repository is
used. Business-site#19/#21 and monorepo#3936/#3939 retain real-data, privacy,
identity and deployment prerequisites. A source merge or container is not portal
activation or legal compliance.
