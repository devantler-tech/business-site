# Frozen identity base sources

This explicit Go 1.27.3 command retains the original Debian sources and notices
for the frozen Linux/amd64 identity base. No arguments show help and perform no
work. It supports Linux and Darwin metadata; it neither runs retained programs
nor publishes an image, changes a cluster, creates users or activates the portal.

Build and test from `server/`:

```sh
GOTOOLCHAIN=local go test -race ./cmd/identity-release
GOTOOLCHAIN=local go build -trimpath -buildvcs=false -o /absolute/identity-release ./cmd/identity-release
```

Explicit acquisition contacts only the frozen public GCR/Debian inputs declared
in `base-profile.json`, with no credentials, ambient proxy, cookies or URL/profile
override. Responses have complete length/hash, status and close checks. Requests
and the whole acquisition are bounded. GCR redirects may reach its Google blob
storage; every resulting byte still has to match the original frozen digest.

```sh
identity-release capture-base-source --output /absolute/new-capsule
identity-release verify-base-source --input /absolute/capsule --sha256 EXTERNAL_MANIFEST_SHA256
identity-release pack-base-source --input /absolute/capsule --sha256 EXTERNAL_MANIFEST_SHA256 --output /absolute/new-base-source.tar
identity-release unpack-base-source --input /absolute/base-source.tar --archive-sha256 EXTERNAL_ARCHIVE_SHA256 --sha256 EXTERNAL_MANIFEST_SHA256 --output /absolute/new-capsule
```

The source set is closed: three original OCI metadata bodies, thirteen original
compressed layers, five original clear-signed Debian source descriptors, seven
original source archives/signature files, six installed-package records, six
copyright bodies and the complete referenced GPL-2 text. The 41 payloads and one
manifest are transported as read-only regular files under private directories.
Original image modes, including the ca-certificates copyright's 0555, remain
separate metadata; none of these data files is executed.

The verifier joins the original index to its single Linux/amd64 manifest, config
and ordered thirteen layer/DiffID pairs. It reads selected tar members as data,
checks package/source versions and each descriptor's complete SHA-256 section,
and retains the original bodies. It never extracts or runs a source archive.
Readback rejects extra, linked, missing, substituted or writable payloads and
requires the external manifest seal. Output must be new; failed partial output
cannot be resumed or overwritten. Packing rechecks the complete capsule before
and after writing, reads back the actual closed transport file, and uses fixed
USTAR metadata. It does not rebuild or parse a Docker-save candidate archive.

Explicit unpacking requires both independent transport and capsule seals. It
uses the same frozen member plan and complete capsule verifier, with canonical
transport readback rejecting altered headers, padding, ordering or trailing data.
Only a new private destination is accepted. Source archives remain opaque files;
no retained program or nested source archive is executed. A failed private
reservation is ineligible and cannot be resumed or overwritten.

The original retention operations retain OpenPGP armor without verifying it.
Their four signature/publication/admission receipt claims remain false. The separate
publisher must establish those facts and join this base source to the actual
tested image and application corresponding source before distribution. This
command is outside the repository's specific 37-file AGPL grant; no new license
grant for other authored application code is implied.

## Local publication handoff

`prepare-handoff` is a data-only observation of the original native job's three
archives and fixed witnesses. It streams bounded, complete physical-file
snapshots and joins the archive seals, original image/binary and independent
offline binary to their original receipts. Source and image archives retain
their producer modes and bytes. It writes only a new exclusive manifest.

The manifest binds raw witness bytes rather than rewriting selected fields or
copying private service process/network details into a summary. The existing
native engines remain responsible for archive membership, complete inventory,
Docker inspection, screenshots and replay execution. This operation does not
repeat those engines or turn their component observations into release admission.
The supplied source revision and CI run/attempt are caller assertions, not
authenticated GitHub provenance. A later publisher must authenticate the original
successful main run and independently verify the complete payload/witness set.
No source offer, registry, signature, distribution or activation claim is granted.

