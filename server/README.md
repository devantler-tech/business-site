# Portal identity and browser sign-in

This module supplies a Go executable composing the public Astro output with the
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
boundary without creating a new service. No production activation provider is wired
here; the executable installs the maintained SDK's no-op provider.
No environment variable activates a private route. Public Pages builds
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

`Invite` and `Revoke` remain **trusted control-plane operations**, not public
HTTP endpoints. An operator workspace does not expose these operations. Likewise,
`Complete` accepts only a server-obtained token, never a browser authentication claim.

Authenticated clients can edit their name, contact email and optional company at
`/portal/contact`. The actual query uses current server-owned issuer/subject membership
to select the client record; URL/form client IDs and operator overrides are not accepted.
CSRF/origin checks and bounded single-value fields protect saves. A revision comparison
rejects stale/concurrent updates instead of silently overwriting newer details.
The stored email is explicitly **unverified**, including after a change: it does not
link sign-in identities, authorize orders or verify agreements/payments. This slice
introduces no verification delivery provider, SMTP connection or verification endpoint.
Do not enable real contact collection before the parent's privacy/retention gates clear.

The parent remains open for production broker/provider configuration and
ingress deployment, contact verification, retention/expired-record cleanup and
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
obtain acceptance or authorize work.

Schemas v1 (identity), v2 (sessions), v3 (contacts), v4 (requests), v5 (triage index) and v6 (draft proposals)
are embedded and transactionally serialized with a contiguous version/checksum
ledger. Original v1–v5 bytes remain unchanged by the proposal migration.
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

With no `PORTAL_*` dependencies, startup serves public pages without broker/store
connections and reserves private routes as non-cacheable 404s. For later private
wiring, supply **all** `PORTAL_DATABASE_URL` (verified TLS, no insecure fallback),
`PORTAL_OIDC_ISSUER`, `PORTAL_OIDC_CLIENT_ID`, fixed HTTPS `PORTAL_CALLBACK_URL`
ending `/portal/callback`, and absolute `PORTAL_CLIENT_SECRET_FILE`. Store the secret
as a regular, private file or a confined projected secret; never put it in arguments,
public assets, build receipts or logs. Partial/unsafe configuration fails generically.
The Unix service runtime requires an already migrated least-privilege store, fixes its SQL schema,
caps its pool/timeouts and performs no DDL. Dependency configuration alone does not
turn on the OpenFeature gate; real provider/ingress/activation remain parent work.
`/healthz` is public-runtime liveness only. SIGTERM drains requests for up to ten
seconds; request/header/idle limits are explicit. Serve behind an eventual verified
TLS gateway before any production identity activation. No forwarded-header trust
or plain-HTTP cookie exception is added.

See [the package decision](../docs/adr/0004-combined-runtime-package.md).

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
GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

The Go database suite does not require Node. Separately, use Node 24 from the
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
truncate the seven fixture identity/session/contact/request/proposal tables between tests. Never reuse a database
with real data. CI provisions its own immutable-image PostgreSQL service and
aggregates this unconditional job into `CI - Required Checks`.

After testing, stop and remove only the disposable service and data directory you created.
Keep shared services and databases untouched; do not turn this fixture into a permanent service.

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
