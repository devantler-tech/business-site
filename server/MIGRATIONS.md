# Prepare the portal schema

This is a separate operator procedure for the unified application, not the
website's first-run path or permission to operate a production database. Use it
only on an authorized store through the reviewed deployment job. Agent shell
access remains read-only against production. The disposable tests described in
[README.md](README.md#actual-database-tests) require no production access.

## Prerequisites

Use the verified signed immutable application image or its matching runtime
archive. Check its source receipt and provenance using the
[distribution procedure](README.md#restricted-container-distribution). The
one-shot job uses that same executable and embedded schema versions; do not copy
SQL from tests or rebuild under an existing receipt. The job does not serve
public pages, so it needs neither a company-disclosure projection nor a broker.

Provision a dedicated logical database and separate migration and web-runtime
roles through the reviewed store/secret contract. The migration role needs
creation rights on the `public` schema and ownership of its migration ledger
and application tables and consent-binding domains. Table GRANT privileges do not replace ownership. It may
own its dedicated database. It must not
be a superuser, bypass row-level security, create roles/databases, replicate, or
assume another role. The database-scoped implicit `pg_database_owner` membership
is the sole membership exception. The effective and session identities must
match. Existing `portal_` relations in `public` must belong to the current role
before migration; the same ownership requirement covers `portal_` domains.
Managed `portal_` functions also belong to the migration role. Completion checks
the owned membership transition table and the two installed trigger functions,
their fixed trusted search paths and complete enabled row-trigger bindings.
Success additionally requires the complete managed table set
to exist and belong to that role. A current readable ledger or table grants alone
cannot produce a successful job. Keep this table set aligned with the embedded
migrations when evolving the schema. The three consent domains must also exist
as owned domains in `public`, with their expected text/integer base types.
This check is not proof that the underlying
database is tenant-isolated.

Project the migration connection into an absolute-path, regular private file
readable by the job's UID. The file must have no group/other permission bits and
contain at most 16 KiB. Both this file and the CA file must be owned by the
job's operator UID or root. Every selected directory and symlink, including
directories reached through links, must have that ownership. Directories must
not be group/other-writable, except root/operator-owned sticky shared directories
whose selected entries have the same trusted ownership. A private leaf alone
does not protect an input under a replaceable parent. Use canonical absolute
input paths without `.` or `..` components. Confined projected-secret links are
supported; escapes, link cycles and non-regular files are refused. Use TLS with full certificate verification,
without insecure or plaintext fallbacks. Supply a complete `postgres`/`postgresql` URI
with explicit host, port, database, user and password. Only `sslmode=verify-full`
and an absolute `sslrootcert` path are accepted as query settings; no ambient
credential, service-file, client-certificate or query routing override is used.
The explicit job discards PostgreSQL-prefixed environment settings inside its
own process; it changes no persistent host settings. The CA bundle must be a
readable regular file containing at most 64 KiB. Its permissions need not be
private because it is public trust material, not a credential, but group/other
write permissions are refused to protect its integrity. The job reads a
confined nonblocking snapshot and passes a private temporary copy to the native
TLS parser, then removes it. Provide writable ephemeral temporary space even
when the application root filesystem is read-only; the reviewed restricted
container tests already supply this temporary mount. Temporary-copy paths must
meet the same ownership and directory-integrity requirements.
Administrative/default databases are refused. Keep connection values and passwords out of
arguments. Keep private paths and values out of application images, public
assets, receipts and logs. Do not
project this file or the migration role into the web deployment.

## Run the explicit job

From the extracted package directory, replace `MIGRATION_FILE` with the path to
that private projection. Inside the restricted image the executable is
`/app/business-site`; override its ordinary web arguments with only these job
arguments:

```sh
./business-site --migrate --migration-database-file "$MIGRATION_FILE"
```

To also grant the web login its runtime privileges, name that existing login:

```sh
./business-site --migrate --migration-database-file "$MIGRATION_FILE" --runtime-role portal_web
```

The role must be a lowercase SQL identifier, differ from the migration login,
be able to log in, have no superuser, bypass-RLS, create-role, create-database
or replication attribute, and not be able to assume the migration role. In the
same transaction as the schema changes, the job revokes what the migration role
earlier granted that role on every managed table, then grants schema usage and
exactly the privileges listed below. Grants made by another role are not
revoked; web startup still checks the runtime boundary below. A refused role
or grant rolls back the whole job; re-running converges on the same grants. The
list lives beside the embedded migrations in `internal/identity/migrate.go`
(`runtimeGrants`); a migration that changes runtime access updates both.
Without `--runtime-role`, the job grants nothing, as before.

In Kubernetes, the kubelet writes a Secret projection root-owned, and with the
pod's `fsGroup` it is readable by that group (mode 0440), which the private-file
option refuses. A Job reading a Secret uses `--migration-database-projection`
instead of `--migration-database-file`, running as a non-root UID whose primary
group is the `fsGroup`. Only that group may read the file, and no other user may.
Every other check above is unchanged, and the two options cannot be combined.

```sh
/app/business-site --migrate --migration-database-projection /run/portal-migration/database-url --runtime-role portal_web
```

No server, version, public-check, disclosure or portal-settings option may be
combined with this mode. Repeated migration options are rejected. The operation is off by default;
browser dependency settings and identity flags cannot activate it. Unbound
ordinary Go builds are not distributable migration jobs.

Success exits zero and prints `portal schema migration completed`. Failure
exits nonzero with `portal schema migration failed`; dependency values, SQL and
record contents are not printed. A failed job must stop the deployment sequence.
Never reinterpret a timeout or missing output as completion.

The job uses one connection, a five-second connect/statement limit, a one-second
lock limit and a thirty-second overall deadline including cancellation. The
actual embedded transaction serializes migrations with its advisory lock,
checks a contiguous ordered version/checksum ledger, and commits the ledger
with its schema changes. Unknown, changed or missing-prefix versions fail
instead of deleting records. Concurrent jobs either safely repeat or fail on
the bounded lock wait; there is no unbounded automatic retry. Investigate a
failure and retry explicitly only after the prior job has ended and the
obstacle is understood.

## Web-runtime permissions and deployment proof

Migration does not create roles, invite clients, change membership, dispatch
offers or enable any release flag. It grants web permissions only to an explicit
`--runtime-role`; otherwise the reviewed store provisioning must grant them. The
separate web role gets schema usage and only its required table DML:
SELECT/INSERT/UPDATE/DELETE on login attempts, invitations,
memberships, sessions and contacts; SELECT/INSERT only on immutable requests and
proposals, proposal-visibility events, acceptance receipts, external invoices,
bank-transfer identities and credit revisions; SELECT/INSERT/UPDATE
only on the bounded ephemeral proposal-review contexts. The shared key, event and
content-hash domains centralize binding validation; each column retains NOT NULL.
Neither direct nor assumable domain ownership belongs to the runtime: it could
alter those constraints. No schema/ledger ownership,
DDL, broad role membership or UPDATE/DELETE/TRUNCATE of immutable
request/proposal/visibility/acceptance/invoice/credit history belongs to that role. Review-context
DELETE/TRUNCATE is also refused. Context rotation never mutates the receipt ledger.
Actual
web startup independently checks this boundary and refuses incomplete grants.

The web role may also read the ledger (SELECT only on `portal_schema_version`, which
holds version numbers and checksums, never records). Web startup refuses to serve the
portal until the ledger holds every migration that release embeds, with unchanged
checksums. A new web revision and its migration job start together, so the revision
serves only after its job has finished. A ledger with later versions is accepted,
so an older release keeps serving while a newer one migrates. The job itself refuses
a ledger newer than the versions it embeds, so roll back to such a release only if
it is compatible.

v14 adds `portal_work_approvals`, `portal_work_revocations` and
`portal_work_current` after unchanged v1–v13. Grant SELECT/INSERT on all three,
and UPDATE only on the current pointer's `approval_id` and `generation` columns.
Do not grant request/client key updates, immutable-history mutation, DELETE,
TRUNCATE, references, triggers, maintenance, delegation or ownership to the
runtime or an assumable role. The compiled migration job must own these relations
as well as every earlier managed relation. Only `--runtime-role` grants the web permissions.
See [the approval boundary](WORK-APPROVALS.md); this migration adds no endpoint
or client-work execution authority.

v15 extends only the immutable request table's product check to accept
`website`, `app`, `service` and `other`. Earlier migration bytes/checksums,
existing requests, indexes and runtime grants are preserved. Unknown categories
still fail the database constraint. This adds no priced product, payment or
execution authority and enables no release gate.

v16 adds `portal_orders`, `portal_order_charges` and `portal_numina_contacts` after
unchanged v1–v15 (see [portal billing](BILLING.md)). Grant SELECT/INSERT on orders and the
Numina contact map, and SELECT/INSERT/UPDATE on charges, whose state, provider reference and
send/settle markers change as an invoice advances. Never grant DELETE, TRUNCATE, references,
triggers, maintenance, delegation or ownership on them to the runtime or an assumable role.

v17 adds `portal_card_checkouts` and `portal_stripe_events` after unchanged v1–v16. Grant
SELECT/INSERT/UPDATE on card checkouts (every session is kept; only its state and subscription
change) and SELECT/INSERT only on Stripe events, which are applied once and never changed.

v18 adds `portal_order_ends`, `portal_card_cancellations` and `portal_charge_reversals` after
unchanged v1–v17 (see [portal billing](BILLING.md#ending-and-reversing-orders)). Grant
SELECT/INSERT only: an order's end, Stripe's confirmed subscription end and each refund or
dispute are recorded once and never changed or deleted.

v19 rebinds work approval to an order's funding (#268) after unchanged v1–v18. New approvals
name their order and its settled funding charge, and composite foreign keys bind the request,
client, acceptance and exact amounts. Earlier rows keep their invoice binding unchanged, and a
check refuses a row with both bindings or neither. No grant changes.

v20 lets a client end their own order's monthly billing (#21) after unchanged v1–v19.
`portal_order_ends` gains the `client` source with the client's issuer and subject, and a
check requires exactly those for a client end. No grant changes: the end is still recorded once.

v21 adds `portal_order_withdrawals` (#21) after unchanged v1–v20: a client's withdrawal from an
order, recorded once with the client's identity. It also adds `portal_withdrawal_cancellations`,
Stripe's confirmed end of a withdrawn order's card subscription, which takes precedence over an
end scheduled earlier. Grant SELECT/INSERT only on both, like the other fixed billing tables:
neither is changed or deleted.

v22 records the client's request, when ordering, to start work within the withdrawal period
(#21): `portal_orders` gains `early_start_at`, dated with the order. It also adds
`portal_order_completions`, the operator's record that early-started work is completely finished,
and `portal_withdrawal_settlements`, the amount owed for the work done before a withdrawal. Grant
SELECT/INSERT only on both; the order's new column is written only when the order is inserted.

v23 adds the portal's own emails (#21): `portal_mail_outbox`, an order confirmation or withdrawal
receipt queued with its order or withdrawal, and `portal_mail_deliveries`, one record per sent
email. Grant SELECT/INSERT only on both.

v24 adds `portal_launch_places`, the launch offer's places (BILLING.md, *Launch discount*): one
row per taken place, with the client, the order, the percent, the full one-time price and the
discount. Grant SELECT/INSERT only; the retention purge, as the migration role, clears the client
and order of a purged customer's place and keeps the place itself.

v25 adds `portal_order_addons`, the standard add-ons of an order (BILLING.md, *Add-ons*): the
add-on, its kind, units and unit price at the time of the order. Grant SELECT/INSERT only; the
order's prices already include them.

v26 adds `portal_request_closures`, the operator's record that a request is finished (BILLING.md,
*Feature-request priority and fair use*). Grant SELECT/INSERT only.

v27 adds `portal_addon_ends`, a client's end of one monthly add-on, and `portal_addon_removals`,
Stripe's confirmation that a card subscription no longer charges it (BILLING.md, *Add-ons*). Grant
SELECT/INSERT only on both.

v28 adds `portal_operator_audit`, one row per operator API call: who, what, when and how it
ended (CUSTOMER-IDENTITY.md, *Operator API*). Grant SELECT/INSERT only; the retention purge
deletes rows older than the retention period.

v29 adds `portal_workspaces`, including workspaces with no members yet, and records the
workspace IDs already used by invitations and memberships. Grant SELECT/INSERT only;
workspace identifiers are immutable. The existing former-customer retention
purge removes its workspace identifier together with its expired records.

v30 adds nullable producer evidence to the existing immutable mail outbox. New
complete-agreement confirmations use format 1 and bind the order, acceptance,
proposal version/content hash, terms hash and frozen body hash. Constraints refuse
partial metadata, a mismatched order/acceptance or a changed body. Existing rows
retain NULL metadata; no legacy email or sender receipt is upgraded into proof.
Runtime grants stay SELECT/INSERT only on the outbox and sender receipt tables.
This grants no service execution authority and does not prove inbox receipt.

v31 adds immutable simulated purchase receipts for the fictional client workspace.
The named preview database and runtime login receive SELECT/INSERT. The normal
runtime has no receipt privileges. The migration changes no billing or work records.

v32 adds workspace names and closure records beside immutable workspace IDs. Grant
SELECT/INSERT and UPDATE only on the display name and closure timestamp. Operator
deletion removes portal access and revokes pending invitations in one transaction.
The retained ID prevents reusing the workspace while its historical records remain.
The retention purge removes eligible closed workspaces and their settings together.

v33 records typed proposal terms and the original membership incarnations at
sharing and acceptance. Existing proposals remain in their original format.
The service fulfillment and consumer tables grant the runtime SELECT and INSERT.
Appointment sharing and prepayment remain held pending the booking workflow.

v34 adds client-bound appointment reservation IDs and append-only slot revisions.
Each revision records its interval and hold expiry. The runtime can read and
append both records. A paid appointment order references the selected revision.
The original confirmation uses producer format 3 for this order type. The
retention job deletes the order and its fulfillment before deleting the former
client's reservation and revisions. The booking service checks availability,
current funding and sender acceptance before recording the appointment consumer.

v35 records the initial calendar creation intent for a confirmed appointment
when a destination binding is supplied. The runtime can read and insert intents.
The record keeps an independent event key and points to the confirmed revision,
which also removes the intent during retention. Existing bookings are unchanged.

v36 records a shared software queue revision. Changes to requests, agreement,
approval, payment, membership, mail confirmation and closure evidence advance
it when their transaction commits. The web role can read the revision. The
migration-owned triggers are its only writer in normal operation.

v37 stores the first verified calendar result for a confirmed free introduction.
It records either the matching event's validator or a conflict for operator
review. The runtime can read and append observations. The original reservation
and intent also govern observation retention.

v38 records a 30-day removal deadline for customer sign-in accounts when an
operator permanently closes their workspace. It references the recorded
end of membership and follows its retention. The runtime can read and append
records. Provider removal is a separate operation.

v39 gives the web role a single function to hold its observed software queue
revision until transaction end. A changed revision returns false. The function
has no write path and its EXECUTE grant cannot be delegated. The web role keeps
SELECT-only table access and cannot call the migration-owned revision recorder.

v40 adds the single software-work lease slot and its claim records. PostgreSQL
assigns the next fence and acquisition time under the slot lock. An exclusive
lease window rejects overlaps, including during a concurrent insert. The claim
binds its request, approval, observed revisions and the actor's submission key.
Removing a former customer's approval removes its lease records and keeps the
global fence. The migration owner alone can access the lease storage. The
future work service must authorize selection, renewal, release and downstream
effects before it discloses a committed fence. Restore verification must prove
that a fence observed before recovery cannot authorize work afterward.

v41 records an account-removal deadline when an operator permanently removes
an individual customer or closes their workspace. The record must cite the
client deletion event from the same transaction, with its database timestamp.
The deadline is 30 days after that event. Temporary suspension remains a
membership update and is reversible.

v42 checks a new software lease against the selected approval generation and
the held queue revision. The database assigns the acquisition time after
locking the slot and revision. A missing, revoked or expired approval refuses
the insert without consuming a fence. The worker must still select the oldest
eligible request, validate all evidence and authenticate every operation.

v43 records the first calendar creation attempt before contacting the provider.
An unconfirmed attempt survives a restart and allows only a read of the
reserved event on retry. Provider absence during that read leaves the attempt
unresolved. The attempt follows retention of the immutable calendar intent.

v44 records the calendar result of cancelling an introductory meeting. It
references both the cancellation and the original creation observation. One
verified absence or conflict is retained per reservation. Retention of either
source removes the result with it.

v45 preserves the original software lease and allows one release only while
its window is open. PostgreSQL timestamps that release after acquiring the
row lock. An unchanged repeat preserves the original release. Existing lease
records and the retention deletion path are unchanged. The migration adds no
runtime grant or worker authority.

v46 requires new software leases to start without a release timestamp. Invalid
inserts stop before acquiring the slot lock or allocating a fence. Existing
lease history stays intact. A release is still a separate, database-timed
transition.

v47 limits the end of a new software lease to the expiry of its exact
approval. The lease and approval intervals both exclude their end, so an
equal expiry is valid. A rejected insert consumes neither a fence nor its
submission key. Historical lease records are preserved.

v48 stores hosting entitlements, period revisions and their current pointers.
Each revision references the accepted software proposal for the same workspace
and request, including the confirmed version and content hash. The database
locks and checks the current operator membership before insertion. Period
bounds are finite and exclude the end. Web startup refuses any privileges on
these tables or their operator guard, including grants through another role.
The existing retention purge removes a former workspace's hosting records
before deleting its agreements and preserves operator history referenced by
remaining hosting periods. The portal's authorization and period locking are
required before a workflow begins writing real hosting periods.

The operator hosting coverage query can execute under the web login. It
returns only the effective period or next scheduled period for the exact
project and workspace. The query validates the current operator membership
and refuses inconsistent period heads. The web login still has no access to
the hosting tables, and the display requires the explicitly enabled hosting
periods capability.

The hosting-period recording function checks the exact typed acceptance and
copies its maintenance terms. It holds the workspace and entitlement locks
while it checks for overlap and appends the period and its current revision.
Its runtime grant is limited to execution. Recording requires both the hosting
periods and proposal acceptance capabilities.

The workspace early-deletion migration grants execution of two owner-controlled
functions only. They report eligibility and delete ordinary records of one exact
closed workspace without granting runtime table DELETE. The deletion function
rechecks the closure timestamp, current operator membership and protected records,
leaves the identity closure history intact, and records the outcome in its transaction.
Both functions pin their search path. Runtime startup verifies ownership and refuses
public execution or delegated grants.

Workspace invitation emails have one queued notice and one delivery receipt
per pending invitation. The runtime can read and append both rows and update
only the timestamp of the last delivery attempt. Acceptance and invitation
cleanup remove both rows through their foreign keys. The scheduled retention
job clears recipients of revoked and expired invitations.

Grant SELECT only on `portal_membership_transitions`. Membership DML invokes
migration-owned triggers; never grant direct event writes, delegated event
privileges, membership TRUNCATE/TRIGGER/MAINTAIN, trigger-function execution or
trigger-bypass parameter authority to the runtime or an assumable role. These
grants are unnecessary for ordinary sign-in and trusted provisioning. The v13
baseline and subsequent immutable events stay with the database across process
restarts and deletion/re-enrollment. Do not prune them or reset incarnations as
routine maintenance. The only pruning is the retention purge (README, *Retention purge job*):
3 years after a client's last membership ended, it deletes the client's approvals, then the
whole event chain of each of its memberships whose last change is as old. A chain that names
another client with records, or that a remaining approval cites, stays, so no approval outlives
the history that invalidates it.
There is no application history cap; physical database/storage failures still
refuse a transaction rather than claim unrecorded success.

Before staging or publication, demonstrate the restricted-role
startup, persistence/restart, backup/restore, resource bounds and rollback through
the enacting deployment path tracked by monorepo#3939. A schema commit or source test does not prove
those outcomes. Never downgrade or erase the ledger for a rollback: choose a
compatible reviewed artifact and recover from a verified backup when required.
Broker registration, real-provider isolation and privacy/legal checks remain
separate release conditions; keep public Pages unchanged until replacement live
publication and rollback are proved.