```sh
identity-release prepare-handoff --input /absolute/native-job-root --source-sha FULL_COMMIT_SHA --run-id ORIGINAL_RUN_ID --run-attempt 1 --output /absolute/new-handoff.json
```

## Original transport and authenticated publication

`pack-carrier` retains the fixed 14 witnesses, three original archives and their
handoff manifest, bounded to 1.25 GiB. `unpack-carrier` preserves bytes and modes,
rejects malformed or unexpected members and repeats the existing handoff join.
`restore-artifact` first authenticates the complete Actions ZIP against the digest
from the primary API, permits only `carrier.tar` and `carrier.json`, and requires
the externally authenticated source/run/attempt before restoration. It never
executes retained source. Transport receipts keep all five authority fields false.

```sh
identity-release pack-carrier --input /absolute/native-job-root --output /absolute/new-carrier.tar
identity-release unpack-carrier --input /absolute/carrier.tar --sha256 EXTERNAL_CARRIER_SHA256 --manifest-sha256 EXTERNAL_HANDOFF_SHA256 --source-sha ORIGINAL_SOURCE_SHA --run-id ORIGINAL_RUN_ID --run-attempt 1 --output /absolute/new-native-root
identity-release restore-artifact --input /absolute/artifact.zip --sha256 PRIMARY_API_ARTIFACT_SHA256 --source-sha ORIGINAL_SOURCE_SHA --run-id ORIGINAL_RUN_ID --run-attempt 1 --output /absolute/new-transport-root
identity-release verify-base-auth --input /absolute/base-capsule --sha256 EXTERNAL_BASE_MANIFEST_SHA256 --repository /absolute/trusted-caller --output /absolute/new-auth-evidence
```

The last operation uses the original verified descriptor bytes on GPGV's stdin
and only the public certificates pinned in `auth.go`/`third_party/debian-source-signers`.
Its receipt records five exact historical signer matches and the documented
Distroless Cosign identity. GPGV's supplied-key trust and expiry/revocation limits
do not establish current certificate validity, personal trust or Debian archive
Release verification. Original capsule proof fields are never rewritten.

