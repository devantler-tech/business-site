# Prepare customer sign-in configuration

The portal trusts dedicated customer Dex, backed by self-hosted ZITADEL for
invited local users. See [ADR 0021](../docs/adr/0021-customer-identity-provider.md).
The explicit offline command prepares source inputs only. It creates no services,
users, credentials, DNS, mail, database or portal activation. Ordinary builds and
publication do not run it or include its output.

From `server/`, using the exact compiler in `go.mod`:

```sh
GOTOOLCHAIN=local go run ./cmd/customer-identity-config
# No output unless preparation is explicitly requested.
GOTOOLCHAIN=local go run ./cmd/customer-identity-config --prepare \
  --dex-issuer=https://login.example.invalid \
  --zitadel-issuer=https://accounts.example.invalid \
  --portal-callback=https://site.example.invalid/portal/callback
```

These fictional destinations cannot be deployed publicly. Supply separately
approved canonical HTTPS origins on distinct hosts. Ports, IPs, user information,
wildcards, paths on either issuer, queries, fragments and alternate spellings fail.
The portal callback must end exactly in `/portal/callback`.

## Native consumers and private provisioning

The output wraps four documents. Extract each for its actual consumer; the wrapper
is not a deployment or import format. A failed or partial write is not a package.

| Document             | Consumer and boundary                                                                                                                                                                           |
|----------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `zitadelConfig`      | Native `--config`: HTTPS, initial instance policies, supported embedded Login V1, closed user/organization registration, password/lockout/MFA and token lifetimes.                              |
| `zitadelSteps`       | Native `--steps`: `FirstInstance.Skip=true` prevents creating the upstream default administrator or any instance.                                                                               |
| `zitadelApplication` | Modern `CreateApplication` request template for one confidential web client, code grant and the exact Dex callback. Supply its project ID privately after controlled instance/project creation. |
| `dexConfig`          | Native customer Dex configuration with one ZITADEL OIDC connector and one separate confidential portal client.                                                                                  |

The formats target ZITADEL 4.20.0 and Dex 2.46.0. Submit a privately completed
application request to `/zitadel.application.v2.ApplicationService/CreateApplication`
with its supported Connect protocol and private provisioning authorization. Save the
returned client ID and secret privately as `CUSTOMER_ZITADEL_CLIENT_ID` and
`CUSTOMER_ZITADEL_CLIENT_SECRET`; neither is a fixed source identifier. Dex expands
those native environment references. Its independent downstream client reads
`CUSTOMER_PORTAL_CLIENT_SECRET` through `secretEnv`. Only that downstream credential
belongs in the portal's supported private secret-file input. The portal receives
no provider administration, database or bootstrap credentials.

The source command does not expand private values or render the API template.
Runtime preflight must reject missing, empty or unresolved inputs and use JSON-safe
substitution. Never commit expanded configurations, invite links or account data.
The ZITADEL configuration has no database, master key, mail or bootstrap credential.
It is deliberately insufficient to create a usable identity instance alone.

### Private native phase preparation

The separate `cmd/customer-identity-materialize` command prepares one private
ZITADEL phase. It writes private native inputs for a separate deployment job.
With no arguments it reads and writes nothing. Ordinary builds do not invoke it.

```sh
GOTOOLCHAIN=local go run ./cmd/customer-identity-materialize \
  --phase serve --inputs /absolute/private/inputs --output /absolute/private/new-output
```

The three options must appear exactly in that order. The accepted selections are
`init`, `setup`, `setup-mail`, `serve` and `dex-serve`. Inputs use a physically resolved absolute directory, owned
by the invoking user with mode 0700. Every member is a bounded regular file with
mode 0600, the same owner and one hard link. Symlinks and extra members fail.
The standalone command accepts only this owner-file transport.

The verified combined application also exposes the same maintained producer as an
exclusive one-shot command. A deployment job can use its packaged executable without
installing Go, including in the production-template package:

```text
/app/business-site --identity-materialize --identity-phase serve --identity-inputs /absolute/private/inputs --identity-output /absolute/private/new-output
```

All four options are required exactly once. No web, public-content, disclosure,
version or migration option may accompany them, even explicitly set to false.
An ordinary unbound build and a cancelled invocation fail before preparation.
The command reads no public artifact, opens no listener and performs no database,
network or service operation. Success prints only `customer identity phase prepared`;
errors never reflect supplied paths or credentials. The output contract below is
unchanged. Native API provisioning remains required integration work in #305.

