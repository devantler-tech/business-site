# Private work decisions

This internal service records an operator's explicit decision against an exact
current agreement and fully credited external invoice. Its default-off operator
screen uses the retained Go frame and native forms; it has no client decision
form, worker, queue, API or MCP adapter. It does not activate the portal,
start work, collect money or create a monthly entitlement. Operator review is
tracked in [#267](https://github.com/devantler-tech/business-site/issues/267)
under [#138](https://github.com/devantler-tech/business-site/issues/138);
the portal-owned work queue remains under #20, not a client-orders repository.

## Exact evidence and lifetime

A prepared review is only a content hash, the selected acceptance/invoice and a
current-generation comparison token. It is not a persisted approval and grants
no authority. A fresh decision independently reconstructs the same evidence
inside the request transaction:

- Current shared proposal version, visibility event, content and exact client
  acceptance, without manufacturing new client consent.
- Selected invoice's explicit net, VAT and payable amount, plus the complete
  bounded invoice history. Legacy unknown components cannot authorize work.
- Complete bounded bank-transfer reservation and revision history, and every
  latest observation. Only an exact confirmed total with no disputed or
  unverified observation is eligible. Missing observations, revision gaps,
  overflow or failed reads are unavailable, never a zero or success.
- Exact client acceptor and original operator identities, workspaces and
  database-owned membership incarnations. The current viewing operator is
  independently authorized; a different authorized operator may review a
  decision without changing its original actor.

The closed typed encoding normalizes timestamps to UTC and sorts transfer
identities before hashing. It contains private evidence and must not be logged
or published. Changes to agreement, invoice history, credit observations or the
bound memberships invalidate the existing approval even when totals or current
membership values later return to their earlier values.

The database assigns the decision time after locking and feature evaluation.
Expiry is exactly 168 elapsed hours later, not seven local calendar dates.
Validity ends at the deadline: it requires database time strictly before expiry,
including after the final SDK wait. Principal expiry, cancellation or closed
release decisions prevent commit or release of a valid result.

Membership restoration never revives a prior approval. Once the same acceptor
and an eligible operator have current access again, a new explicit operator
review may bind the new incarnations to the existing exact client consent.
This is not a rule requiring the client to repeat unchanged consent.

## Immutable history and current selection

Migration v14 follows unchanged v1–v13. Immutable approvals and terminal
revocations/supersessions are separate from one confined current pointer per
request. Composite foreign keys bind request, client, invoice and agreement.
The request lock comes before deterministic issuer/subject membership locks;
membership writers never acquire request locks. Locks survive the transaction's
final release checks.

A new decision atomically supersedes the prior selection permanently and advances the
generation. A matching retry returns its original actor/content-bound receipt
without extending expiry or moving the pointer. Concurrent distinct decisions
against one generation conflict; identical retries share a receipt.
Revocation appends terminal evidence and clears only the expected selection.
It still requires current operator access and its independent release gate,
but not valid old funds, sharing or client access. A stale revocation cannot
clear a newer decision. No reader falls back to a superseded approval.
An empty current selection means no selected approval, not no historical work.

## Native operator review

`GET /portal/operator/requests/{id}/work-review` shows the domain's current,
paused, expired or absent selection and descending history, with at most twenty
decisions per page. The cursor binds original database time and receipt ID at
microsecond precision, including equal-time ties. Read/scan failures are unavailable,
never successful empty history. Historical terminal receipts are never a fallback.

Selecting an explicit `invoice` opens a fresh candidate review, not an assertion
that an earlier selected approval is still valid. It shows the exact accepted
scope, deliverables, one-time/monthly amounts and terms, selected invoice's
net/VAT/payable facts, and every transfer observation/revision. The candidate
and comparison identity are captured under the same request lock. Its native
`POST .../work-approvals` reconstructs the evidence and checks generation before
recording a decision. A separate `POST .../work-revocations` clears only the
expected selected decision. GET/HEAD never write authority or review contexts.

Both actions use existing domain operations and actor/content-bound retries.
Revocation does not need positive old funding, current client consent, the former
approver's membership or an unexpired old deadline. No migration or grant changes
are needed. The UI does not calculate eligibility from localized labels or totals.

The whole frame is buffered. A real maintained SCS store/codec snapshot is captured
before acquiring a work transaction; it is only comparison material. After the last
SDK wait, final SQL on that transaction binds the exact original durable session
bytes/token, database expiry and current original operator workspace/role. It
does not replace the request's original principal or deadline with a new identity.
Changed/deleted durable data fails that check. Request/membership locks protect
evidence until commit; final expiry, cancellation and SDK-state checks still apply.
No second pool connection is acquired while a work transaction is held, including
when the configured pool permits only one connection. Contact retains its existing
post-SDK maintained-store capture and final SQL check.

Grant SELECT/INSERT on all three new relations, and UPDATE only on
`portal_work_current.approval_id` and `generation`. Do not grant pointer-key
updates, DELETE, TRUNCATE, trigger/maintenance/reference privileges, delegation
or ownership, directly or through assumable roles. The normal web constructor
refuses incomplete or excessive grants. The separate migration executable
requires ownership of all managed relations, including these three.

## Release and verification

The independent false-default `portal-work-approvals` decision has no environment
activation mapping. Review retirement under
[#268](https://github.com/devantler-tech/business-site/issues/268) by
20 November 2026. Approval reads/writes also require the existing identity,
request, triage, proposal, sharing, acceptance, invoice and credit gates.
Revocation deliberately requires only identity, request, triage and approval
gates, so invalid old evidence cannot trap an approval.

Run the full restricted-role PostgreSQL race suite, compiled migration ownership
controls, default Go quality checks and normal package validation in
[README.md](README.md#build-and-run-the-combined-package). Fixtures are fictional,
disposable and use no real payment, customer or bank connection. Passing source
tests and a merged foundation are not deployed identity, recovery, legal
clearance or permission for an agent to execute client work. The eighteen new
EN/DA work documents have explicit state/form/fact expectations and common-frame
and method-aware link controls. The original 120 document/body references remain
unchanged; they are not regenerated from the new renderer.
