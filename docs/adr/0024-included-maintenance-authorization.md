# Included hosting maintenance authorization

Status: proposed, 2026-10-25, for #259 under #20.

## Context

Hosted client agreements include optimization, accessibility improvements, bug
fixes and routine upkeep in the agreed monthly price. New features and larger
iterations need a new agreement. The paid-scope approval in
[`server/WORK-APPROVALS.md`](../../server/WORK-APPROVALS.md) binds one accepted
proposal version, one fully credited external invoice and a 168-hour decision.
A proposal's monthly price and terms are recorded facts, not a service
entitlement, so neither existing record can say that a particular project is
covered today.

## Decision

Keep three separate records under the Go/PostgreSQL authority:

1. **Maintenance entitlement.** A versioned record per client and maintained
   project, naming the exact accepted proposal version it derives from, the
   agreed maintenance scope and an explicit service period. Period bounds are
   PostgreSQL `timestamptz` values compared with the transaction's database time
   at microsecond precision: the start is included and the end is excluded. Only
   an authorized operator records or corrects an entitlement, from that accepted
   agreement; it never manufactures client consent, and a monthly price alone
   never creates one. Each change appends a new immutable version.
2. **Included-task authorization.** An operator's explicit decision that one
   described task is included maintenance under exactly one entitlement version.
   The decision is the operator's alone and creates no new client acceptance. It
   binds client, project and entitlement version, the membership incarnation of
   the client acceptor already recorded for that accepted proposal version, and
   the approving operator's incarnation
   ([ADR 0018](0018-durable-membership-incarnations.md)). Decisions are immutable,
   with actor- and content-bound retries; revocation appends terminal evidence.
3. **Queue eligibility and worker claims** stay in #20's single portal-owned
   boundary, which also owns request order, service classes and never preempting
   active work. Neither record above starts an agent, publishes work, or changes
   portfolio review, merge, spend or repository-access controls.

### Entitlement states

Every new version is one of two kinds. A **correction** (including a scope or
project change) permanently supersedes the version it corrects: a superseded
version never governs again, even after the correction's own period ends. A
**renewal** adds a later service period that starts no earlier than the end of
the period it renews. The **governing version** at database time is the one
non-superseded version whose service period contains that time; the no-overlap
rule for renewals makes it unique, and a renewal recorded ahead of its start
governs only from that start.

One domain reader returns exactly one state, judged at database time, by taking
the first rule in this order that matches:

| Order | State | Matches when | New included-task authorization |
|---|---|---|---|
| 1 | unavailable | facts are missing, unreadable, overflowed or inconsistent | paused |
| 2 | cancelled | the governing version records a cancellation | paused |
| 3 | disputed | the governing version records a dispute | paused |
| 4 | active | a governing version exists | permitted while that version's authorization history is below its bound |
| 5 | future | no version governs and a recorded period starts later, which covers both the time before the first period and a gap between two periods | paused |
| 6 | ended | no version governs and no recorded period starts later | paused |

Rules 4 to 6 between them cover every readable entitlement, so no reading is
left unclassified.

Paused stops automated eligibility only. It never terminates a contract, adds a
payment or grace period, denies a contractual remedy or creates a refund rule;
those stay agreement terms handled outside this code. An unpaid monthly fee
changes no state by itself: the operator records a dispute or cancellation when
the agreement says so.

### Classification

- **Included:** optimization, accessibility improvements, bug fixes and routine
  upkeep of the maintained project, within the entitlement's agreed scope. No new
  feature purchase, invoice or credited funds are required.
- **Not included:** new features and larger iterations keep the paid-scope path.
- **Ambiguous:** the operator classifies the task explicitly.

A client's label, a browser field, an agent assertion, an invoice total or a
payment-provider event can neither classify a task nor change an entitlement.

### Validity and invalidation

A task authorization is valid only while its entitlement version is the
governing version, that version is active and its bound memberships are still
the current incarnations. A correction supersedes the governing version at once,
so authorizations bound to the corrected version end immediately. A renewal
governs only from its start, so recording it early neither ends current
authorizations nor pauses the current period. An authorization therefore ends at the earliest of
revocation, a newer governing version, the end of its version's service period
or a membership transition. It has no elapsed lifetime of its own and reuses neither
the paid-scope approval's 168-hour window nor its invoice binding. Restoring
earlier values, labels, totals or roles never revives an ended authorization; a
new explicit operator decision is required.

A task reader distinguishes valid, paused, absent and unavailable authority.
UI, API and MCP adapters call that reader and never copy or cache its policy.
Writers lock the entitlement first, then memberships in deterministic identity
order, and keep both through commit, with the final SDK, membership, expiry and
cancellation checks retained.

### Revocation and bounds

Revocation needs only current operator access and its own release gate. It never
needs an active entitlement, valid funding or the original approver's
membership. Positive history is bounded per entitlement version. The bound is
applied only when admitting a new decision: reaching it closes new
authorizations, but it never changes the entitlement's state, never ends an
authorization already recorded and never blocks revocation or correction. A
failed or overflowed history read is unavailable, never an empty success.

### Emergency and security work

Only security fixes and incident recovery that must happen regardless of a
client's entitlement use their own separately authorized route. A task on that
route is never also recorded as included maintenance, and it is not a general
maintenance exemption: every other bug fix follows the included-maintenance
rules above.

### Release

A distinct false-default release decision has no environment activation mapping;
missing, false, wrong-type, failed and stale evaluations deny. Migrations are
additive and owned by the separate migrator, runtime grants stay least-privilege
and the runtime runs no DDL. Client isolation is enforced by the authority's own
client-scoped queries, never assumed from database row-level security. Private
pages use the retained Go frame and native forms ([ADR 0017](0017-retain-go-private-presentation.md)).

## Alternatives

- A feature purchase for every covered fix contradicts the agreed offer.
- Authorizing any task labelled maintenance gives clients or agents an
  uncontrolled execution path.
- A paid-scope approval with a zero invoice misstates credited-funds evidence and
  inherits that record's invoice binding and lifetime.
- GitHub issues or labels as a maintenance queue duplicate the portal's authority.

## Verification

Use disposable restricted-role PostgreSQL and fictional data. Cover a covered
fix authorized without a new invoice; future, active and end-boundary cases; a
gap between two service periods; a renewal recorded ahead of its start; a
correction that shortens a period without re-exposing the version it superseded;
renewal, correction, revocation and restoration; changed project, agreement or
scope; cross-client access and stale memberships; disputed and unavailable
facts; bounded and failed history reads; a reached history bound that closes new
decisions but leaves earlier authorizations valid; concurrent authorizations;
late SDK and cancellation checks; and English/Danish operator and client
journeys. Keep every existing gate. A source merge activates nothing: hosting,
identity, privacy, legal/consumer and worker activation remain separate
decisions.
