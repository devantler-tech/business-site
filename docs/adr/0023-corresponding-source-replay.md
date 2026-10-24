# 0023: Seal and replay the identity build's corresponding source

Status: Proposed

## Context

The selected ZITADEL service is built from modified upstream source. The compact
build diagnostics deliberately exclude that source and dependency archives, so
their receipts cannot reconstruct it. Re-running the original preparation also
requires the original inputs that contain the removed, unadmitted fonts.

Issue #343 supplies one source-bundle and replay increment under #308. Image
publication, recipient retrieval and the visible source offer remain release
acceptance; runtime deployment and customer invitations remain #305/#302.

## Decision

The independently deliverable first boundary is the complete pre-install source
snapshot, literal transformation/recipe records and closed archive round trip.
The existing Linux builder exposes it through an explicit `--capture-source`
option. CI requires that option, the adverse controls and the actual archive
unpack; ordinary builds remain inert. Its completed build receipt explicitly
records `dependencyInputsCaptured: false` and `nativeOfflineReplayProved: false`.
This increment does not close #343 or admit an image.

The selected service-Go increment retains the actual 231 original/effective
module selections, their complete ZIP/go.mod inputs and the four literal
catalogue/lock/compiler records. The exact root command's native `Deps` plus root
must equal the observed package set; seeing one package per module is insufficient.
Go's maintained module library validates ZIP structure and full `h1` content sums,
including replacements. A closed-member verifier rechecks the copied bytes and
external manifest seal. CI moves the actual retained package to a fresh directory
and verifies it again before image smoke checks. Its narrow
`selectedServiceGoSourcesCaptured` field can be true while both overall dependency
capture and native offline replay remain false.

The verifier itself is a separate general-purpose build tool with an explicit
Go module/version/checksum acquisition boundary. It does not enter the service's
231-module graph. No module ZIP, complete retained package or executable is added
to the compact diagnostic upload; full source publication remains #308.

## Required complete replay boundary

The complete #343 replay must extend the existing explicit Linux entrypoint with an exclusive verified-prepared
input. Keep the ordinary upstream-Git mode and reuse the same installation,
generation, compilation, notice and image checks. The source-bundle helper packages
and verifies inputs; it does not introduce a second build graph or execute a
command supplied by an archive.

Seal the prepared tree immediately after the existing API serialization change
and before any install or generation. Verify complete sorted file membership,
bytes and executable modes against the frozen preparation profile. Preserve valid
empty files and tracked bootstrap protobuf files. Retain original and serialized
API configuration, the exact preparation/build recipes and a dated modification
record. Do not guess source membership by excluding generated-looking files from
a post-build tree.

Capture only build-owned dependency inputs observed in that build. Keep the
service's selected effective Go modules distinct from the version-suffixed Go
generation tools and their resolution/checksum inputs. Recompute complete ZIP
and go.mod content hashes; a cache marker alone is insufficient. Preserve the
actual filtered pnpm workspace/peer/platform closure and exact imported BSR
schema revisions with their source/documentation/licence inputs. General-purpose
tool acquisition is explicit, version/checksum bound and separate from service
source membership.

An externally supplied digest seals the closed archive and its member manifest.
Use only bounded regular members with canonical paths, declared modes and complete
reads/writes. Refuse duplicate, missing, extra, linked, escaping and altered
members. Archive hashes live in external sidecars, avoiding self-references.
The final build receipt binds the source manifest and resulting binary; do not
rewrite an earlier captured receipt to create a circular claim.

Replay into fresh source, store, module and compiler caches. Keep the native
commands and API-before-console tool version ordering. Supply declared checked
local inputs and deny network access during the build. Retain public checksum
verification; do not turn it off to hide missing cache or resolution metadata.
Successful replay establishes the tested input/source correspondence, not
bit-for-bit reproducibility.

## Validation and limits

Tests cover a valid empty source, executable modes, bootstrap retention, changed
preparation/configuration/locks, malformed archives, missing or extra dependency
input, conflicting identities, partial-success reads and failed publication of a
success receipt. A real clean hosted capture/unpack/network-denied replay must
complete the native generation, service and notice path.

Ordinary website output and portal-default-off behavior remain unchanged.
Diagnostics retain only bounded manifests, hashes and replay outcomes, not the
unadmitted full source archive or service executable. This decision alone grants
no image distribution, credential, account, customer or deployment authority.
