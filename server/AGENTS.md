# Portal server maintenance

The parent repository's AGENTS.md and reviewed shared engineering contract apply.
This Go module is the unified product's **default-off browser identity boundary**,
not a deployed portal. It provides sign-in/callback/shell/logout and private contact
profile handlers and separately default-off free project-request intake; no order,
payment or agent endpoint exists. Read `REQUESTS.md` for the request boundary. Contact emails remain
self-declared and unverified, never identity or order authority. Never present synthetic broker
tests or the loopback visual gateway as Google/Microsoft login or production
ingress/cookie proof.

Use the exact Go version in `go.mod` with `GOTOOLCHAIN=local`; run `gofmt`,
`go mod verify`, `go vet ./...`, `go test -race ./... -count=1 -timeout=90s` and
the pinned `govulncheck` command in CI. Database tests require BOTH fixture URLs
documented in README.md and fail rather than skip if absent. Never point them at
a real customer database; they refuse non-fixture database/role names and truncate
only the fixture's six identity/session/contact/request tables. Provision a fresh disposable PostgreSQL
17 service with `testdata/provision.sql`; never start a permanent service for tests.
The tracked module-local `.go-test-fixture.json` also opts input-free organization
validation into its closed disposable PostgreSQL fixture. Keep its namespace and
database aligned with these test-only roles; never add endpoints or credentials.
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
Lychee's test-only preprocessor renders all four actual private templates in EN/DA.
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
The credential scanner retains its complete recommended preset. Only five fully
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
Free project requests additionally require false-default `portal-requests` on every
operation. A receipt never authorizes a quote, agreement, payment or worker execution.
The request flag's lifecycle is tracked in business-site#57. Requests are immutable;
the runtime needs SELECT/INSERT only on that table, not UPDATE/DELETE or DDL.
Read-only operator review additionally requires false-default `portal-triage`
on every operation, plus current server-owned operator membership in its SQL.
It never grants client impersonation or paid-work approval. Keep keyset pagination
bounded and ordered by original submission time/ID with microsecond precision.
The triage flag's retirement review is business-site#62, due 12 November 2026.

Migrations run under a separate migration role; the runtime must not own schema
creation, the migration ledger, superuser or BYPASSRLS authority. Authorization
here uses explicit current membership, NOT a claim of database row-level security.
Customer-record handlers must authorize the server-owned record's client ID and
scope the data query; an application helper is not blanket database isolation.

Keep fixtures fictional. No tokens, login secrets, customer records or raw
database errors in public responses/logs. No server files under `public/` or the
Astro content collection. Static Pages publication remains source-owned and
uploads only `dist`; it does not deploy this module. Application code stays here,
never in the monorepo or a separate portal source repository.
