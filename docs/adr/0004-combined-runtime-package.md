# One runnable public-site and portal package

Status: accepted first implementation slice of [business-site #44](https://github.com/devantler-tech/business-site/issues/44).
Production deployment and Pages replacement remain gated by [monorepo #3939](https://github.com/devantler-tech/monorepo/issues/3939).

## Decision

Keep Astro's existing static output and the existing Go browser identity boundary.
Package them together, rather than rewrite the website or duplicate authentication.
One Go executable loads the package's validated public output into a bounded,
read-only snapshot and delegates canonical `/portal` routes to the real existing
browser handler before public-file lookup. Missing public paths return 404, not an
SPA fallback. Directories, links, private source, credentials and ambiguous paths
are refused. English, Danish and existing public assets retain their URLs.

`scripts/package-runtime.sh --release` requires a clean checkout and supported
toolchains, builds ordinary unflagged Astro output and packages the executable,
public output, linked Go dependency LICENSE/NOTICE/PATENTS texts and a build receipt.
The executable embeds the full checked-out source SHA, mode and public-byte digest;
startup rejects mismatching public output. `--development` truthfully labels a local
package built from a possibly dirty source tree; it is not immutable release proof.
`--version` describes the build, never a live deployment.

This is a build package and CI review artifact, not a container, release enrollment
or production deployment. The source-owned Pages publisher continues uploading
only static `dist` and remains the live website owner. No monorepo caller is added.
No backend configuration or notice is copied into public output.

## Private dependencies and safety

Absent identity configuration starts a public-only runtime without connecting to
a broker or PostgreSQL. Partial configuration fails generically. Complete private
configuration requires verified TLS to PostgreSQL, a fixed HTTPS callback and one
OIDC broker. A root-confined, bounded private secret-file read supports projected
secrets without following links outside the configured mount. Nonblocking open
rejects nonregular files without waiting for a pipe writer.

Startup caps the pool and queries the runtime role's actual privileges. It requires
all needed DML permissions, rejects schema/database creation and effective or
assumable privileged/ownership roles,
and never migrates. A separate migration job, schema recovery and restricted role
provisioning remain launch work; startup never acquires migration authority.

The executable installs the maintained OpenFeature no-op provider. Supplying
dependencies **does not activate the portal**. Existing handlers re-evaluate the
false-default `portal-identity` flag on every operation. A production provider
adapter, real federated login and activation proof are separate release work.
Source tests use the real handler with a synthetic broker and PostgreSQL in both
flag states, including closed/missing/invalid evaluation and session/CSRF/client
boundaries. They are not Google/Microsoft or deployed TLS proof.

HTTP header/request/idle limits and bounded graceful shutdown protect the listener.
`/healthz` proves only the loaded public runtime is serving, not broker/database
health or production readiness. Responses and startup diagnostics expose no raw
private dependency errors, request URLs, identities or credentials.

## Consequences and remaining gates

This closes the missing executable composition: the actual package can be built,
run and checked in CI. It does not close #3939, #3936, real-provider, recovery,
legal, cost or production-cutover gates. No customer intake, payments, queue/MCP,
agent execution, TLS gateway trust, OCI publication or cluster change is introduced.
Pages stays live until a replacement has actual source/artifact, deployed EN/DA,
real-provider/two-customer/operator and recovery evidence. No paid service is used.

References: [Astro static deployment](https://docs.astro.build/en/guides/deploy/),
[Go root-confined filesystem](https://pkg.go.dev/os#Root),
[OpenFeature Go SDK](https://openfeature.dev/docs/reference/technologies/server/go/).
