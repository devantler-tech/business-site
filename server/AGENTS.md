# Portal server maintenance

The parent repository's AGENTS.md and reviewed shared engineering contract apply.
The explicit developer-only `cmd/contact-trial-host` uses an inherited retained
loopback TLS listener and bounded private setup pipe. Read `CONTACT-TRIAL-HOST.md`.
Keep migration, invitations, synthetic broker and collection outside this child;
preserve the maintained authority and production runtime graph. Its real-process
correctness controls are not comparative measurements or portal activation.
The explicit offline `cmd/contact-trial-report` tool analyzes only the frozen
paired timing format. Read `CONTACT-TRIAL-REPORT.md` before changing it. Preserve
whole-journey pairing, failure retention, closed bounded input and private output.
Its overall decision remains inconclusive; it neither collects trial evidence nor
activates portal behavior. ADR 0014/0015 and the full #213 runner remain separate.
The explicit `DeveloperContactBrowser` composes only the private contact experiment
with a caller-owned fixed `contactrenderer.Supervisor`; `Browser` and the executable
retain Go presentation. Read `CONTACT-RENDERER.md`. Buffer the complete frame before
headers and revalidate the original durable session, expiry and query client after
renderer/SDK waits, including initial opt-out. Use the maintained context store and
codec and bind its decoded bytes in final SQL, not SCS's request cache. Never
extend a session on a read, pass identity/client
selectors to the child, or treat framed output as authority or arbitrary safe HTML.
The selected Node-24 renderer integration needs BOTH disposable PostgreSQL fixture
URLs and real compiled artifacts; ordinary Go/database tests do not need Node.
Run the real Go trial host separately with `go test -race -tags=contacttrialintegration
./internal/identity -run '^TestContactTrialHost' -count=1 -timeout=90s`. Source CI
requires this step after database provision, alongside the complete ordinary suite
and compiled renderer integration. Preserve each package's existing 90-second bound;
never make these expensive real-process controls optional to fit the ordinary suite.
This Go module is the unified product's **default-off browser identity boundary**,
not a deployed portal. It provides sign-in/callback/shell/logout and private contact
profile handlers and separately default-off free project-request intake; no order,
payment collection or agent endpoint exists. Read `REQUESTS.md` for the request boundary and
`INVOICES.md` for private external invoice metadata and `CREDITS.md` for operator
bank-credit evidence. Contact emails remain
self-declared unless the separately default-off broker confirmation in `CONTACTS.md`
succeeds; they are never identity or order authority. Never present synthetic broker
tests or the loopback visual gateway as Google/Microsoft login or production
ingress/cookie proof.

Use the exact Go version in `go.mod` with `GOTOOLCHAIN=local`; run `gofmt`,
`go mod verify`, `go vet ./...`, `go test -race ./... -count=1 -timeout=90s` and
the pinned `govulncheck` command in CI. Database tests require BOTH fixture URLs
documented in README.md and fail rather than skip if absent. Never point them at
a real customer database; they refuse non-fixture database/role names and truncate
only the fixture's thirteen identity/session/contact/request/proposal/visibility/consent/invoice/credit tables. Provision a fresh disposable PostgreSQL
17 service with `testdata/provision.sql`; never start a permanent service for tests.
The tracked module-local `.go-test-fixture.json` also opts input-free organization
validation into its closed disposable PostgreSQL fixture. Keep its namespace and
database aligned with these test-only roles; never add endpoints or credentials.
Run `bash scripts/runtime-dependencies.test.sh` from the repository root. Packaging
compares `DEPENDENCIES.md` with the actual executable's module metadata before
copying upstream notices. Missing/stale/extra entries and incomplete reads fail;
test-only dependencies are not linked runtime modules. License links use verified
immutable raw files, not HTML presentation pages. This is not legal clearance.
Run the actual browser-script controls separately from the repository root with
Node 24: `node --test scripts/portal-request-limits.test.mjs`. The required source CI
job runs that exact command unconditionally after explicitly setting up Node 24;
do not make it optional or couple the Go database suite to an ambient Node version.
The separate template-quality step uses HTMLHint 1.9.2 and keeps all 23 pinned
default rules; only the literal Go field `.Request.Product` is allowed through
the attribute-case rule. Actual rendered offering selections are tested in EN/DA.
Revive retains its selected-file batch and default rules with package context
prepended; test all selected files and a genuine missing-package-comment control.
MegaLinter's supported executable override runs pinned golangci-lint 2.14.0 using
the declared compiler, preserving its native rules, project scope and fix mode.
The analyzer regression exercises all actual module packages and a genuine format
diagnostic against the pinned container's rule snapshot; native container proof is
still separate. Never replace it with a disabled scanner or ignored failures.
Lychee's test-only preprocessor renders all five actual private templates in EN/DA.
It checks effective form methods against the production route registrations and
canonical public/private boundary, and exercises actual asset handlers. Ordinary
public links and documents retain normal Lychee checking; no status is exempted.
Run `bash scripts/portal-links-preprocess.test.sh`; `--native` additionally requires
the actual Lychee executable and checks a broken public link and adapter failure.
Rendered route contracts are not authenticated browser, database or live proof.
The CSpell glossary records audited current Danish UI copy and technical names;
it is not a complete Danish dictionary. Keep default checking enabled and reject
English/Danish typos. Three exact encoded paths are scoped only to their actual
refusal fixture, never admitted as ordinary words or whole-file exclusions.
The credential scanner retains its complete recommended preset. Only nine fully
anchored fictional database strings are recognized; altered host, password, role
or database still fails. Preserve the independent private-key control and never
print source content or detected credential values in test diagnostics. The
pinned upstream detector itself does not report every placeholder or repeated
password; the regression is not a claim to overcome that upstream limitation.

