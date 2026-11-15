# Public runtime package

This directory prepares the public website for the platform's namespace-bound
WebApp interface. The source-owned runtime publisher can publish this package after
explicit release admission; no workflow applies it to a cluster. The platform serves
this package at devantler.tech since GitHub Pages was retired; portal access stays disabled.

From the repository root, with Node 24, installed root dependencies, Kubectl and
Docker available:

```sh
kubectl kustomize deploy | node scripts/check-tenant-package.mjs
node --test scripts/tenant-package.test.mjs
node scripts/tenant-runtime-smoke.test.mjs
```

The last command exercises the committed immutable image. Source CI passes its
newly tested production image as an explicit argument, retaining the rendered
arguments and runtime restrictions. Neither mode verifies a registry signature;
that remains mandatory in the signed publisher's independent receipt check.

The package uses two replicas (the validator permits two or three), a
half-core/256 MiB limit and the real `/healthz` probe, plus an application-owned
disruption budget allowing one unavailable Pod.
These are provisional bounds, not load, availability, actual drain or recovery evidence.
Only gateway ingress to the application port is opened. The only outbound rule
allows HTTPS (TCP 443) to exactly `api.numina.app` and `api.stripe.com`, for
the portal's default-off billing (below); its presence keeps every other egress
default-denied. Cilium matches these names only after its DNS proxy has seen the
lookup, which the platform's `business-site-dns-visibility` policy provides
(devantler-tech/platform#6337); a tenant policy may not name kube-system itself.
Other platform policies remain additive, so this is not an overriding deny
of every platform-granted connection. No database, migration, broker or private
activation setting is configured. The checker accepts only the committed,
independently verified baseline image; a same-repository digest alone is insufficient.

The application-owned namespaced `openbao` SecretStore authenticates as only the
`business-site` service account, using the dedicated `business-site` role. Its
ExternalSecret reads only `company-disclosure` from the planned
`apps/business-site/public-disclosure` KV-v2 entry and owns the same-namespace
`business-site-disclosure` Secret. The WebApp projects that selected key read-only
at the fixed mount with mode 0440 and the application's primary group. No shared
ClusterSecretStore, bulk import, private-value writer or inline value is admitted.

These declarations neither provision the Platform authentication role nor create
the private value. Before registration, Platform must separately bind that role
to this exact service account and namespace, with read permission limited to
exact entries, including the disclosure entry and the two billing entries below.
The owner must materialize the confirmed disclosure privately through the
supported non-printing store mechanism. A missing or unsafe required
file must prevent serving; a fictional placeholder is not a production input.
The runtime snapshots the file at startup, so disclosure rotation requires a
reviewed restart. Controller synchronization and real projection/rotation behavior
remain separate live acceptance. No disclosure value belongs in this repository,
image or publication receipt. Tests use fictional values in disposable volumes.

The WebApp uses exactly `business-site.platform.devantler.tech`, where the public pages are
checked before a DNS switch. Two routes add the public names on their own gateway listeners:
`http-route-public.yaml` for `devantler.tech` and `www.devantler.tech`, and
`http-route-portal.yaml` for `portal.devantler.tech`. The runtime's `--public-host` and
`--portal-host` arguments route by host, so nothing is served twice: `portal.devantler.tech`
opens the portal at `/` and redirects public pages to devantler.tech, `www` redirects to the
apex, and every other host serves the public pages and redirects `/portal…` to
portal.devantler.tech ([ADR 0020](../docs/adr/0020-public-staging-hostname.md), amended
2026-11-13). The validator admits exactly these names and routes and refuses wildcards,
lookalikes and other tenants' names. Platform admits the hostnames, listeners and certificates
separately (devantler-tech/platform#6177). Do not apply this directory to an ambient cluster.

Issue #102 retains actual platform consumption proof and each adopted signed pair.
The input declarations tracked by #300 require their own newly verified signed
image/manifest publication before consumption; the earlier v1.0.2 pair does not
contain these declarations and cannot be relabeled as doing so.
See [the original source-contract decision](../docs/adr/0019-public-tenant-package.md)
and [the staging-hostname decision](../docs/adr/0020-public-staging-hostname.md).

## Portal settings and invitations

The WebApp has no environment variables, so the portal reads its settings from
one projected file selected with `--portal-settings-projection
/run/webapp-inputs/portal-settings`. The file uses the same confinement as the
disclosure projection (root- or process-owned, mode 0440 with the application's
primary group, at most 16 KiB). These manifests do not add it yet; the
platform wires it with the portal database and broker egress.

Lines are `KEY=value`, separated by LF. Blank lines and lines starting with `#`
are ignored; a file with no keys keeps the portal as it is today: no portal database or
broker connection and sign-in off. The accepted keys keep their existing meaning
(the billing keys are under [Portal billing inputs](#portal-billing-inputs)):

| Key                                    | Value                                                   |
|----------------------------------------|---------------------------------------------------------|
| `PORTAL_DATABASE_URL`                  | verified-TLS `postgres://` URI for `portal_web` (below) |
| `PORTAL_OIDC_ISSUER`                   | `https://login.devantler.tech`                          |
| `PORTAL_OIDC_CLIENT_ID`                | `business-portal`                                       |
| `PORTAL_CALLBACK_URL`                  | `https://portal.devantler.tech/portal/callback`         |
| `PORTAL_CLIENT_SECRET_PROJECTION_FILE` | `/run/webapp-inputs/portal-client-secret`               |
| `PORTAL_IDENTITY_PROFILE`              | `production`, only when sign-in is switched on          |
| `PORTAL_IDENTITY_FLAG`                 | provider JSON for `portal-identity`, with the profile   |

The database URI names host `business-site-portal-db-rw.business-site.svc`,
port 5432, database `portal` and login `portal_web` with its password, and the query
`sslmode=verify-full&sslrootcert=/run/webapp-inputs/portal-database-ca`.

The five dependency keys come all together or not at all. With them but no
activation keys, startup checks the database login and grants and discovers
Dex, but sign-in stays off. Unknown or repeated keys, empty values, surrounding
whitespace and CR line endings are refused. The staging lease and the owner-only
secret file stay environment-only, and no portal variable may also be inherited
from the environment. Errors never include values or paths. Restart to rotate.

Run the schema job before serving, with migration login `portal_migrator`
(which owns database `portal`) and `--runtime-role portal_web`; see
[MIGRATIONS.md](../server/MIGRATIONS.md#run-the-explicit-job). With sign-in on,
invite customers from a web pod as
[CUSTOMER-IDENTITY.md](../server/CUSTOMER-IDENTITY.md#invite-a-customer) describes.

## Portal billing inputs

The portal invoices through Numina and takes cards through Stripe
([BILLING.md](../server/BILLING.md)). Both paths stay off behind the
false-default `portal-invoice-billing` and `portal-card-billing` flags, which
have no environment mapping. Two ExternalSecrets read their keys through the
same namespaced `openbao` store:

| ExternalSecret and Secret | OpenBao KV-v2 entry         | Keys                        |
|---------------------------|-----------------------------|-----------------------------|
| `business-site-numina`    | `apps/business-site/numina` | `api-key`                   |
| `business-site-stripe`    | `apps/business-site/stripe` | `api-key`, `webhook-secret` |

The maintainer stores both values; nothing here creates them. The platform's
`business-site` role reads exactly these two entries, with no metadata, list or
write access (devantler-tech/platform#6337). An ExternalSecret that cannot sync
keeps the tenant's Flux reconciliation from becoming healthy. Adopt a release
containing these files only after the role change is deployed and both entries
exist.

The WebApp does not project these Secrets yet. The platform's WebApp admits one
Secret projection and mounts it with `optional: false`. It refuses a second
projected Secret, and a missing projected key keeps every new Pod from
starting, including the public pages. When the one projected Secret also
carries these keys, add these items to it:

| Secret key              | File under `/run/webapp-inputs/` |
|-------------------------|----------------------------------|
| Numina `api-key`        | `numina-api-key`                 |
| Stripe `api-key`        | `stripe-api-key`                 |
| Stripe `webhook-secret` | `stripe-webhook-secret`          |

Each file uses the disclosure projection's confinement: a nonempty regular
file, root- or process-owned, mode 0440 with the application's primary group,
at most 16 KiB. One trailing LF or CRLF is removed. The runtime reads a file
only when the portal settings name it, so until then nothing reads these keys.

To configure a path, add its lines to the portal settings file. The runtime
refuses billing lines unless the five dependency keys above are present:

```text
PORTAL_NUMINA_TOKEN_FILE=/run/webapp-inputs/numina-api-key
PORTAL_STRIPE_KEY_FILE=/run/webapp-inputs/stripe-api-key
PORTAL_STRIPE_WEBHOOK_SECRET_FILE=/run/webapp-inputs/stripe-webhook-secret
```

The Numina line alone starts the invoice sync, which invoices nothing while its
flag is off. The two Stripe lines come together or not at all. Omit the
optional settings to keep their defaults:

| Key                           | Default                  |
|-------------------------------|--------------------------|
| `PORTAL_NUMINA_API_URL`       | `https://api.numina.app` |
| `PORTAL_NUMINA_SALES_ACCOUNT` | `1000`                   |
| `PORTAL_NUMINA_VAT_CODE`      | `U25`                    |
| `PORTAL_STRIPE_API_URL`       | `https://api.stripe.com` |

The Numina settings other than the key file are refused without it. Another API
origin also needs its own egress rule. The flags still decide whether anything
is invoiced or charged.

## Offline publication receipt admission

With the exact Go compiler from `server/go.mod`, run this from `server/` against
an unextracted publication ZIP downloaded from a separately verified successful
main-push CI run. Supply that run's source, current original attempt and artifact
metadata; do not derive the expected values from the archive itself.

```sh
go run ./cmd/runtime-receipt --archive /private/tmp/publication.zip \
  --sha256 "$EXPECTED_ARCHIVE_SHA256" --source "$EXPECTED_SOURCE_SHA" \
  --run "$EXPECTED_CI_RUN_ID" --attempt "$EXPECTED_CI_ATTEMPT" \
  --artifact "$EXPECTED_ARTIFACT_ID"
go test -race ./cmd/runtime-receipt -count=1 -timeout=45s
```

The explicit command makes no network request or filesystem write. It emits the
eleven-field receipt only after verifying the archive digest, exact five regular
JSON members, complete streams and source/run/attempt/profile/signer bindings.
The Cosign `attestation.json` member may contain multiple JSON records, as a
successful retry can verify earlier attestations for the same image digest. Every
record must be valid, the stream must be nonempty, and all other members still
require exactly one JSON value. This does not replace cryptographic verification.
It reads one descriptor into a bounded snapshot, never extracts paths, and rejects
symlinks, pipes, ambiguous receipt keys and unsafe numeric IDs. Compressed input
is limited to 4 MiB, each expanded member to 2 MiB, total expanded content to
8 MiB and the receipt to 16 KiB. Refusals emit no receipt or inspected values.

Artifact ID syntax is validated, not its ownership: the caller must bind the ID,
digest and original current successful CI attempt through GitHub's API before
downloading. Use non-extracting artifact download with digest errors enabled.
Archive admission does not verify signatures, provenance, current GitHub state,
release identity or image/manifest publication. The accepted immutable publisher
verifier and independent paired-artifact readback remain mandatory under #276.

## Source-owned runtime release

The release and runtime-publisher callers remain **off** unless the repository variable
`RUNTIME_PUBLICATION_ENABLED` is exactly `true`. They do not select a platform
tenant, move the public hostname, enable portal identity or accept customer orders.

`release-runtime.yaml` admits a complete successful original main-push CI attempt whose source
equals its frozen caller commit. It independently reads current API metadata, downloads only the
bounded publication ZIP without extraction and executes the source-owned receipt reader. The
accepted immutable shared verifier then authenticates the original image signature and complete
provenance against a temporary deployment copy; source manifests are never edited by admission.
Only then can the shared release tool obtain its existing GitHub App token. The caller forwards
only `APP_PRIVATE_KEY`, disables issue side effects and uses the main-only GitHub release
configuration; it does not publish an npm package.

After the source has merged and its main CI has succeeded, the explicit nonpublishing preflight is:

```sh
gh workflow run release-runtime.yaml --ref main -f mode=dry-run
```

That path is allowed while publication is off. It uses the real workflow-context App credentials
and release decision, but creates no tag, release or registry publication. An offline fixture or
repository-secret inventory is not evidence that effective inherited credentials work. Keep
publication off until the current-source preflight and review have succeeded.

An explicit enabled `mode=release` dispatch on main can create a genuine semantic-version release
through the accepted shared release tool. Release initiation is manual: no privileged completion
trigger or release dispatch is added to source CI. Both admission jobs disable Go-cache reuse.
One `release: published`
caller then rebinds the actual stable tag to its source and current original CI receipt. It calls
only the consumer-accepted immutable prebuilt-WebApp publisher, with caller pinning and signed
promotion required, rebuilding and recovery disabled. The release-only tool uses its maintained
Node 22/npm 11 environment; the application still builds and validates under Node 24/npm 11.

The final read-only job verifies both immutable subjects and both version aliases under the exact
accepted publisher identity and paired source/version/run/attempt/original-CI/public-digest claims.
It compares the actual manifest package with the source YAML, allowing only the separately verified
image pin, and rechecks current CI/publication attempts. The verifier creates an exclusive extraction
directory for Flux before pulling the authenticated immutable package. A failed or partial pull
never emits a publication receipt. Before and after those registry reads,
it also requires the release to remain published and its lightweight or bounded annotated tag to
resolve to the verified source. Its public
`business-site-runtime-publication` artifact is emitted only after verification succeeds.
An accepted dispatch, a GitHub release or a successful reusable job alone is not a verified pair.
Version tags are published sequentially, not atomically; the queue is not a guarantee that deliberate
older reruns cannot affect latest. A partial publication remains a failure, never permission to
enable recovery or bypass an occupied version.

Issue #276 is complete: release v1.0.1 from source `f7212b4120446d95079b768e78f4194a8597a6f7`
published an independently verified signed image/manifest pair. That release still contains the
old inert hostname; the staging-hostname change requires a subsequent verified publication before
consumption. Its receipt is recorded on #102, not inferred from this source directory.
Issue #102 retains platform registration, rollback, live acceptance and the separate private-portal
requirements. No deployed portal or new credential scope is implied by this source configuration.
