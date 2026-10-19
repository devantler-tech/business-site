# Portal identity and browser sign-in

This module supplies a Go executable composing the selected public output with the
**default-off HTTP identity handlers**, not a deployed portal. It discovers one configured HTTPS
OIDC broker through maintained go-oidc, verifies signed ID tokens and the attempt
nonce, and uses PostgreSQL for browser-bound login attempts, bound invitations
and current client/operator membership. Accounts use issuer + subject, not email.
Synthetic tests use a local TLS broker with generated keys; they are not proof of
live Dex, Google or Microsoft federation.

For the credential-free public-site first run and contribution workflow, start with the
[repository README](../README.md) and [contribution guide](../CONTRIBUTING.md). This reference
covers the optional combined runtime and its separate disposable database tests. Implementation
history starts at [business-site#33](https://github.com/devantler-tech/business-site/issues/33)
under [monorepo#3936](https://github.com/devantler-tech/monorepo/issues/3936).

The foundation evaluates the boolean OpenFeature flag `portal-identity` with a
false default on every operation, using the application's injected SDK client.
Missing client/flag, false, a wrong type or evaluation error all deny access;
missing broker/store also denies it. Changing the provider to false closes the
boundary without creating a new service. Ordinary startup installs the maintained
SDK's no-op provider. The optional [bounded staging provider](#bounded-identity-staging)
uses operator-only process configuration; it is not production activation.
Public Pages builds
never import this module. The existing mock preview remains a separate simulation.
The temporary release flag's retirement task is [#34](https://github.com/devantler-tech/business-site/issues/34),
with a review date of 10 November 2026. An overdue review is not activation
permission: keep it closed until the remaining parent release gates are proved.

## Security boundary and remaining work

`Begin` stores independent state/browser digests, nonce and a PKCE verifier
for ten minutes. `Complete` atomically consumes the browser-bound attempt and
checks the signed token/nonce before redeeming a subject-bound invitation.
Concurrent attempts cannot consume one invitation twice. A duplicate identity
cannot transfer clients or revive revoked access. Membership is re-read on every
authorization, which requires a concrete server-owned record client ID.

`Browser` constructs sign-in, callback, private workspace, operator and logout handlers
for one fixed HTTPS callback at `/portal/callback`. The callback consumes the
browser-bound attempt before exchanging the authorization code with its S256 PKCE
verifier. Code exchange is bounded and cannot follow redirects. Signed issuer,
audience, expiry and nonce checks remain mandatory. No return URL, email, provider
group or browser-supplied role grants authority.

Opaque SCS sessions are stored in PostgreSQL with hashed storage keys, rotated
across sign-in, destroyed on logout, and bounded by both token expiry and a
30-minute absolute lifetime. Each private request rechecks current membership;
only a stored operator role can access `/portal/operator`. Session and CSRF
cookies are Secure, HttpOnly, SameSite=Lax and `__Host-` scoped. Patched nosurf
protects state-changing forms; fixed Host/Origin checks, no-store responses and
an origin-only referrer policy complement it. EN/DA native forms support keyboard,
responsive layout and System/Light/Dark appearance. Only public CSS/JS are exposed.

The five private page templates share their document metadata, header and footer
through ordinary Go `html/template` definitions in `portal.html`. Each page keeps
its existing native forms, content and navigation targets. This presentation
sharing adds no renderer process. The explicit [developer contact
composition](CONTACT-RENDERER.md) can use the bounded compiled Svelte body; ordinary
`Browser` and the production executable retain Go with no Node activation mapping.
Both buffer contact responses and revalidate the original durable session/client
after waits. A replacement identity, client or extended deadline cannot authorize
the original response, and a read never refreshes or resurrects a deleted session.

`Invite` and `Revoke` remain **trusted control-plane operations**, not public
HTTP endpoints. An operator workspace does not expose these operations. Likewise,
`Complete` accepts only a server-obtained token, never a browser authentication claim.

Authenticated clients can edit their name, contact email and optional company at
`/portal/contact`. The actual query uses current server-owned issuer/subject membership
to select the client record; URL/form client IDs and operator overrides are not accepted.
After reading, it rechecks current client membership and the original client
binding before returning even an empty profile. Membership SQL completion also
checks expiry and cancellation. These are final read authorization checkpoints,
not an atomic guarantee against a later change or a way to retract received data.
CSRF/origin checks and bounded single-value fields protect saves. A revision comparison
rejects stale/concurrent updates instead of silently overwriting newer details.
Saved addresses are **unverified** unless the client explicitly completes the
separately default-off [sign-in-provider confirmation](CONTACTS.md) for that exact
saved address/revision. Any profile edit clears confirmation. Neither state links
sign-in identities, authorizes orders or verifies agreements/payments. No mail
delivery provider, SMTP connection or individual identity-provider SDK is used.
Do not enable real contact collection before the parent's privacy/retention gates clear.

The parent remains open for production broker/provider configuration and
ingress deployment, configured-provider contact proof, other-address verification,
retention/expired-record cleanup and
deployed two-client/operator proof. `Browser` requires a live least-privilege store,
a configured broker and a private client secret; construction neither migrates
the database nor enables the injected flag. There is no production identity endpoint,
production customer intake, payment, queue, MCP or agent execution. No paid service is used.

The separately default-off [project-request intake](REQUESTS.md) lets a synthetic
invited client submit a free brief and revisit a durable private receipt. It does
not create an order, approve a price, accept an agreement or authorize execution.
The independent false-default `portal-triage` gate adds read-only operator review
over those same immutable records, with current operator authorization and bounded
oldest-first pagination. It neither joins contact profiles nor approves work.
The independently gated [draft proposal history](PROPOSALS.md) lets the operator
prepare bounded immutable scope and price versions; it does not dispatch an offer,
obtain acceptance or authorize work. Independently default-off exact client
acceptance records an audited
version/content-bound receipt after explicit review; it never approves payments or
worker execution. See [the consent boundary](PROPOSALS.md#exact-client-review-and-acceptance).

Private [external invoice records](INVOICES.md) let the operator associate an
explicit invoiced amount and purpose with exact current client acceptance.
They do not generate invoices, confirm funds or authorize work, and remain
independently default-off with no environment activation mapping.

Separately default-off [bank-credit evidence](CREDITS.md) records operator
observations and append-only corrections against those invoices. Exact confirmed
totals distinguish partial/full/excess credit; disputes and unverified evidence
prevent a fully-credited result. A globally reserved transfer identity cannot
belong to two invoices or clients. This is manual evidence, not payment processing
or work-start authority; no bank connection or activation mapping is added.

Schemas v1 (identity), v2 (sessions), v3 (contacts), v4 (requests), v5 (triage index),
v6 (draft proposals), v7 (explicit sharing), v8 (contact confirmation), v9 (exact consent)
v10 (external invoice records) and v11 (bank-credit evidence)
are embedded and transactionally serialized with a contiguous version/checksum
ledger. The credit migration preserves the original v1–v10 bytes.
Unknown, missing-prefix or changed versions fail instead of deleting records. Before
deployment, use a separate migration role and give the runtime only the explicit
table DML it needs, not ownership/DDL, superuser or BYPASSRLS. This library does
not implement database RLS. Contact handlers scope their actual queries in addition
to checking current membership; later handlers must preserve the same boundary.

## Build and run the combined package

From a clean repository root, with Git, Bash, Node 24/npm 11, exact Go 1.27.2 and a fresh
`runtime-dist` output directory:

```sh
npm ci
bash scripts/package-runtime.sh --release
node scripts/runtime-smoke.test.mjs runtime-dist
set -- runtime-dist/package.*/
if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  printf '%s\n' 'Expected exactly one generated runtime package.' >&2
  exit 1
fi
PACKAGE_DIR="${1%/}"
(
  cd "$PACKAGE_DIR" || exit 1
  ./business-site --version
  ./business-site --listen 127.0.0.1:8080
)
```

The version command prints the source/public-digest receipt. Visit
`http://127.0.0.1:8080/` for the public snapshot; stop the local server with Ctrl+C.
The subshell leaves your original shell at the repository root.

The readiness line retains its `runtime ready;` prefix and reports the bound
listener as `listen=<address>`. For disposable process tests, use
`--listen 127.0.0.1:0` and read that actual endpoint instead of releasing a reserved
port before startup. This endpoint is operational readiness, not a portal or
production-deployment receipt. The compiled staging fixture bounds startup at
45 seconds, accepts only a numeric loopback address with a nonzero port, and
reports closed diagnostic categories rather than child output. Its private
512-byte error capture is concurrency-safe; unknown or oversized output never
appears in test diagnostics. One reaper owns child shutdown, including refusals
that happen before readiness.

Use a fresh output directory for smoke tests. `--release` requires clean source;
`--development` permits a labelled local build. Each directory contains the actual
Go executable, its digest-bound public snapshot, upstream Go notices and a build
receipt. An ordinary unconfigured `go build` cannot start a distributable server.
No package receipt proves deployment. CI builds and exercises the actual package
unconditionally alongside the existing identity checks. It also extracts and runs
`runtime-dist/business-site-runtime.tar.gz`, then uploads that permission-preserving
archive as the review artifact, not a production backend. After downloading the
artifact, extract the tar archive before running the executable; no manual `chmod`
is required. Pages publication is unchanged and uploads only `dist`.

Packaging copies the selected Go distribution's license and patent notice. Official
distributions keep both under `GOROOT`; Homebrew keeps `LICENSE` at the same-version
formula prefix and `PATENTS` under `libexec`. The resolver checks the compiler against
the distribution's `VERSION` and confines that fallback to its matching formula, not
another installed Go. Missing or empty notices still stop packaging. Run
`bash scripts/package-go-notices.test.sh` for the supported layouts and refusal controls.

With no `PORTAL_*` dependencies, startup serves public pages without broker/store
connections and reserves private routes as non-cacheable 404s. For later private
wiring, supply **all** `PORTAL_DATABASE_URL` (verified TLS, no insecure fallback),
`PORTAL_OIDC_ISSUER`, `PORTAL_OIDC_CLIENT_ID`, fixed HTTPS `PORTAL_CALLBACK_URL`
ending `/portal/callback`, and exactly one absolute `PORTAL_CLIENT_SECRET_FILE` or
`PORTAL_CLIENT_SECRET_PROJECTION_FILE`. The ordinary file input requires owner-only
permissions. The explicit projection input supports a non-root Unix process and
a root/current-user-owned regular file with read access for its primary group only;
writable groups, other-user access, execution/special bits and escaping links are
refused. Both readers bound the same-descriptor read to 16 KiB, remove only one
terminal LF/CRLF, and create no scratch copy. Mixed inputs fail before connections.
For the restricted image, project mode `0440` with group 65532 and searchable
directories. Configuration is snapshotted at startup; restart to rotate it.
Store the secret privately; never put it in arguments,
public assets, build receipts or logs. Partial/unsafe configuration fails generically.
The Unix service runtime requires an already migrated least-privilege store. Its
configured database login must match both the session and effective role before
the existing permission check and broker discovery; switching a broader login
into a restricted role is refused. It fixes its SQL schema, caps its pool/timeouts
and performs no DDL. Dependency configuration alone does not
turn on the OpenFeature gate; real federation/ingress/activation remain parent work.
This sign-in transport does not change the separate migration credential reader.
`/healthz` is public-runtime liveness only. SIGTERM drains requests for up to ten
seconds; request/header/idle limits are explicit. Serve behind an eventual verified
TLS gateway before any production identity activation. No forwarded-header trust
or plain-HTTP cookie exception is added.

See [the package decision](../docs/adr/0004-combined-runtime-package.md).

### Separate schema migration job

The same verified executable offers an explicit one-shot `--migrate` mode. It
uses a private verified-TLS connection file and a separate, constrained migration
role; ordinary web startup never reads that file or performs DDL. The job has
bounded connection, statement, lock and overall deadlines, prints a generic
completion or failure, and exits without opening HTTP or contacting the broker.
See [the migration procedure](MIGRATIONS.md) for prerequisites, invocation,
permissions and failure handling. No migration enables a portal operation or
establishes a production deployment.

### Bounded identity staging

With the complete private identity configuration above, an authorized disposable
staging process may additionally supply `PORTAL_IDENTITY_FLAG` in the maintained
OpenFeature from-env provider's JSON format and `PORTAL_IDENTITY_LEASE_UNTIL`
as an RFC3339 timestamp in the future, at most ten minutes away. Both belong to
the operator's private process configuration, never public assets, images,
receipts or logs. No provider service, direct Google/Microsoft SDK or paid
enrollment is introduced. This does not authorize using real customer data.

The provider evaluates only `portal-identity`. Missing, false, malformed,
wrong-type or oversized values deny; unhealthy SDK state, process cancellation,
shutdown and lease expiry also deny, including existing sessions. The
`portal-requests`, `portal-triage` and `portal-proposals` decisions remain
separate and closed in this composition. A valid identity lease cannot submit a
request, approve an offer, confirm funds or execute work.

Configuration is owned by one process. Changing an external environment file
does **not** change that process: stop and restart it to rotate or revoke the
configuration. The lease cannot be extended in place. Expiry closes private
access while the verified public pages continue serving. Incomplete or unsafe
private dependencies and missing or invalid leases fail generically before a
listener opens. Invalid flag values deny private access during evaluation while
the public pages remain available.

The required Go suite runs the compiled command against a disposable restricted
database and synthetic TLS broker, covering actual on/off/error HTTP routes and
expiry. Separate real TLS-browser/session controls use the same maintained
provider and verify invitation redemption, current membership and revocation.
These are synthetic staging proofs, not live Dex/Google/Microsoft registration,
production ingress, customer isolation or a legal-compliance verdict. Keep
monorepo#3936/#3939, website #19 and portal #21 open for their remaining release
proofs. See [the staging decision](../docs/adr/0009-bounded-identity-staging.md).

### Production public-content template

Use `bash scripts/package-runtime.sh --release --production-template --svelte` to build
`runtime-production-dist`. This explicit profile refreshes the complete public
portfolio into build-local scratch and includes EN/DA company information, but
**never embeds the registered address**. Missing or invalid ranking data fails;
ordinary default builds remain network-free. `--development` labels local source.

Before starting this variant, supply `--company-disclosure-file` with an absolute
path to a regular, owner-readable private projected file containing the approved
two-line address. The same-descriptor read is bounded and root-confined. Missing,
unsafe or malformed input fails before opening a listener. The default profile
does not accept or read this input. No actual address belongs in package source,
build environment, image layers, logs or receipts.

For a root-owned, group-readable atomic volume projection, use the separate
`--company-disclosure-projection <absolute-path>` instead. This mode requires a
non-root process and a regular file owned by root or the process user. Any group
read permission must match the process's primary group; group write, other-user
access, executable bits and special modes are refused. For the restricted image,
project mode `0440` with group 65532 and searchable directories; the leaf and
its symlink targets must stay within the supplied parent directory. Reads are
bounded to 16 KiB on the same descriptor and create no scratch file. The approved
two-line content is retained only in the immutable in-memory serving snapshot.
Mixed or duplicate input options and the ordinary default profile reject this mode.

This is permanent, explicit company-content configuration, not a portal release
flag or an identity/migration-secret transport. The original owner-only reader
remains unchanged for `--company-disclosure-file` and other private credentials.
Synthetic image tests cover projection ownership and permissions; actual
Kubernetes volume wiring/controller proof remains platform#4854. Restart to rotate
the snapshot rather than claiming live rereads of a rotated volume.

`--version` reports `PublicProfile` and the signed template's `PublicDigest`.
`--check-serving-public --public <packaged-public-directory>` with the same
projection validates the effective immutable serving snapshot and reports its
separate `EffectivePublicDigest`, never the address or filename. This is a
deployment configuration check, not provider/portal activation or compliance
certification. Restart deliberately to rotate the projected disclosure.

The same runtime/container smoke scripts support both profiles. Their production
controls use fictional private input outside the build context; image controls
exercise both an owner-only volume and a root-owned, matching-group atomic
projection in disposable, read-only volumes. CI preserves the
default checks, additionally tests and archives the production variant, and signs
that exact production-template image with source, digest and profile annotations.
See [the profile decision](../docs/adr/0008-production-public-content-profile.md).

## Restricted container distribution

After packaging clean source on Linux, return to the repository root with Docker
available. Build using **only the generated package directory as context**, not
this source checkout. Missing or multiple packages stop rather than choosing one
arbitrarily:

```sh
set -- runtime-dist/package.*/
if [ "$#" -ne 1 ] || [ ! -d "$1" ]; then
  printf '%s\n' 'Expected exactly one generated runtime package.' >&2
  exit 1
fi
PACKAGE_DIR="${1%/}"
docker build --file Dockerfile --label "org.opencontainers.image.source=https://github.com/devantler-tech/business-site" --label "org.opencontainers.image.revision=$(git rev-parse HEAD)" --tag business-site:test "$PACKAGE_DIR"
node scripts/container-smoke.test.mjs business-site:test runtime-dist
```

The digest-pinned, officially signed distroless base supplies CA roots and its
upstream notices; the application adds its linked Go notices. It has no shell,
compiler, test fixtures or source checkout. User/group 65532 serves port 8080;
run with a read-only root, all capabilities dropped and no new privileges.
Use HTTP GET `/healthz` for liveness/readiness (it proves the public runtime only).
The actual-image tests retain EN/DA GET/HEAD bytes, closed private/traversal paths,
network-disconnected startup, tamper rejection and bounded SIGTERM shutdown.

CI runs its image publisher only on main pushes after all required checks
and actual-image tests succeed, without a privileged cross-run trigger or rebuild.
Its 90-day `business-site-container-publication` artifact
contains `receipt.json`, the runtime receipt, verified signature and signed SLSA
v0.2 provenance. Consume `receipt.image` (the **full registry digest**) rather
than a tag. Verify its exact signer identity and GitHub issuer using Cosign, plus
the source/public-digest annotations and attestation source/subject binding.
The expected caller identity is
`https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main`.
Run the same container smoke test against that digest and its matching CI runtime
archive. Registry push, signing and post-publication testing must all succeed;
a staging tag alone is not release proof.

If publication fails, keep Pages unchanged and diagnose the specific run; do
not deploy its unverified staging tag or rebuild an artifact under an old receipt.
A CI rerun on the same main source can retry distribution of its newly tested
artifact through the same gates. Roll back a future deployment only to an earlier
verified immutable digest, never by rewriting a receipt or moving a trusted tag.
No cluster, domain, identity or payment setting is changed by this publisher.
See [the distribution decision](../docs/adr/0005-signed-runtime-container.md).

## Actual database tests

Use Go 1.27.2, the `psql` client and a **fresh disposable** PostgreSQL 17 service matching
the [CI fixture](../.github/workflows/ci.yaml): its synthetic owner is `portal_fixture_owner`
with the test-only password `fixture-only`, allowed to create the fixture roles and databases.
The constructor test uses that exact owner too; an arbitrary privileged account is insufficient.
Never use a production service or an existing database containing real data.

The examples assume your owned disposable service is at `localhost:5432`. Confirm the port
belongs to that fixture, not another service. Tests accept only `localhost:5432`,
`127.0.0.1:5432` or `127.0.0.1:55444`; for the last option, change all three fixture URLs below
to that exact host and port. Do not relax the test's endpoint guard. From the repository root:

```sh
cd server
export FIXTURE_OWNER_URL='postgres://portal_fixture_owner:fixture-only@localhost:5432/postgres?sslmode=disable'
psql "$FIXTURE_OWNER_URL" -v ON_ERROR_STOP=1 -f testdata/provision.sql
export PORTAL_TEST_MIGRATION_DATABASE_URL='postgres://portal_migrator:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'
export PORTAL_TEST_DATABASE_URL='postgres://portal_runtime:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'
GOTOOLCHAIN=local go mod verify
GOTOOLCHAIN=local go vet ./...
bash ../scripts/check-portal-quality.sh
GOTOOLCHAIN=local go test -race -cover ./... -count=1 -timeout=90s
GOTOOLCHAIN=local go test -race -tags=contacttrialintegration ./internal/identity -run '^TestContactTrialHost' -count=1 -timeout=90s
GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

The Go database suite and explicit trial-host integration do not require Node.
The real-process integration is selected separately so it retains its complete
refusal/journey controls without charging startup to the ordinary package's
existing 90-second budget. Source CI requires both, with no conditional or error-tolerant
step. Separately, use Node 24 from the
repository root to run `node --test scripts/portal-request-limits.test.mjs` against
the actual served browser asset. The required source CI job explicitly sets up
Node 24 and runs all six controls unconditionally; its runtime contract rejects
a missing, conditional or error-tolerant browser check. Moving these controls to
their own runtime does not make them optional.

The constructor test also creates and drops its own uniquely named fixture database,
uses verified TLS through a synthetic loopback database gateway and discovers a
synthetic TLS broker; complete dependencies must still leave every private route closed.
It does not migrate through the runtime role or activate real providers.

The optional complete Go quality launcher verifies Go 1.27.2 and golangci-lint 2.14.0
built with Go 1.27.2 before running the entire module. It fails if version/build
metadata is missing or differs, never installs tools, and preserves linter failures.
`--no-config` prevents ambient machine configuration from suppressing diagnostics.
The two zero-valued display limits show every diagnostic; they disable no rule.
Line-level deduplication is also disabled, so findings sharing a line remain visible.
No linter configuration or suppression is required. Fixture response, connection
and transaction cleanup failures fail their tests. Production read-only descriptor
cleanup and rollback after a completed transaction preserve the operation's verdict;
pgx discards failed rollback connections before releasing them to the pool.

These URLs are test-only examples, not production configuration. Tests require
the exact fixture database and role names; missing configuration fails. They
grant runtime table DML within the fixture, exercise the restricted role, and
truncate the thirteen fixture identity/session/contact/request/proposal/visibility/consent/invoice/credit tables between tests. Never reuse a database
with real data. CI provisions its own immutable-image PostgreSQL service and
aggregates this unconditional job into `CI - Required Checks`.

After testing, stop and remove only the disposable service and data directory you created.
Keep shared services and databases untouched; do not turn this fixture into a permanent service.

The [private frontend comparison protocol](PRIVATE-FRONTEND-COMPARISON.md) defines
the opt-in, real Go contact-journey benchmark. It uses these same disposable
database and synthetic TLS broker fixtures, separately from the correctness suite.
Its raw measurements are a comparator, not evidence that a Svelte private renderer
or a production portal is ready.

The explicit [trial host](CONTACT-TRIAL-HOST.md) launches that maintained authority
as a separate developer-only process over a retained loopback TLS listener and a
restricted disposable database. It is not the complete paired comparison runner;
its process restart controls do not decide the private frontend or activate it.

The [private contact experiment decision](../docs/adr/0014-private-contact-renderer-experiment.md)
freezes the next candidate's presentation boundary, process limits, comparison
method and same-record recovery check. Its
[comparator amendment](../docs/adr/0015-private-contact-comparator-amendment.md)
makes both sides run the current shared Go contact authority. The developer renderer and actual HTTP
composition are available through [their explicit test entrypoint](CONTACT-RENDERER.md#exercise);
the complete packaged paired measurements and recovery drill remain prerequisites,
not delivered results or permission to activate the portal.

For manual visual checks, build tag `portalvisual` exposes a bounded ten-minute
loopback-only synthetic fixture. Run it separately from database tests: they share
and reset the disposable records. Its HTTP gateway maps fixture cookies and origin
to the actual TLS application and broker; it is presentation/interaction evidence,
not production TLS, cookie, federation or ingress evidence. Ordinary untagged tests
exercise the real HTTPS boundary directly. No browser security warning is bypassed.

Review locked dependencies and their upstream licenses before every update.
The current compiled-module inventory is in [DEPENDENCIES.md](DEPENDENCIES.md).
The source repository has not introduced a new license grant for its own code.
Dependency notices do not license Devantler Tech's application source. Include
the required upstream license/notice texts when distributing a binary; the runtime
packager carries the actually linked Go graph and Go standard-library notices.