CI retains the admitted original carrier only for main pushes, with compression
disabled and one-day retention. This is temporary Actions storage, not durable
corresponding-source hosting. A roughly 1.08 GiB carrier would cost about USD 0.01
for one day if entirely billable at USD 0.25/GiB-month; actual incremental charges
depend on the account's included storage and
[current GitHub pricing](https://docs.github.com/en/billing/concepts/product-billing/github-actions#storage-pricing).

The separate `Publish - Original Customer Identity` workflow accepts an original
successful main CI run and defaults to `dry-run`. It authenticates the complete
current run/attempt, required native steps, artifact identity/digest and original
source ancestry. Trusted caller code restores source and notices, compares the
original build records and inspects the original saved image without a rebuild
or service execution. A later caller with changed retained build records is refused.

Only explicit `publish` enters a separate privileged job and repeats admission.
Registry authentication and the existing image tag are checked before any source
release mutation. The complete release inventory is paginated, with a bound of
100 pages of at most 100 records; failed or malformed reads authorize no write.
After anonymous source readback, credentials are refreshed and the image tag is
checked again before a push. This preflight does not make the two services atomic:
later network, registry or signature failures can still leave a source-only release.
The fixed `identity-v4.20.0-devantler.1` release stages the two original source
archives and `identity-source.json` in a draft before publication. Every existing
asset must already match before additions; source-bound drafts can resume only
with the same original. Staging reads use the validated release database ID,
because REST by-tag cannot find drafts. The published release must be actually
immutable and its tag must retain the original source. Complete anonymous retrieval and immutable
asset digest readback must pass before the saved image is pushed to
`ghcr.io/devantler-tech/business-site-identity:4.20.0-devantler.1`, signed and attested.
The exact registry digest, signer, original source/run and source-offer digest
are independently verified. Existing mismatched tags/assets are refused; nothing
is clobbered. Before a release or tag reserves the version, an expired carrier
requires a new native build, never a reconstruction from diagnostics. Once the fixed source release exists,
only the same retained original can complete it. If that original expires before
image publication, recovery requires a reviewed new version/source-offer profile;
a different run cannot resume its immutable assets. There is no durable-original
recovery path in this initial release. Artifact storage, source publication, image publication and
portal activation remain separate states; #305/#302 still own runtime delivery
and the deployed invitation/login/MFA/recovery/revocation/isolation journey.

## Identity validation requirements

- `Dockerfile.zitadel` and `scripts/zitadel-{image-package,image-smoke,service-smoke}.go` assemble an
  explicit local identity image and exercise its real version command. CI
  checks a closed package, the copied-back image payload and inspected container
  restrictions, then saves the exact exercised image to a bounded private archive. It removes
  the original, confirms the complete owner-labelled image set is absent, reloads the sealed
  archive and rechecks the same config identity and inspected configuration before final
  owner-bound cleanup. The archive is excluded from diagnostic upload and is retained
  only by the separate main-push original carrier after transport admission.
  The inspected filesystem must contain the pinned base config's thirteen ordered
  DiffIDs followed by exactly three application layers (WORKDIR and two COPY steps). Its `baseRootFS` readback
  remains distinct from the archive, image-config and registry-manifest digests.
  Copy and retain the six original base package copyrights and their referenced
  complete GPL-2 text from that actual owned container. Keep their closed readback
  separate from /app notices; recheck it before export and after reload.
  Its size, digest and successful reload enter the receipt; they are not a registry identity,
  corresponding-source offer or distribution admission. The bounded
  diagnostic is `cli-version-smoke`, with admission and license closure unknown.
  The separate explicit service profile also cold-starts the same image with an
  ephemeral fictional PostgreSQL fixture on an internal Docker network, publishes
  no host ports and reads actual Linux process restrictions and RSS/peak
  measurements. Preserve its closed network, writable-path and owner-bound cleanup
  controls. HTTP connects only to the inspected owned private endpoint; its fictional
  authority is never resolved through DNS and redirects cannot leave that authority.
  Read the actual process mount table too; Docker's legacy tmpfs option is not
  guaranteed to appear among regular mount points. Keep the kernel read-only
  root and bounded, non-executable, owner-bound tmpfs admission mandatory.
  HTTP discovery/console availability is not browser, invitation, MFA, production
  TLS, complete-stack acceptance or activation. No image is published.
  The separately explicit `--browser-smoke` profile also uses the runner's Chrome
  and matching WebDriver inside the owned service network namespace as a non-root
  user. Its native browser sandbox remains enabled. It exercises only fictional
  Login V1 and authenticated Console presentation, plus ten native neutral-provider
  cases (light/dark chooser, two configured table rows, Google editor, login and
  registration). Fixture-only native API setup preserves the login policy except
  enabling the two unusable fictional providers; no external action is followed.
  Login/registration links are reached by native Tab, and the picker by Enter.
  The unchanged operator journey has 100s; the additional presentation phase has
  120s, within a five-minute worker wrapper. That same phase independently requires twenty
  SMTP cases (chooser and nine unsaved preset editors in both themes), including
  unchanged defaults, native Enter, loaded envelope glyphs and each screenshot.
  Console observations wait for the frozen fixture's native background transition
  to finish and its computed background color to match the requested theme.
  It never saves a mail configuration or sends a message. It retains source/image-bound receipts
  and screenshots; it grants no production login, mail, invitation or activation
  clearance. The Go browser worker is reachable only through the explicit profile.
  Four separate source-offer observations require genuine Tab focus, visibility,
  the exact versioned destination and same-page readback after each new screenshot,
  in Login and Console in both themes. They never follow the external URL inside
  the fictional fixture; public retrieval is a separate publication requirement.
  No arguments are inert; the real smoke entrypoint requires native Linux amd64
  and Go 1.26.10. Read `docs/ZITADEL-SOURCE.md` before using it.