Replace `--identity-inputs` with `--identity-projection` to select the separate
atomic-projection transport. Never supply both options. The process must be non-root
with a nonzero primary group. Mount a flat, phase-specific projection with exactly
the required keys, their `..data/name` aliases, one timestamped generation and
its `..data` link. Root or the invoking user may own the entries; file group reads
require the process primary group. Other-user access, writable groups, executable
files, hard links and arbitrary link targets fail. Generation directories retain
strict permissions. The mount root may retain the volume writer's broad directory
bits only when the kernel reports that its pinned descriptor belongs to a read-only
mount. Root identity, permissions and mount status are checked again during final
snapshot validation; a writable root with those same broad bits is refused.

The producer pins one generation and rechecks complete membership, identities and
bytes around output creation. A rotation or incomplete writer transition fails;
retry the one-shot job from a fresh invocation. No input permissions are changed
and no intermediate credential directory is created. Outputs remain new, owner-only
phase documents. These are observed snapshots, not a lock against later rotations.
The transport tests use fictional native-layout projections. macOS additionally
exercises actual read-only disk-image mounts. The container smoke command
requires the Go integration test for both application image profiles. All five
phases accept the copied root-owned read-only projection and reject its writable
counterpart. Actual copied membership, bytes, ownership and modes are checked
before execution; missing tests, skips and partial results fail the smoke command.
These are filesystem proofs; actual Kubernetes controller, service, provisioning
and login acceptance remain under #305/#302.

All ZITADEL phases require `settings.json`, `database-password` and `database-ca.pem`.
The JSON contains exactly these string fields:

- `schemaVersion`: `customer-zitadel-private-v1`.
- `dexIssuer`, `zitadelIssuer`, `portalCallback`: the separately approved canonical
  HTTPS destinations accepted by the dormant preparer.
- `databaseHost`: a canonical DNS hostname without a port, scheme or path.
- `databaseName`, `databaseUser`: separate lowercase native SQL identifiers.

Initialization additionally requires `databaseAdminUser` in settings and the
independent `database-admin-password` file. The administrator identity and password
must differ from the application identity and password. It receives no master key,
human bootstrap credential or service private key.

Setup and ordinary service additionally require `master.key` (exactly 32 raw
bytes, never a complete unresolved environment reference), `tls.crt`, `tls.key`
and `service-ca.pem`. The current certificate chain
must validate for the exact ZITADEL issuer hostname with the supplied service CA,
and the private key must match. Database and service trust remain separate inputs.
Passwords are opaque valid UTF-8 of 32–1024 bytes, without surrounding whitespace,
line breaks or unresolved environment references. No input value is expanded.

Both setup profiles also require `bootstrap-password` and these additional string
settings: `instanceName`, `organizationName`, `humanUsername`, `humanFirstName`,
`humanLastName`, `humanEmail`. Its fresh human has an unverified email and must
change the initial password. That password is limited to 32–72 bytes and requires
the selected native ASCII lowercase, uppercase and digit classes, plus a character
outside ASCII letters/digits. This retains the selected bcrypt input limit without
applying it to database passwords. Ordinary service refuses every bootstrap/admin input.
No OIDC application credential is needed before supported API provisioning exists.

Select `setup-mail` for first-instance setup with the approved dedicated mail
sender. It requires all ordinary setup inputs plus `smtp-password` and four
private string settings:

- `smtpHost`: the submission service's canonical DNS hostname, without a port.
- `smtpPort`: `465` or `587`.
- `smtpFrom`: the approved sender's bare email address.
- `smtpUser`: the dedicated submission credential's username.

Use a separate password under the same opaque password requirements. The
producer writes these settings only to the private setup configuration. ZITADEL
uses its system trust store to verify the SMTP hostname before authenticating.
Use the same native `setup` invocation for either setup profile. The remaining
delivery work verifies that the provider persists across restart and that the
approved sender delivers invitations and recovery messages. See #374 and #302.

All input fields, certificate material and stable file snapshots are checked
before reserving a new mode-0700 output. Native files are mode 0600, use only fixed
consumer paths under `/run/customer-zitadel/`, and share the dormant preparer's
complete provider policies. Mount only a successfully completed output for its
selected phase. The native consumer arguments are:

```text
--config /run/customer-zitadel/config.json init
--config /run/customer-zitadel/config.json setup --steps /run/customer-zitadel/steps.json --masterkeyFile /run/customer-zitadel/master.key --tlsMode enabled
--config /run/customer-zitadel/config.json start --masterkeyFile /run/customer-zitadel/master.key --tlsMode enabled
```

Initialization emits configuration and the database CA. Setup adds explicit steps,
master key and the service TLS pair. Ordinary service contains no bootstrap steps
or administrator credentials. Do not mount an earlier phase's directory into a
later phase. No environment override or extra native argument is approved by this
offline preparation. Inputs may not contain the output directory.

