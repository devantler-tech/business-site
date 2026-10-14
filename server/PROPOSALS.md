# Private draft proposals

An authorized operator can prepare immutable scope and price versions for an
existing project request. The private request-detail page shows complete history
and a native English/Danish form. A draft is **not accepted or approved for work**;
unshared versions remain private to the operator.

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

POST `/portal/operator/requests/{id}/proposals` prepares a draft. Current
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

Client acceptance, tax calculation, invoices, funds, worker claims,
agent execution and API/MCP endpoints are absent. No client-orders repository is
used. Business-site#19/#21 and monorepo#3936/#3939 retain real-data, privacy,
identity and deployment prerequisites. A source merge or container is not portal
activation or legal compliance.

## Explicit client review and withdrawal

The independently false-default `portal-proposal-sharing` SDK decision additionally
requires all four gates above. Review its lifecycle by 13 November 2026 under
[#78](https://github.com/devantler-tech/business-site/issues/78). Ordinary production
composition still cannot enable requests, proposals or sharing.

With sharing enabled in a fictional fixture, the operator chooses **Share version
N for review**. The intended current client sees that exact immutable version on
their existing request receipt, in English or Danish. Saving another draft does
not release it. Sharing another version replaces the visible selection. Withdrawal
hides it from future reads without falling back to a previously shared version;
it cannot retract copies already seen or saved. A withdrawn version cannot be
shared again: prepare a new version if the proposal should be offered for review again.
Acceptance and ordering are not available in this interface.

POST `/portal/operator/requests/{id}/proposal-visibility` accepts only a share or
withdraw decision, an exact version, the expected latest draft/event and an opaque
retry key. It retains native CSRF, fixed Host/Origin, session expiry and no-store,
with the ordinary 8,192-byte form bound. The append-only v7 decision ledger follows
unchanged v1–v6 migrations. The runtime requires SELECT/INSERT only on
`portal_proposal_visibility`, with direct and assumable mutation grants refused.

Decisions and draft revisions use the same per-request transaction lock. Current
operator membership stays locked through commit; SDK gates and expiry are checked
again. Retries bind the original actor and decision and never reapply a withdrawn
selection. At most one share and one withdrawal per version bound the complete
history at 100 events, preserving room to withdraw the final shared version.
Client reads bind the server-owned request/client and recheck current membership,
client binding, expiry and gates after the query. Independent gate evaluations
precede the final membership read; SQL completion also checks expiry and
cancellation before releasing data. Operator visibility history retains current
operator authority without requiring a fixed operator client workspace. Authorized
clients with no shared version still receive an empty result. These are bounded
authorization checkpoints, not atomic wire-time revocation. Missing storage is unavailable,
never an empty successful result. Neither retry keys, operator attribution nor
unshared history is returned to clients or allowed in public build artifacts.

The synthetic `portalvisual` gateway enables all five gates for manual review;
its fixed `/fixture/client` and `/fixture/operator` actor selectors are test-only,
not authentication. This adds no real customer activation, acceptance, payment,
agent execution, API/MCP endpoint or client-orders repository.