From the repository root, the optional full quality check is
`bash scripts/check-portal-quality.sh`. It requires Go 1.27.2 and golangci-lint
2.14.0 built with Go 1.27.2 before invoking the entire module's default linter set.
`--no-config` ignores ambient configuration so local suppressions cannot hide findings.
The display limits expose all diagnostics; never use a reduced delta as a full
clean verdict, deduplicate findings sharing a line or silence a rule. Fixture cleanup
errors fail tests; intentional best-effort production cleanup must explain its
resource lifecycle at the call.

Preserve issuer/subject identity, nonce/authorized-party checks, atomic database
consumption, current membership and concrete record client scopes. A token's
email/groups or browser fields cannot assign a role. `Invite`/`Revoke` are trusted
control-plane provisioning methods, not permission to expose unauthenticated APIs.
Browser routes use maintained code exchange/PKCE, durable opaque sessions,
token-based CSRF, current membership and accessible EN/DA forms. Keep the
production release gate off until monorepo#3936 and #3939's real-provider,
deployment, recovery and privacy prerequisites clear. Synthetic source tests
do not grant production activation.
Use the injected OpenFeature SDK client and false-default `portal-identity`
boolean on every operation; missing/error/type-mismatched evaluation denies.
Do not introduce a custom environment-flag parser or cache an earlier on verdict.
The executable's optional staging composition uses maintained OpenFeature from-env
v0.1.6, mapping only identity and requiring an operator lease of at most ten minutes.
Environment rotation requires restarting the process; expiry, cancellation and SDK
shutdown revoke retained clients. Test the compiled command, real disposable
database and synthetic TLS broker. No service or production activation is implied.
Check SDK READY before and after each identity evaluation; ERROR/STALE can otherwise
still evaluate through the SDK. Keep private activation values out of build assets,
images, receipts and logs, and keep other work decisions independently closed.
Contact confirmation additionally requires false-default `portal-contact-verification`;
its retirement review is business-site#93, due 14 November 2026. Read `CONTACTS.md`.
Only an explicit, CSRF-protected purpose-bound flow requests email claims. Confirm
the exact saved revision/address for the same broker identity and current client;
never use email to link identities, grant roles or authorize work. Ordinary sign-in
does not confirm contacts. SDK waits precede private-read authorization and final
write expiry/cancellation checks. Edits clear confirmation/provenance; recovered
draft values cannot inherit a saved address's confirmation. The environment provider
remains identity-only, with no new activation mapping. Preserve unchanged v1–v7;
v8 adds bound purposes and confirmation provenance without broadening table authority.
Free project requests additionally require false-default `portal-requests` on every
operation. A receipt never authorizes a quote, agreement, payment or worker execution.
The request flag's lifecycle is tracked in business-site#57. Requests are immutable;
the runtime needs SELECT/INSERT only on that table, not UPDATE/DELETE or DDL.
Read-only operator review additionally requires false-default `portal-triage`
on every operation, plus current server-owned operator membership in its SQL.
It never grants client impersonation or paid-work approval. Keep keyset pagination
bounded and ordered by original submission time/ID with microsecond precision.
The triage flag's retirement review is business-site#62, due 12 November 2026.

Operator draft preparation and history additionally require false-default
`portal-proposals`; its retirement review is business-site#65, due 12 November 2026.
Read `PROPOSALS.md`. Proposals append immutable versions, require expected-version
and content-bound retry keys, and retain original intake order. Grant their table
SELECT/INSERT only, never UPDATE/DELETE/TRUNCATE or schema ownership. A draft is
not a dispatched offer, agreement, payment or work authorization. Keep credited
funds, worker claims and API/MCP execution separate from draft preparation.

