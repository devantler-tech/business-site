# Portal server maintenance

The parent repository's AGENTS.md and reviewed shared engineering contract apply.
This Go module is the unified product's **default-off browser identity boundary**,
not a deployed portal. It provides sign-in/callback/shell/logout handlers; no
customer form, payment or agent endpoint exists. Never present synthetic broker
tests or the loopback visual gateway as Google/Microsoft login or production
ingress/cookie proof.

Use the exact Go version in `go.mod` with `GOTOOLCHAIN=local`; run `gofmt`,
`go mod verify`, `go vet ./...`, `go test -race ./... -count=1 -timeout=90s` and
the pinned `govulncheck` command in CI. Database tests require BOTH fixture URLs
documented in README.md and fail rather than skip if absent. Never point them at
a real customer database; they refuse non-fixture database/role names and truncate
only the fixture's four identity/session tables. Provision a fresh disposable PostgreSQL
17 service with `testdata/provision.sql`; never start a permanent service for tests.

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
