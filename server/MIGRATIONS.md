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
and application tables. Table GRANT privileges do not replace ownership. It may
own its dedicated database. It must not
be a superuser, bypass row-level security, create roles/databases, replicate, or
assume another role. The database-scoped implicit `pg_database_owner` membership
is the sole membership exception. The effective and session identities must
match. Existing `portal_` relations in `public` must belong to the current role
before migration; success additionally requires the complete managed table set
to exist and belong to that role. A current readable ledger or table grants alone
cannot produce a successful job. Keep this table set aligned with the embedded
migrations when evolving the schema. This check is not proof that the underlying
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

No server, version, public-check or disclosure option may be combined with this
mode. Repeated migration options are rejected. The operation is off by default;
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

Migration does not create roles, grant web permissions, invite clients, change
membership, dispatch offers or enable any release flag. The reviewed store
provisioning must give the separate web role schema usage and only its required
table DML: SELECT/INSERT/UPDATE/DELETE on login attempts, invitations,
memberships, sessions and contacts; SELECT/INSERT only on immutable requests and
proposals. No schema/ledger ownership, DDL, broad role membership or
UPDATE/DELETE/TRUNCATE of request/proposal history belongs to that role. Actual
web startup independently checks this boundary and refuses incomplete grants.

Before staging or publication, demonstrate the restricted-role
startup, persistence/restart, backup/restore, resource bounds and rollback through
the enacting deployment path tracked by monorepo#3939. A schema commit or source test does not prove
those outcomes. Never downgrade or erase the ledger for a rollback: choose a
compatible reviewed artifact and recover from a verified backup when required.
Broker registration, real-provider isolation and privacy/legal checks remain
separate release conditions; keep public Pages unchanged until replacement live
publication and rollback are proved.
