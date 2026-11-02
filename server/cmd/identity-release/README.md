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

The descriptor's original OpenPGP armor is retained, **not verified**. The command
does not verify the base image signature, public source-release availability or
an identity image's admission. All four receipt claims remain false. The separate
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