Explicit sharing/withdrawal additionally requires false-default
`portal-proposal-sharing`; review retirement under business-site#78 by 13 November
2026. Its append-only v7 ledger follows unchanged v1–v6. Client reads expose only
the latest explicitly shared version of their own request, never draft history
or retry/actor metadata. Withdrawal hides future reads without restoring an older
version. Keep the same per-request writer lock, current membership rechecks,
content/actor-bound retries and SELECT/INSERT-only immutable history permissions.
Read `PROPOSALS.md`; ordering remains absent. Exact client consent additionally
requires independent false-default `portal-proposal-acceptance`, with release-gate
review business-site#106 due 14 November 2026. Explicit native POST review creates
a short, actor/request/client/content/version/event-bound context, never a session
write on GET. v9 follows unchanged v1–v8. Review contexts alone may be updated;
their bounded row does not own consent history. Acceptance receipts copy immutable
bindings with SELECT/INSERT-only runtime authority and no FK to mutable contexts.
Shared key/event/hash domains retain column NOT NULL constraints; migration
completeness checks their ownership and base types. Refuse direct or assumable
runtime domain ownership, which could weaken those validation constraints.
Share only the request lock, never the operator writer's authorization helper.
Lock current client membership after that request lock and independently derive
request ownership. Recheck gates/expiry/cancellation before commit. Final display
SDK waits precede the final membership SQL checkpoint.
Optional client and operator detail-display SDK evaluations, including false/error
outcomes, precede all private reads; the readers retain their independent
authorization checkpoints.
Original actor-bound retries return historical receipts, not consent to later scope.
New unshared drafts remain
readable only through the old shared snapshot and are never acceptable or exposed.
No environment activation mapping, credited funds or worker eligibility is added.

External invoice metadata additionally requires independent false-default
`portal-invoices`; review retirement under business-site#117 by 15 November 2026.
Read `INVOICES.md`. Record only explicit external references, purpose/period and
exact positive DKK minor units against current accepted proposal content. Never
derive a first-month invoice from proposal prices. v10 follows unchanged v1–v9;
its composite foreign key preserves the immutable acceptance binding. Retain
SELECT/INSERT-only runtime privileges, content/actor-bound retries, bounded
history and the shared request/operator lock. Optional display SDK waits precede
private reads. Historical invoices grant no payment, monthly or execution
authority; no environment activation mapping or client/agent invoice route exists.

Migrations run under a separate migration role; the runtime must not own schema
creation, the migration ledger, superuser or BYPASSRLS authority. Authorization
here uses explicit current membership, NOT a claim of database row-level security.

Manual bank-credit evidence additionally requires independent false-default
`portal-credit-reconciliation`; review retirement under business-site#120 by
15 November 2026. Read `CREDITS.md`. v11 follows unchanged v1–v10. Reserve each
case-sensitive source/reference pair globally to its immutable invoice/client,
including after invalidation. Append corrections under the shared request lock;
never sum historical confirmations. Unverified/disputed evidence prevents full
credit, and excess is distinct. Keep 32 identities per invoice and 16 revisions
per identity, reserving the last for invalidation. Preserve complete history,
content/actor-bound retries and SELECT/INSERT-only runtime permissions. Historical
money remains recordable after a re-quote but never approves its replacement.
No environment mapping, bank connection, client/agent route or work-start
authority is introduced. Optional display SDK waits precede private reads.
The web constructor binds both session and effective database identity to the
parsed login before its permission gate and broker discovery. Preserve the real
restricted-login positive control and mismatched-login refusal controls.
The actual executable's `--migrate` job requires a private verified-TLS migration
connection file, rejects broad/assumable authority and mixed server options, and
has fixed connection/statement/lock/overall deadlines. Read `MIGRATIONS.md` before
changing it. Normal web startup must never read migration credentials or perform
DDL. Exercise the compiled job against its uniquely named disposable databases;
source proof never authorizes a production migration or customer activation.
Customer-record handlers must authorize the server-owned record's client ID and
scope the data query; an application helper is not blanket database isolation.

Keep fixtures fictional. No tokens, login secrets, customer records or raw
database errors in public responses/logs. No server files under `public/` or the
Astro content collection. Static Pages publication remains source-owned and
uploads only `dist`; it does not deploy this module. Application code stays here,
never in the monorepo or a separate portal source repository.

Sign-in credentials select exactly one ordinary owner-only file or explicit
`PORTAL_CLIENT_SECRET_PROJECTION_FILE`. The latter requires a non-root process,
confined same-descriptor regular-file reads and root/current-user ownership;
optional group read must match the primary group. Reject mixed/relative inputs,
group writes, other-user access, executable/special modes and oversized/empty data.
Keep generic diagnostics, no scratch copy and restart-based rotation. Configuration
cannot activate identity. Preserve the separate migration credential transport.
Real constructor and compiled staging tests use atomic links and matching-group
files; actual Kubernetes controller/wiring remains platform#4854.

The production public-content profile accepts explicit company-disclosure input
only. Its separate `--company-disclosure-projection` mode reads a confined,
bounded, root/current-user-owned file with optional primary-group read access
into an immutable in-memory snapshot. Reject writable groups, other-user access,
execution/special bits, mixed input options and default-profile use. Preserve the
owner-only credential reader and use no writable scratch or copied private file.
Actual image controls use fictional atomic projections; platform#4854 still owns
real Kubernetes projection/controller proof. This configuration grants no portal
activation, migration or authentication authority.