A refusal prints only a fixed diagnostic. An occupied output is never modified.
If a write fails, cleanup removes only file identities created through the retained
output descriptor; an empty failed reservation is kept rather than deleting a
possibly replaced directory. A private reservation has one writer; no protection
against arbitrary concurrent same-user edits is promised. Never consume a failed or partial output. Offline
certificate/configuration checks do not prove a database handshake, persisted
policies, native execution, mail, customer authentication or deployment. Those
remain the separate runtime and acceptance work in #305/#302.

Actual initial provisioning needs a separate private steps document with explicit
fresh administrator inputs and `Skip=false`. Omitting `Human` does not prevent
upstream default account creation. Verify the persisted policy/client state before
exposure. `DefaultInstance` policies belong in `--config`, not `FirstInstance`
steps; unknown setup keys can otherwise be silently ineffective. Startup defaults
initialize a new instance, not reconciliation of an existing one. Use supported
administration APIs for existing policies and persisted credential rotation.

ZITADEL needs its own PostgreSQL role/store, private master key, verified database
TLS, certificate/key and working mail.

### Private Dex serving preparation

Use the same packaged command with `--identity-phase dex-serve` to prepare
the customer broker after the operator has commissioned the retained ZITADEL
application. Its `settings.json` uses the same endpoint and database fields
as ZITADEL serving, with `schemaVersion` set to `customer-dex-private-v1`,
`databaseName` set to `customer_dex`, and the additional `zitadelClientID` field
from the commissioned application. Use a dedicated PostgreSQL role and store.
The other required inputs are:

- `database-password` and `database-ca.pem` for verified PostgreSQL TLS.
- `tls.crt`, `tls.key` and `service-ca.pem` for the exact Dex issuer hostname.
- `zitadel-ca.pem` to add the upstream CA to Dex's HTTPS trust.
- `zitadel-client-secret` and `portal-client-secret` for the two independent
  confidential clients. Both use the same opaque password format as the database
  password. All three values must be distinct.

The new owner-only output contains only `config.json`, `database-ca.pem`,
`zitadel-ca.pem`, `tls.crt` and `tls.key`. The setup and database administrator
inputs remain separate. Run the command as the pinned Dex image's numeric
UID/GID 65534 so it can produce files for that same identity, then mount the
output read-only at `/run/customer-dex` in Dex. Start Dex with
`serve /run/customer-dex/config.json` and set
`DEX_EXPAND_ENV=false`, `DEX_CONFIG_DISALLOW_UNKNOWN_FIELDS=true` and
`DEX_CONTINUE_ON_CONNECTOR_FAILURE=false`. This preserves literal credentials,
rejects unknown configuration fields and refuses a failed upstream connector.
Only the independent portal client secret belongs in the portal's own
secret-file projection.

Native Dex startup, wrong-CA and wrong-name refusal, durable restart, and the
real ZITADEL code exchange remain release acceptance under #491 and #305.

## Invited users and authorization

Administrators create only approved users, without pre-shared customer passwords
or an email marked verified in advance. Native invitation mail establishes address
control and credential setup. Fresh authentication requires MFA; an upstream SSO
session can reuse its already-verified factor within the configured 15-minute
verification lifetime. Portal logout alone does not end that SSO session. Passwordless
and external login are disabled initially. Exercise expiring/replayed links, wrong
factors, recovery and deactivation against the actual selected version.

Dex requests `profile`/`email`, adds `openid` and reads UserInfo with issuer, subject
and TLS checks. Its `insecureSkipEmailVerified=false` requires a boolean verification
claim but **does not reject a present `false`**. It is not an email-verification
policy. The selected ZITADEL Login V1 requires address verification before completing
authentication, including after an email change. Keep that native lifecycle in
acceptance tests; do not pre-verify an invited user's address administratively.

Ordinary portal sign-in requests only `openid` and authorizes current, explicitly
invited issuer/subject membership, not email. It neither collects nor confirms the
saved contact address. The separate purpose-bound contact-confirmation flow requires
the signed boolean `email_verified=true` and the exact saved address/revision. A
missing, malformed or false claim is refused there. UserInfo can reflect an email
changed after authentication; never promote its address or status into membership,
roles or contact proof on ordinary sign-in. This configuration does not promise that
Dex refuses every identity carrying a false verification claim.

Dex sends S256 to ZITADEL; the confidential upstream client
has no documented setting that guarantees rejecting an omitted challenge. Do not
claim that rejection without proof. The downstream Dex policy requires S256.

