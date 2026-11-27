# Public runtime package

This directory prepares the public website for the platform's namespace-bound
WebApp interface. The source-owned runtime publisher can publish this package after
explicit release admission. The platform applies it to serve the public site at
devantler.tech and the invitation-only portal at portal.devantler.tech.

The whole directory is published as one artifact and split into Kustomize overlays
([ADR 0016 in the monorepo](https://github.com/devantler-tech/monorepo/blob/main/docs/adr/0016-dev-and-prod-deployments.md)):

- `kustomization.yaml` is the prod entry the platform reconciles at the artifact root. It
  only includes `prod/`.
- `base/` holds the shared WebApp. The publisher pins its image in `base/webapp.yaml`.
- `prod/` holds the production routes, network policy, disruption budget, secret
  synchronization, migration Job and retention CronJob.
- `dev/` (#542) packages one portal replica and a migration Job for
  `business-site-dev`. Both use the release's pinned image. The platform supplies
  `business-site-dev-runtime` for the portal, `business-site-dev-migration` for
  schema changes, and the restricted database and sign-in client. The platform's
  authenticated proxy and network policies control access to `dev.devantler.tech`
  (devantler-tech/platform#6126).

Overlays reference only `../base` and their own files. `kubectl kustomize deploy/dev`
must render, and `deploy` must render exactly what `deploy/prod` renders.

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

CI renders the production overlay and checks its WebApp, migration Job,
retention CronJob, routes, SecretStore, ExternalSecrets and network policy with
the pinned Platform policies and Kyverno engine. The Tenant graph supplies the
namespace labels. Each protected resource has an explicit refusal control for
creation and updates.

The pinned Platform WebApp compiler also produces the Deployment, Service and
HTTPRoute from the same rendered application. The admission check requires
those three generated objects and exercises policy for their ownership, the
Deployment's pod security and the HTTPRoute's hostname.

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
of every platform-granted connection. The package itself sets no database, broker or
activation value; the migration Job below only names the platform's migration Secret. The checker accepts only the committed,
independently verified baseline image; a same-repository digest alone is insufficient.
That baseline predates the host and portal-settings arguments, so the smoke test
needs an image built from current source, as CI supplies.

The WebApp projects the one Secret the platform composes in this namespace,
`business-site-runtime`, read-only at `/run/webapp-inputs` with mode 0440 and the
application's primary group. It admits exactly seven items: `company-disclosure` as
`disclosure`, and `portal-settings`, `portal-client-secret`, `portal-database-ca`,
`smtp-username`, `smtp-password` and `portal-zitadel-token` at their own names. While sign-in is off,
`portal-settings` holds only a `#` comment line, so the portal reads none of the other
portal items. Serving needs both the disclosure and the settings file: without that
Secret or any projected item the Pods do not start, so the Secret must exist before
this package is applied.

The application-owned namespaced `openbao` SecretStore and its ExternalSecret stay
declared but are no longer projected; removing them is a later cleanup. The
SecretStore authenticates as only the `business-site` service account, using the
dedicated `business-site` role. Its ExternalSecret reads only `company-disclosure`
from the planned `apps/business-site/public-disclosure` KV-v2 entry and owns the
same-namespace `business-site-disclosure` Secret. No shared ClusterSecretStore,
bulk import, private-value writer or inline value is admitted.

The retained declarations neither provision the Platform authentication role nor create
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
`prod/http-route-public.yaml` for `devantler.tech` and `www.devantler.tech`, and
`prod/http-route-portal.yaml` for `portal.devantler.tech`. The runtime's `--public-host` and
`--portal-host` arguments route by host, so nothing is served twice: `portal.devantler.tech`
serves the portal at its root (`/`, `/sign-in`, …; an old `/portal…` address redirects 301
without the prefix) and redirects public pages to devantler.tech, `www` redirects to the
apex, and every other host serves the public pages and redirects `/portal…` to
the same path at the root of portal.devantler.tech ([ADR 0020](../docs/adr/0020-public-staging-hostname.md), amended
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
primary group, at most 16 KiB). The WebApp passes that argument and projects the
file from `business-site-runtime`. Switching sign-in on changes that Secret's
settings item, adds the platform's database and broker egress and restarts the
Pods; these manifests stay unchanged.

Lines are `KEY=value`, separated by LF. Blank lines and lines starting with `#`
are ignored; a file with no keys keeps the portal as it is today: no portal database or
broker connection and sign-in off. The accepted keys keep their existing meaning
(the billing keys are under [Portal billing inputs](#portal-billing-inputs)):

| Key                                    | Value                                                   |
|----------------------------------------|---------------------------------------------------------|
| `PORTAL_DATABASE_URL`                  | verified-TLS `postgres://` URI for `portal_web` (below) |
| `PORTAL_OIDC_ISSUER`                   | `https://login.devantler.tech`                          |
| `PORTAL_OIDC_CLIENT_ID`                | `business-portal`                                       |
| `PORTAL_CALLBACK_URL`                  | `https://portal.devantler.tech/callback`                |
| `PORTAL_CLIENT_SECRET_PROJECTION_FILE` | `/run/webapp-inputs/portal-client-secret`               |
| `PORTAL_IDENTITY_PROFILE`              | `production`, only when sign-in is switched on          |
| `PORTAL_IDENTITY_FLAG`                 | provider JSON for `portal-identity`, with the profile   |
| `PORTAL_UPSTREAM_END_SESSION_URL`      | optional: ZITADEL's `end_session` endpoint              |
| `PORTAL_UPSTREAM_CLIENT_ID`            | optional: customer Dex's client ID in ZITADEL           |
| `PORTAL_OPERATOR_SUBJECTS`             | optional: comma-separated operator subjects (below)     |
| `PORTAL_AGENT_AUDIENCE`                | optional: dedicated machine JWT audience, paired below |
| `PORTAL_AGENT_SUBJECTS`                | optional: JSON issuer/subject/grants list, paired below |

The database URI names host `business-site-portal-db-rw.business-site.svc`,
port 5432, database `portal` and login `portal_web` with its password, and the query
`sslmode=verify-full&sslrootcert=/run/webapp-inputs/portal-database-ca`.

The production overlay ships the upstream logout endpoint and its public client ID as
a pair. An existing complete projection takes precedence. The package defaults take
effect when a complete portal starts with both projected logout keys absent. Sign-out
and a refused sign-in then also end the ZITADEL session. The ZITADEL client must list
`https://portal.devantler.tech/signed-out` and `https://devantler.tech/` as post-logout addresses.

`PORTAL_OPERATOR_SUBJECTS` requires all five dependency keys. It lists the exact
customer Dex subjects of the operators, separated by commas. Configure the
ZITADEL subject for an operator whose GitHub account is linked in ZITADEL.
Customers and operators both use [the portal sign-in page](https://portal.devantler.tech/sign-in).
The server selects the role and workspace from the verified identity and current
membership. Once a ZITADEL operator subject is configured, the
`/operator/sign-in` bookmark opens the same page. Deployments configured with
only legacy platform operator subjects retain the earlier platform sign-in.
See [the customer identity guide](../server/CUSTOMER-IDENTITY.md).

The five dependency keys come all together or not at all. With them but no
activation keys, startup checks the database login and grants and discovers
Dex, but sign-in stays off. The runtime skips well-formed portal settings outside
its supported set and logs how many it skipped. Repeated keys, empty values,
surrounding whitespace and CR line endings are refused. The staging lease and
the owner-only secret file stay environment-only, and no supported portal
variable may also be inherited from the environment. Logs and errors omit
setting names, values and paths. Restart to rotate.

The two machine keys are both-or-neither and require the complete portal. They contain
public policy only; no machine secret is projected into this application. The audience
must differ from the browser client, and every listed issuer must match the portal broker.
Agents are read-only by default, with exact write grants from a closed catalogue; money
and deletion grants are always refused. See [machine callers](../server/CUSTOMER-IDENTITY.md#machine-callers)
for the complete format, token limits and action names. Deploy a runtime with
forward-compatible projection support before adding keys older versions do not
recognize. A skipped setting takes effect once a runtime that supports it starts.

The regular production portal can offer operators a fictional client workspace
in their existing session. Leave `PORTAL_CLIENT_PREVIEW_DATABASE_URL` and
`PORTAL_CLIENT_PREVIEW_DATABASE_CA` unset until a release that accepts them is
serving. Supply them together. The URL uses `postgres://`, the isolated
`portal_preview` database, its `portal_preview_web` login on port 5432 and
`?sslmode=verify-full`. The CA value is the separate database's PEM trust root
encoded as a single Base64 line. Use the preview database's restricted runtime
login and current migration. Restart the runtime after changing the settings.
The operator can return to their workspace through the regular portal if the
preview database is unavailable.

With sign-in on, invite customers from a web pod as
[CUSTOMER-IDENTITY.md](../server/CUSTOMER-IDENTITY.md#invite-a-customer) describes.

## Portal schema migration

`job-portal-migration.yaml` runs the schema migration once per release
([MIGRATIONS.md](../server/MIGRATIONS.md#run-the-explicit-job)), as login `portal_migrator`
(which owns database `portal`) with `--runtime-role portal_web`:

- **Image.** The publisher pins the WebApp's image. `prod/kustomization.yaml` copies
  that image into the Job, so the Job always runs its own release. The Job carries the
  `kustomize.toolkit.fluxcd.io/force` annotation, so Flux recreates it when the image changes.
- **Input.** The platform supplies Secret `business-site-portal-db-migration` with `database-url`
  and `ca.crt` (devantler-tech/platform#6372). It is mounted read-only at `/run/portal-migration`
  with mode 0400 and `fsGroup` 65532, which the kubelet writes as 0440 root:65532. So the Job
  uses `--migration-database-projection`; the strict `--migration-database-file` refuses group
  bits. The CA is copied to a private file under `/tmp`, a memory `emptyDir`, which is sticky
  (1777) as the job requires. A disk `emptyDir` is not.
- **Network.** Its pods carry `app.kubernetes.io/name: business-site-portal-migration`, so
  neither the web policy nor the disruption budget selects them. The platform's
  `business-site-portal-migration` policy allows only TCP 5432 to the portal database and DNS.
- **Order.** Flux applies the Job and the new WebApp together and cannot order them within one
  package. With the portal on, web startup refuses to serve until the ledger holds the release's
  migrations, so new pods become ready only after the Job. Old pods keep serving meanwhile.
- **Failure.** It retries at most twice within 240 seconds, inside Flux's five-minute health
  wait. A terminally failed Job stays failed when Flux reconciles the same release. After
  resolving a transient cause, verify that the failed Job's pods have exited, the same
  signed release is still selected, and its migrator supports the committed ledger.
  An authorized platform operator can then delete that failed Job. Flux recreates it
  from the signed release on the next reconcile. Verify that the replacement Job completes
  and the tenant Kustomization becomes Ready.

### Recovery after an additive migration

An older web pod can keep serving after an additive migration, while its release's migration
Job refuses ledger versions beyond its embedded list. A responding homepage alone cannot
show that the older package will reconcile.

1. Check the migration Job result and the committed ledger versions and checksums through the
   authorized database read path. Treat the Job's generic failure as inconclusive. Record the
   last successful migration image and the attempted and applied Flux revisions.
2. If migrations from the newer release committed, prepare a forward fix that retains every
   committed migration byte for byte. Rehearse upgrade and recovery on disposable PostgreSQL
   with the release's migration command and separate migration and runtime roles. Verify that
   a repeat run leaves the ledger unchanged and preserves an existing synthetic record.
3. Use the normal signed release workflow and the platform's
   [tenant recovery procedure](https://github.com/devantler-tech/platform/blob/main/docs/TENANTS.md#releases-reach-production-by-themselves).
   Check that Flux applied the intended signed manifest digest, the Job completed from the
   paired runtime image, and the rollout and Kustomization remain ready on a later reconcile.
   Complete an authenticated portal journey before closing the recovery.

Preserve the committed ledger and the separate database roles throughout recovery. If a
forward fix is unavailable, use the separately authorized backup and restore procedure.

## Portal retention purge

`cronjob-portal-retention.yaml` runs the retention purge
([server/README.md](../server/README.md#retention-purge-job)) every day at 03:17 Copenhagen
time. It deletes the portal records of former clients 3 years after their last membership ended,
as the privacy notice states:

- **Image and input.** Same as the migration Job. `prod/kustomization.yaml` copies the WebApp's
  pinned image into it, and it reads the platform's `business-site-portal-db-migration` Secret
  through `--migration-database-projection`, as the migration role. The runtime cannot delete
  this history.
- **Network.** Its pods carry the migration Job's `app.kubernetes.io/name:
  business-site-portal-migration` label, with `app.kubernetes.io/component: retention-purge`.
  The platform's migration policy is therefore its only path to the database.
- **Runs.** `concurrencyPolicy: Forbid`, no retries and a 300-second deadline. A failed run
  deletes nothing; a later run tries again. Flux does not wait on a CronJob, so a failed run
  never makes the tenant unhealthy. Each run logs only how many former clients lost records.
- **Before the migration.** While a new release's migration Job has not finished, the purge's
  schema ownership check refuses to run, so it never deletes against an older schema.

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
PORTAL_STRIPE_TAX_RATE=txr_…
```

The Numina line alone starts the invoice sync, which invoices nothing while its
flag is off. The three Stripe lines come together or not at all;
`PORTAL_STRIPE_TAX_RATE` is the ID of the inclusive 25% VAT tax rate in the
Stripe Dashboard (`server/BILLING.md`), not a secret. Omit the optional
settings to keep their defaults:

| Key                           | Default                  |
|-------------------------------|--------------------------|
| `PORTAL_NUMINA_API_URL`       | `https://api.numina.app` |
| `PORTAL_NUMINA_SALES_ACCOUNT` | `1000`                   |
| `PORTAL_NUMINA_VAT_CODE`      | `U25`                    |
| `PORTAL_STRIPE_API_URL`       | `https://api.stripe.com` |

The Numina settings other than the key file are refused without it. Another API
origin also needs its own egress rule. The flags still decide whether anything
is invoiced or charged.

## Portal email inputs

The portal emails order confirmations and withdrawal receipts itself
([BILLING.md](../server/BILLING.md#emails)) through the site's send-only SES sender
(devantler-tech/platform#5400). The platform adds that sender's OpenBao entry
`apps/business-site/smtp` (`username`, `password`) to `business-site-runtime` as
`smtp-username` and `smtp-password` (devantler-tech/platform#6368), and its portal egress
policy allows `email-smtp.eu-central-1.amazonaws.com` on port 587. The WebApp projects both
files. Nothing reads them until the portal settings name them:

```text
PORTAL_SMTP_USERNAME_FILE=/run/webapp-inputs/smtp-username
PORTAL_SMTP_PASSWORD_FILE=/run/webapp-inputs/smtp-password
```

The two lines come together or not at all, and the files use the disclosure projection's
confinement. `PORTAL_SMTP_ADDR` (default `email-smtp.eu-central-1.amazonaws.com:587`) and
`PORTAL_MAIL_FROM` (default `no-reply@devantler.tech`) are optional. The portal queues email
only for orders and withdrawals, which their false-default flags control, and sends only while
sign-in is on.

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

Release initiation requires a manual dispatch from `main` that confirms the full
signed source commit. The publisher accepts stable releases created and published
by the configured GitHub release App.

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

The dry run exercises the effective App credentials and release decision without
creating a tag, release or registry publication. Require it to pass for the
reviewed source before dispatching a release.

Pass the full verified commit as `expected_source` to release the same source.
Both release dispatch and stable-tag publication require the repository setting
`RUNTIME_PUBLICATION_ENABLED` to be `true` and `.github/runtime-publication` to
contain the single line `enabled` in the frozen signed source. Use a reviewed
source change to enable the policy after the rollout prerequisites pass.
After production activation, leave the repository setting enabled between
ordinary releases. Setting it to `false` stops new
publication admissions, including those for a source already marked `enabled`.

```sh
gh workflow run release-runtime.yaml --ref main -f mode=release -f expected_source="$source"
```

If `main` advances before GitHub binds the dispatch, repeat the preflight for the new
signed commit. Both admission jobs disable Go-cache reuse. The `release: published`
caller follows the actual stable tag and its original CI receipt as `main` advances. Its
`publish` job runs the commit-pinned `devantler-tech/.github/actions/publish-app` action in
`prebuilt-webapp` mode, directly in this workflow, so the image and the manifests are signed as
`publish-runtime.yaml` on the release tag: this repository signs its own releases
(devantler-tech/monorepo ADR 0017). The action stages, signs and verifies both digests before it
exposes the version, and never rebuilds the image. The release-only tool uses its maintained
Node 22/npm 11 environment; the application still builds and validates under Node 24/npm 11.

The final read-only job verifies both immutable subjects and both version aliases as signed by
this repository's release workflow on the release tag, the only accepted publisher identity, with
paired source/version/run/attempt/original-CI/public-digest claims.
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
