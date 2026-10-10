# Portal identity and browser sign-in

First implementation child: [business-site#33](https://github.com/devantler-tech/business-site/issues/33)
of [monorepo#3936](https://github.com/devantler-tech/monorepo/issues/3936).

This is a Go **library with HTTP handlers**, not a deployed portal. It discovers one configured HTTPS
OIDC broker through maintained go-oidc, verifies signed ID tokens and the attempt
nonce, and uses PostgreSQL for browser-bound login attempts, bound invitations
and current client/operator membership. Accounts use issuer + subject, not email.
Synthetic tests use a local TLS broker with generated keys; they are not proof of
live Dex, Google or Microsoft federation.

The foundation evaluates the boolean OpenFeature flag `portal-identity` with a
false default on every operation, using the application's injected SDK client.
Missing client/flag, false, a wrong type or evaluation error all deny access;
missing broker/store also denies it. Changing the provider to false closes the
boundary without creating a new service. No production provider is wired here.
No environment variable currently activates an HTTP route. Public Pages builds
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

The parent remains open for production broker/provider configuration, listener and
ingress deployment, contact verification, retention/expired-record cleanup and
deployed two-client/operator proof. `Browser` requires a live least-privilege store,
a configured broker and a private client secret; construction neither migrates
the database nor enables the injected flag. There is no production identity endpoint,
customer intake, payment, queue, MCP or agent execution. No paid service is used.

Schemas v1 (identity) and v2 (sessions) are embedded and transactionally serialized
with a contiguous version/checksum ledger. The original v1 checksum is unchanged.
Unknown, missing-prefix or changed versions fail instead of deleting records. Before
deployment, use a separate migration role and give the runtime only the explicit
table DML it needs, not ownership/DDL, superuser or BYPASSRLS. This library does
not implement database RLS or contact-record queries; future handlers must scope
their queries in addition to checking current membership.

## Actual database tests

Use Go 1.27.2 and a **fresh disposable** PostgreSQL 17 database. Run the fixture
provisioning script as its test owner. It creates only fictional roles/records:

```sh
psql "$FIXTURE_OWNER_URL" -v ON_ERROR_STOP=1 -f testdata/provision.sql
export PORTAL_TEST_MIGRATION_DATABASE_URL='postgres://portal_migrator:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'
export PORTAL_TEST_DATABASE_URL='postgres://portal_runtime:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'
GOTOOLCHAIN=local go mod verify
GOTOOLCHAIN=local go vet ./...
GOTOOLCHAIN=local go test -race -cover ./... -count=1 -timeout=90s
GOTOOLCHAIN=local go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

These URLs are test-only examples, not production configuration. Tests require
the exact fixture database and role names; missing configuration fails. They
grant runtime table DML within the fixture, exercise the restricted role, and
truncate the four fixture identity/session tables between tests. Never reuse a database
with real data. CI provisions its own immutable-image PostgreSQL service and
aggregates this unconditional job into `CI - Required Checks`.

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
the required upstream license/notice texts when distributing a future binary.
