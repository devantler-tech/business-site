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

Tax calculation, invoices, funds, worker claims,
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
Exact acceptance is separately gated as described below; ordering is unavailable.

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
not authentication. This adds no real customer activation, payment,
agent execution, API/MCP endpoint or client-orders repository.

## Exact client review and acceptance

The independently false-default `portal-proposal-acceptance` decision requires all
five proposal-sharing gates. Review its release lifecycle under [#106](https://github.com/devantler-tech/business-site/issues/106)
by 14 November 2026. Ordinary production composition cannot enable it. Source
consent is not a legal-launch or paid-work permission.

On their own private receipt, a currently authorized client chooses **Review before
accepting**, reads the exact shared scope, deliverables, integer DKK prices and
separate terms, then explicitly confirms that version. English/Danish native
forms, CSRF, fixed Host/Origin, session expiry and no-store remain mandatory.
No new contractual, consumer, tax or cancellation promises are invented.

POST `/portal/requests/{id}/proposal-review` creates or reuses one short-lived
server context for that request/client/issuer/subject. It copies the current draft,
share event and a canonical typed-content SHA256 binding, and uses the existing
opaque random-token primitive. Matching reuse never extends its original deadline;
rotation after expiry or a state change invalidates the old context. There is no
custom signed browser payload or session write on GET that could revive logged-out
authority. The context alone never grants access or proves a legal signature.

POST `/portal/requests/{id}/proposal-acceptance` accepts only the context key,
explicit confirmation, language and CSRF; unknown or duplicate fields fail. It
shares the operator writers' per-request advisory lock but independently locks
current client membership and scopes the actual request. Current draft must equal
the latest explicit share, with exact event/version/content and unexpired context.
A newer unshared draft rejects acceptance neutrally without leaking its version
or content; withdrawal also denies. Gates, principal/context expiry and cancellation
are checked after waits and before commit. Display SDK waits precede final current
membership SQL, rather than releasing a previously read client's data afterward.
These checkpoints are not atomic wire-time revocation or cancellation of an already
running request by logout.

Checksummed v9 preserves unchanged v1–v8. `portal_proposal_reviews` is mutable,
bounded ephemeral presentation state with SELECT/INSERT/UPDATE, never DELETE or
TRUNCATE runtime access. `portal_proposal_acceptances` is an append-only audited
ledger with SELECT/INSERT only, including inherited/assumable mutation refusal.
Receipts copy bindings and refer only to immutable requests/proposals, not mutable
contexts. One request/client/version can have one acceptance. An original actor/key
retry returns its original receipt after current access checks, including reconnect,
but cannot append or accept a replacement proposal. Historical receipt text and
prices remain available when the proposal is replaced or withdrawn; that historical
decision is not current-scope authorization.

Acceptance does not approve a payment, invoice, credited funds, order or worker
execution. The selected portal-owned queue/API/MCP remains separate under #20.
Privacy/retention and real consumer/legal review remain launch conditions under
#19/#21 and monorepo#3936/#3939. Included hosting maintenance cannot be forced into
a new feature purchase. Synthetic database/browser proof is not customer activation.
