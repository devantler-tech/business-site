# Durable membership incarnations

Status: proposed, 2026-10-21, for #256 and #138.

## Decision

Keep membership authority in PostgreSQL. Add an opaque UUID incarnation to each
current membership and an immutable transition table maintained by a
migration-owned trigger pair. A BEFORE trigger assigns an incarnation; an AFTER
trigger records only actual inserted, updated or deleted rows, in the same
transaction. A conflict that inserts nothing creates no event; an upsert records
only its actual row operation. Updates replace caller-supplied incarnations. Deleted
memberships leave their transition evidence behind. Re-enrollment cannot reuse a
retained incarnation, even when issuer, subject, client and role match old values.

Migration v13 appends to unchanged v1–v12. Its baseline records only the state
observed under the migration's table lock, not earlier changes or a human actor.
Subsequent events retain the prior incarnation and current/deleted membership
snapshot, with the prior event supplying the earlier values.
No event claims who caused a direct database write. UUID uniqueness is enforced
by the retained table's primary key; a collision fails the transaction rather
than reusing evidence. There is no sequence, per-identity event limit or reserved
revocation slot to exhaust. Ordinary database/storage failure still refuses the
transaction; this is not an unconditional revocation-availability guarantee.

The trigger is SECURITY DEFINER with a fixed trusted search path and qualified
relations. Its public EXECUTE grant is revoked. The runtime retains membership
DML but receives only SELECT on transition evidence: no direct event writes,
delegated grants, trigger creation, table maintenance, truncation or ownership.
The startup permission check rejects those powers directly or through assumable
roles. Migration completion checks the additional table, function and trigger.
Privileged schema owners and backup operators remain outside runtime authority;
restoring an entire old database also requires invalidating later external
authority, not treating this ledger as an external anti-rollback service.

A transaction-local reader locks the current, non-revoked membership FOR SHARE
and returns its incarnation only when its expected client/role and retained
transition agree. It neither starts nor commits a transaction, grants a role,
nor approves work. Future approval callers must first take their request lock,
then membership locks in deterministic identity order and keep both through
commit. Membership transitions lock no requests and perform no request scan.
An absent membership cannot supply an approval incarnation; absence is not locked
as a substitute for an active member. Concurrent revocation either precedes the
reader or waits for its owning transaction to finish.

## Alternatives

Application callbacks miss the currently admitted direct DML paths. Current-value
equality and resettable row counters cannot distinguish restoration or deletion
and recreation. Restricting all provisioning to a new stored-procedure API would
change the existing control-plane contract. Scanning and invalidating every
request during a transition adds reverse lock order and unbounded work.

This additive storage invariant uses the existing default-off identity boundary;
it has no independent activation switch that could silently stop recording
transitions. It adds no HTTP endpoint, environment mapping, approval record,
payment rule, consent policy or queue/API/MCP worker. Private release gates stay
off. A successful source migration test is not a production migration or portal
deployment.

## Verification

Use real disposable PostgreSQL with the existing restricted roles. Cover the
missing-history condition before implementation, baseline limitations, normal
invitation/revocation, direct mutations and value restoration, identity-key
changes, deletion/re-enrollment, caller-supplied tokens, transaction rollback,
pool restart and concurrent readers/writers. Prove read-lock ownership and
permission refusal, including indirect privileged roles and event tampering.
Retain all existing migration rewinds, permission, browser, dedicated contact,
race, deadline and source-publication checks. No fixture runs against a customer
database. Implementation, review, protected merge and exact-source publication
are separate delivery gates.

The trigger's privilege boundary follows PostgreSQL's
[SECURITY DEFINER guidance](https://www.postgresql.org/docs/17/sql-createfunction.html#SQL-CREATEFUNCTION-SECURITY);
UUID generation uses the built-in
[UUID function](https://www.postgresql.org/docs/17/functions-uuid.html).