An upstream account grants no portal membership. Trusted portal provisioning must
invite the actual verified Dex issuer/subject into its intended workspace, never
derive identity from email or use the upstream ZITADEL subject directly. Preserve
the adopted Dex issuer and `customer-zitadel` connector ID. Changing federation
paths needs explicit migration/linking with proof of both identities.

Portal logout destroys its local session, not all upstream SSO sessions. Upstream
deactivation prevents new login but does not instantly revoke portal sessions;
revoke portal membership/sessions for immediate denial and test each boundary.
Google, Microsoft, Apple and GitHub remain unconfigured.

## Validation and remaining proof

```sh
GOTOOLCHAIN=local go test -race ./internal/customeridentity ./cmd/customer-identity-config -count=1 -timeout=90s
```

Both complete ordinary cohorts include these packages. Payload tests prove source
preparation, not native service/browser acceptance. #303 tracks real local flow and
resource proof; #305 owns signed runtime delivery; #302 retains deployed acceptance
and real mail before separate activation. Synthetic brokers do not satisfy those gates.

Login V1 is embedded and supported for normal v4 operation. Login V2 adds a separate
Next.js service, API credential and routing; select it for a demonstrated need and
include it in resource/security accounting. Measure ZITADEL including login, Dex,
PostgreSQL, portal and any gateway/mail fixture at idle and during login. Report
versions, platform, duration and failures. Summed process RSS double-counts some
shared pages; it is not unique physical RAM or a production container limit.

### Local native acceptance — 23 October 2026

A disposable arm64 macOS fixture used the checksum-verified ZITADEL 4.20.0 native
release, Dex 2.46.0 built from its pinned release commit, PostgreSQL 17, the unchanged
maintained Go trial host and a loopback-only mail sink. An existing trusted local CA
issued short-lived fixture certificates; no trust store, certificate check or CSP
was bypassed. Numeric loopback ports and local database connections were explicit
fixture adaptations, not relaxed production endpoint or database-TLS requirements.

Native APIs persisted the selected policies and generated the confidential app's
client credentials. The actual browser completed invitation/password setup, required
authenticator enrollment, Dex consent and the signed-in portal workspace. A consumed
callback returned 400 without workspace data. Logout, immediate portal-membership
revocation, email-backed password recovery, rejection of the old password and reused
recovery link, wrong-factor refusal and retained MFA after recovery were observed.
Fresh authentication after recovery returned through Dex into the actual portal.
Native account deactivation refused a new sign-in but left an issued portal session
usable until separate portal revocation, which immediately denied that session.
The bounded host lease expired during an earlier continuation; that attempt did not
reach the portal and was repeated with a fresh disposable fixture, not counted green.

A separate fresh native instance tested an administrator-created, unverified invited
account. Dex redirected to ZITADEL's email-verification/password-initialization screen;
a wrong verification code was refused and no portal callback occurred. This confirms
that native lifecycle boundary, not a universal true-only claim filter in Dex. The
fresh fixture's first setup attempt rejected a generated administrator password
without a symbol; compliant private bootstrap input allowed the retry. The product
preparer creates no administrator and does not supply that password.

The first browser attempt exposed Chromium blocking the provider redirect under
the portal's strict form policy. The corrected native handoff document completed
the same chain without adding upstream origins to the portal policy. The ordinary
HTTP controls alone had not observed this browser-only failure.

Review found the same direct form redirect in the separately gated contact-proof
flow. A failing HTTP control reproduced it before both flows adopted the shared
handoff. A second real native composition enabled only identity and contact proof
for its fictional account: Chromium completed invitation verification, password,
MFA and ordinary `openid` sign-in, saved an unconfirmed contact, then followed the
contact POST's 200 handoff through Dex and ZITADEL and back to a confirmed saved
address. The trace included both real upstream origins with no CSP refusal. The
confirmation control disappeared; the Danish contact page had no overflow at
390 pixels. This used the maintained Foundation/Browser with injected in-memory
fixture flags, not a new activation mapping or a change to the ordinary trial host.

Two five-sample, roughly one-second process observations covered idle and native
password-recovery activity. ZITADEL including embedded login used 79–106 MiB RSS,
Dex 18–20 MiB, the Go portal about 13 MiB, all 20 PostgreSQL processes 110–132 MiB,
and the fictional mail sink 7–9 MiB. The sampled per-observation sum was 228–279 MiB;
the separate controller added about 11 MiB. Browser/automation memory is excluded.
These short observations are not peak/load measurements, unique physical memory,
container sizing, deployment capacity or a head-to-head comparison with Keycloak.
Real database TLS, restricted production identities, delivery/recovery, deployed
client/operator isolation and customer activation remain #302/#305 acceptance.
