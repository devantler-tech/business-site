# 0029: Validate the private identity lifecycle before deployment

## Decision

Use a separate, explicit `--lifecycle-smoke` mode in the existing native image
acceptance helper. It retains the CLI, HTTP and browser baselines, then exercises
TLS and durable startup against that same original image. It does not rebuild or
substitute the candidate. Ordinary invocation stays inert.

Run it only with the pinned Linux amd64 Go 1.26.10 toolchain and Docker, through
required CI. The existing image helper requires explicit build, repository and
output paths. The lifecycle receipt is `lifecycle-smoke.json`; its source hash and
local image ID are joined to the original image receipt by a mandatory CI step.

## Private authority and transport

Generate a fresh local CA, certificates, master key and fictional passwords in an
owned private directory. The complete input inventory is sealed before any native
command runs. Missing or changed inputs refuse rather than falling back to native
bootstrap defaults. Never upload these files, raw event payloads or container logs.

Use native `init user`, `init`, `setup` and ordinary `start` as separate commands.
The first is an explicit phase-boundary interruption checkpoint: the application
role exists but its database does not. Resume with full initialization. This is
not a kill-during-transaction or arbitrary crash-recovery test.

Only initialization receives database-administrator credentials. Setup receives
explicit fictional human inputs, with email unverified and a password change
required. Ordinary serving receives neither input. Revoke the bootstrap database
role's login and prove it remains unusable before and after restart. Assert that
the application role has no superuser, role-creation, database-creation,
replication, row-security bypass or bootstrap-role membership privileges.

Both service and database connections require the fixture CA and the exact
certificate hostname. Native wrong-CA, wrong-hostname and plaintext controls must
fail. A positive database check separates transport refusal from an unavailable
database. The alternate database hostname resolves to that same owned endpoint.
The PostgreSQL key is copied into its own persistent volume under UID999 with
mode0600; the sealed host input remains read-only.

## Persistence and observation limits

Use two non-root containers on an internal network without host ports. Keep
read-only roots, no added capabilities, no-new-privileges, PID/CPU/memory limits
and bounded temporary filesystems. A unique labelled local volume holds the
database. Stop and recreate both containers against that same volume, then use
ordinary start only. Compare the intended instance and unverified human creation
events before and after restart using a seal; retain no password-bearing payload.

The declared workload is initialization/setup, two ordinary starts and three idle
observation rounds, with at least a one-second pause between rounds. Observe both
namespace-init processes separately from Docker's whole-container accounting.
These observations establish no complete-stack, login-load or production sizing.
The volume has a checked 256MiB observation ceiling, not a filesystem quota.
Execution has a 12-minute command budget; owner-bound cleanup has separately
bounded commands and must complete before a successful receipt is written.

This fixture enables no public route, real customer, Dex federation, invitation,
MFA, payment or agent execution. Actual source/image publication belongs to #308;
image admission and deployed runtime packaging remain #305; the real customer and
operator journey remains #302. Passing this fixture is not portal activation or
backup/restore acceptance. Implementation and native evidence are tracked in #465.
