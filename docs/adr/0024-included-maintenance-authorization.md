# Included hosting maintenance authorization

Status: accepted, 2026-10-25, for #259 under #20.

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

1. **Maintenance entitlement and service periods.** One stable entitlement
   identity per client and maintained project, with a unique client/project
   pair. Each service period has its own stable identity, immutable revision
   history and exactly one current revision. That revision names the exact
   accepted proposal version, agreed maintenance scope, service-period bounds
   and operator-recorded cancellation/dispute facts. Bounds are PostgreSQL
   `timestamptz` values at microsecond precision: the start is included and the
   end is excluded. Only an authorized operator records facts derived from an
   accepted agreement; this never manufactures client consent, and a monthly
   price alone never creates coverage. Reassigning the entitlement's client or
   project is forbidden; another project needs its own entitlement and agreement.
2. **Included-task authorization.** An operator's explicit decision that one
   described task is included maintenance under exactly one service-period revision.
   The decision is the operator's alone and creates no new client acceptance. It
   binds client, project, entitlement, service period and current revision, the
   membership incarnation of the client acceptor already recorded for that
   accepted proposal version, and the approving operator's incarnation
   ([ADR 0018](0018-durable-membership-incarnations.md)). Decisions are immutable,
   with actor- and content-bound retries; revocation appends terminal evidence.
3. **Queue eligibility and worker claims** stay in #20's single portal-owned
   boundary, which also owns request order, service classes and never preempting
   active work. Neither record above starts an agent, publishes work, or changes
   portfolio review, merge, spend or repository-access controls.

### Entitlement states

Choose the per-service-period model from #352. A renewal creates a **new period**;
a correction appends a revision to **one existing period** and atomically replaces
only that period's current revision. State changes, including cancellation or
dispute, also append a revision. Superseded revisions remain audit evidence only:
they are never candidates for either current or future coverage.

New periods may be recorded out of chronological order. A renewal identifies the
stable entitlement and its fixed client/project, and supplies a fresh period
identity with agreement-derived bounds. It does not identify or replace a
predecessor period. Reusing a period identity cannot change its current head:
that requires an explicit correction. Coverage follows service-time intervals,
never insertion order.

Every period has a nonempty, finite half-open interval `[start,end)`. Current
revisions of different periods under the same entitlement must never overlap,
including cancelled or disputed periods. Every writer locks the stable entitlement
before reading or changing period heads, checks the complete prospective set
for overlap and commits the immutable revision and current-head change together.
The same rule covers corrections, renewals and concurrent writes; it is not a
renewal-only check. A correction overlapping another current period is refused
without changing either head. Neighboring periods are never shifted automatically.
Any future multi-period amendment must satisfy this same invariant atomically.

The **governing revision** is the current revision of the period containing fresh
database time. The uniqueness of the entitlement and current period heads, plus
non-overlap, prove that there is at most one. Historical revisions do not enter
this query. A separate existence query over current heads determines whether a
later period remains; neither query loads authorization history or selects the
newest revision across unrelated periods. This avoids a history-read bound or an
early renewal changing the coverage in force.

For example, recording February's renewal while January is active does not
replace January's head. Shortening January's current revision to 15 January
creates a gap until February; it cannot expose January's superseded longer
revision. Moving that correction's start into the future pauses coverage at
once. Extending January across February is rejected until a separately authorized
amendment can produce a non-overlapping set.

One domain reader returns exactly one state, judged at database time, by taking
the first rule in this order that matches:

| Order | State | Matches when | New included-task authorization |
|---|---|---|---|
| 1 | unavailable | facts are missing, unreadable, overflowed or inconsistent | paused |
| 2 | cancelled | the governing revision records a cancellation | paused |
| 3 | disputed | the governing revision records a dispute | paused |
| 4 | active | a governing revision exists | permitted while that revision's authorization history is below its bound |
| 5 | future | no revision governs and a current period revision starts later, including before the first period or in a gap | paused |
| 6 | ended | no revision governs and no current period revision starts later | paused |

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

A task authorization is valid only while its bound period revision remains
that period's current head, is the governing revision, is active and its bound
memberships are still the current incarnations. Replacing that period's head
ends its old authorizations immediately, even if the new revision starts later.
A renewal changes a different period, so recording it early neither ends current
authorizations nor pauses the current period. An authorization therefore ends at
the earliest of revocation, replacement of its period's head, the end of its
revision's service period or a membership transition. It has no elapsed lifetime
of its own and reuses neither the paid-scope approval's 168-hour window nor its
invoice binding. Restoring
earlier values, labels, totals or roles never revives an ended authorization; a
new explicit operator decision is required.

A task reader distinguishes valid, paused, absent and unavailable authority.
UI, API and MCP adapters call that reader and never copy or cache its policy.
Writers lock the entitlement first, then memberships in deterministic identity
order, and keep both through commit, with the final SDK, membership, expiry and
cancellation checks retained. The final protected SQL statement uses fresh
database wall-clock time after the last SDK wait. A timestamp fixed at transaction
start cannot establish that coverage still exists after that wait.

### Revocation and bounds

Revocation needs only current operator access and its own release gate. It never
needs an active entitlement, valid funding or the original approver's
membership. Positive history is bounded per period revision. The bound is
applied only when admitting a new decision: reaching it closes new
authorizations, but it never changes the entitlement's state, never ends an
authorization already recorded and never blocks revocation or correction. A
failed or overflowed new-decision history read is unavailable, never an empty
success. Existing-task validity does not depend on reading that positive history.

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
- One linear version chain for the whole entitlement conflates renewal with
  correction. It needs extra rules to preserve current coverage when a future
  renewal is recorded and to prevent old intervals from governing again.

## Verification

Use disposable restricted-role PostgreSQL and fictional data. Cover a covered
fix authorized without a new invoice; exact start/end boundaries at microsecond
precision; a gap between two periods; an early renewal preserving the current
head and its valid tasks; shortened and later-start corrections that never
re-expose superseded coverage; a superseded future revision ignored by the
`future` query; overlapping correction/renewal refused with both heads unchanged;
out-of-order non-overlapping periods, including an earlier period recorded later;
changed renewal content under an existing period identity refused; and concurrent
corrections/renewals preserving the non-overlap invariant.

Also cover renewal, correction, revocation and restoration; forbidden client or
project reassignment; changed agreement or scope; cross-client access and stale
memberships; disputed and unavailable facts; bounded and failed history reads;
a reached history bound that closes new
decisions but leaves earlier authorizations valid; concurrent authorizations;
late SDK and cancellation checks; and English/Danish operator and client
journeys. Keep every existing gate. A source merge activates nothing: hosting,
identity, privacy, legal/consumer and worker activation remain separate
decisions.
