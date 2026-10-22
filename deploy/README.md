# Public runtime package

This directory prepares the public website for the platform's namespace-bound
WebApp interface. It is not applied or published by any workflow. Pages remains
the public deployment; portal access stays disabled.

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
Only gateway ingress to the application port is opened. Cilium's documented
`egress: [{}]` enables egress default deny; this package adds no outbound allow
rule. Other platform policies remain additive, so this is not an overriding deny
of every platform-granted connection. No database, migration, broker or private
activation setting is configured. The checker accepts only the committed,
independently verified baseline image; a same-repository digest alone is insufficient.

The selected disclosure file must be provided separately in the same namespace.
The platform projects it read-only at the fixed mount with mode 0440 and the
application's primary group. No disclosure value belongs in this repository or
image. Tests use fictional values in disposable volumes.

The example.invalid hostname is deliberately inert. It is not admitted by the
current platform's production route policy. Before registration, independently
review the real hostname, its allowlist, selected input materialization and
network/controller behavior. Do not apply this directory to an ambient cluster.

Issue #102 retains the semantic-version release caller, actual signed manifest
artifact and separate platform consumption proof. See
[the source-contract decision](../docs/adr/0019-public-tenant-package.md).

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
No publisher workflow, release switch or portal activation is introduced here.
