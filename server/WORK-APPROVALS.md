# Private work-approval foundation

This internal service records an operator's explicit decision against an exact
current agreement and fully credited external invoice. It has no HTTP handler,
client form, worker, queue, API or MCP adapter. It does not activate the portal,
start work, collect money or create a monthly entitlement. Operator screens and
their native EN/DA review remain [#138](https://github.com/devantler-tech/business-site/issues/138);
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

Grant SELECT/INSERT on all three new relations, and UPDATE only on
`portal_work_current.approval_id` and `generation`. Do not grant pointer-key
updates, DELETE, TRUNCATE, trigger/maintenance/reference privileges, delegation
or ownership, directly or through assumable roles. The normal web constructor
refuses incomplete or excessive grants. The separate migration executable
requires ownership of all managed relations, including these three.

## Release and verification

The independent false-default `portal-work-approvals` decision has no environment
activation mapping. Review retirement under
[#260](https://github.com/devantler-tech/business-site/issues/260) and #138 by
20 November 2026. Approval reads/writes also require the existing identity,
request, triage, proposal, sharing, acceptance, invoice and credit gates.
Revocation deliberately requires only identity, request, triage and approval
gates, so invalid old evidence cannot trap an approval.

Run the full restricted-role PostgreSQL race suite, compiled migration ownership
controls, default Go quality checks and normal package validation in
[README.md](README.md#build-and-run-the-combined-package). Fixtures are fictional,
disposable and use no real payment, customer or bank connection. Passing source
tests and a merged foundation are not deployed identity, recovery, legal
clearance, operator review UI or permission for an agent to execute client work.
