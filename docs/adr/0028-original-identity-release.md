# Original identity image and corresponding source publication

Status: Proposed. Delivery: #308.

## Decision

The identity release distributes the exact saved candidate exercised by one
successful main-push CI run. A new compilation has its own source/run identity
and cannot replace that candidate. Existing source, notice, base and image
engines remain authoritative; publication does not introduce a second inventory.

The native job keeps its read-only token. After complete local checks it retains
one bounded, one-day original-byte carrier on main only. The carrier contains
the existing archives and original handoff witnesses. Transport validation
preserves their complete bytes and recorded modes. Original producer receipts
keep their false/unknown publication claims.

A manual main-only workflow defaults to nonpublishing verification. It binds the
source, complete successful original CI run/attempt, native job and immutable
artifact digest before reading carrier data. Verification has read permissions.
The original source must be an ancestor of current caller main; complete retained
build records are compared with trusted caller source. Source and caller revisions
remain separate provenance fields, allowing unrelated main updates without
silently accepting changed build tools.
Only the explicit publish mode enters a separate privileged job, which repeats
admission from trusted caller source. Neither job executes retained source or
builds an image. The existing source engine verifies corresponding source and
restores original notices. Existing image helpers verify the saved image's exact
config, ordered RootFS, complete application payload and base notices.

The base trust declaration freezes public Debian certificate bytes, official
keyring revision and permitted signing/primary fingerprint pairs. Verification
establishes those exact historical cryptographic matches, not personal owner
trust, current certificate validity or Debian archive Release admission. The
exact Distroless signature uses its documented issuer and subject. Original
base capsule proof fields remain false; new evidence has a separate receipt.

Publication creates the fixed corresponding-source release already linked by
the native Login/Console footers. Both original source archives and a source/run/
digest manifest must be fully retrievable anonymously before the saved image is
pushed or signed. Existing mismatched tags/assets are refused rather than
overwritten. Image provenance binds the same source, original run, archive and
source-offer digests; registry readback verifies the actual signed digest.
The ordinary public-runtime publisher excludes identity tags.

## Boundaries and recovery

The carrier has a 1.25 GiB total bound and one-day retention; it is not durable
corresponding-source hosting. Public release assets retain the offered sources.
Before anything is published for the fixed version, an expired or incomplete
carrier requires a new native run, never a reconstruction from diagnostics.
A partial source-only release may be completed only with that same retained
original and matching immutable contents; it grants no image or activation claim.
If its original expires before image publication, recovery requires a reviewed
new version and source-offer profile. A different run cannot overwrite or resume
the old version's run-bound assets. This initial release has no durable-original
recovery path; one-day retention is an explicit operational limitation.

The publisher has no deployment, credential, user-provisioning, payment, customer
intake or portal-activation operation. #305 owns the complete identity runtime;
#302 owns deployed invitation, MFA/recovery/revocation and isolation acceptance.
No general legal-compliance or bit-for-bit reproducibility claim is introduced.

## References

- [GitHub workflow inputs and permissions](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- [GitHub artifact transport](https://docs.github.com/en/actions/tutorials/store-and-share-data)
- [GnuPG gpgv verification limits](https://www.gnupg.org/documentation/manuals/gnupg/gpgv.html)
- [Distroless signature identity](https://github.com/GoogleContainerTools/distroless#how-do-i-verify-distroless-images)
