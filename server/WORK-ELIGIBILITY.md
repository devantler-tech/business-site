# Operator work eligibility

The independently gated reader lets a current human operator inspect the service
prerequisites of portal requests. `GET /portal/operator/api/work-eligibility`
records `work.eligibility.list` through the existing operator authorization and
audit path. It creates no approval, claim, worker session or execution grant.
Administrative machine identities cannot call it or gain it through configuration.

## Current evidence

The in-transaction `inspectWorkEligibility` policy reuses
`inspectWorkCurrent` and its exact [work-approval evidence](WORK-APPROVALS.md).
It does not reproduce funding decisions in a route, query label or adapter.
An eligible work observation requires the current accepted scope, its selected
operator approval, current bound membership incarnations, the exact order and
settled funding, no reversal or withdrawal, permitted early start and the
exclusive database approval deadline. Funding follows ADR 0030 and
[portal billing](BILLING.md).

The exact original confirmation must also have the billing producer's complete,
immutable format-1 agreement proof and a receipt from the mail sender. The shared
`readOriginalConfirmation` lookup binds request, client, acceptance, order,
proposal version and content hash, and verifies the frozen body digest. Missing
proof, a legacy link-only confirmation, preparation alone, a different message
kind and a failed send all remain ineligible. A sender receipt means the sender
accepted the message for delivery; it does not prove inbox delivery.

An approval may exist before a confirmation is sent. Reading eligibility does
not rewrite that approval or erase its history. Closed requests and completed
orders cannot become ready to start through a historical approval.

## Response and ordering

The response contains at most twenty `items`, ordered by original server
submission time and then request ID by byte order. Each contains `requestId`, the
original `submittedAt`, `observedAt`, `approvalGeneration`, `state`, and an
`approvalId` when there is a selected receipt. A `nextCursor` continues strictly
after the last returned original position. The cursor conveys no authority.
The endpoint accepts only one canonical `cursor` query parameter.

States are `approval-required`, `approval-paused`, `confirmation-pending`,
`closed` and `eligible`. An eligible observation describes satisfied service
prerequisites at its recorded instant. Every future operation must revalidate
its own authority and evidence; an observation grants no execution lease.
The response contains no brief, customer contact, agreement body, mail content,
payment reference or private membership evidence.

Each row retains its request, funding and membership locks through its final
SDK evaluation and the original durable operator-session check. Separate rows
have separate observation instants. This keeps the read bounded without taking
cross-request membership-lock sets or asserting an atomic queue snapshot.
The complete JSON frame is buffered and the shared operator dispatcher checks
the original durable session again after all waits and audit recording.
The held transaction handles its own session check, including with a pool of
one connection. Every actor and viewer check binds the original server-issued
membership incarnation. Revocation and reactivation cannot restore an old
principal or session; a genuinely new sign-in must bind the current incarnation.

A genuine empty page is `{"items":[]}`. Storage or scan uncertainty fails the
whole page with a generic unavailable response; no partial observations escape.
Malformed input returns 400, signed-in clients and administrative agents are
refused, and a call that cannot be audited releases no observations.

## Gates and delivery boundaries

`portal-work-eligibility` is a default-false OpenFeature access and
operations switch, separate from the existing identity, request, triage,
proposal, sharing, acceptance and work-approval gates. It has no environment
activation mapping. The long-lived switch can close this private inventory
independently; its continued purpose will be reviewed with the worker API in
[#663](https://github.com/devantler-tech/business-site/issues/663).
False, missing, wrong-type, error and stale provider states fail closed.

This reader depends on the certified confirmation producer and schema in
[#667](https://github.com/devantler-tech/business-site/issues/667) and the shared
operator API in [#665](https://github.com/devantler-tech/business-site/pull/665).
It adds no migration of its own. Included-maintenance authorization remains
[#259](https://github.com/devantler-tech/business-site/issues/259); covered work
must not acquire an additional feature invoice through this policy.
Appointments consume `collectWorkEvidence` and `readOriginalConfirmation` in
their own authorized booking transaction, without selecting `work_current` or
acquiring a software lease. They remain outside software delivery claims.
A later software queue needs
an explicit server-owned delivery kind bound to the approved agreement;
`other` and free-text briefs cannot establish that classification.

Fictional disposable PostgreSQL and TLS/browser tests prove ordering, bounded
continuations, failure behavior, SDK and deadline checks, private response
contents and original durable sessions. Source tests, CI, review and merge are
separate from signed publication, deployment, real sign-in and live acceptance.
