# Signed distribution of the combined runtime

The combined-runtime archive is the input to the non-root container, not a second
build of the website or server. CI builds it from clean, exact source; audits the
public snapshot; packages linked dependency notices; builds a pinned, signed
distroless runtime image; and actually runs that image with a read-only root,
dropped capabilities and bounded resources. Private identity stays default-off.

The image-only publisher job runs within same-repository **main push** CI,
after the complete prepublication aggregate and actual-image job succeed. The
final required-check aggregate also covers publication and fails if signing,
provenance verification or the published-digest smoke test fails. The publisher has
no privileged cross-run trigger, manual dispatch or inherited custom secrets.
It checks out that immutable source and downloads that run's tested image/package
artifact, re-exercises it, pushes a run-scoped GHCR staging tag, then signs and
verifies the returned immutable digest with the portfolio's keyless Cosign flow.
A signed SLSA v0.2 statement records the source, CI run/attempt and public digest;
it does not claim hermeticity, reproducibility or SLSA certification. The final
check pulls and runs the signed registry digest. The downloadable publication
receipt binds that digest, source and CI/publication runs. Tags are discovery
handles, never trusted identities; no moving `latest` alias is published.
The expected Cosign certificate identity is the caller's
`https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main`;
the publisher and its tests belong to that exact source-owned workflow.

## Bounded validation stages

Identity/database and portal-quality checks run before the dependent runtime
package job. Each retains a ten-minute job deadline. Packaging installs its own
locked Node dependencies and exact Go toolchain; its download-cache key includes
both frontend lockfiles. Download caches are an optimization, not evidence of an
installation or a guarantee of registry availability.

The portal HTML, secret and spelling checkers are exact development dependencies
in the root lockfile. The existing root clean install provisions them once;
their unchanged version-qualified `npx` commands resolve the installed packages
instead of fetching manifests and installing separate ephemeral tool trees during
validation. A real npm/npx loopback fault control demonstrates that an absent
checker reaches a deliberately stalled registry, whereas installed exact checkers
run without registry resolution and propagate nonzero observer exits. This does
not make cold installs offline or suppress fresh dependency audits.

The runtime job retains every package, extracted-archive and restricted-container
smoke check. Both stages feed the prepublication and final aggregates. Only a
successful same-run runtime artifact can reach the unchanged main-only signer.
This separation keeps slow dependency setup from consuming the identity job's
remaining packaging budget without increasing deadlines or tolerating failures.
Hosted timings and a verified main-push publication, not a local timing model,
establish recovery for issue #272.

This reuses pinned Docker/Cosign actions from the established portfolio publisher.
Its shared `publish-app` interface requires a semantic-version image/manifests
pair and deployment manifest that this slice intentionally does not create.
Source-owned image-only distribution therefore uses those compatible primitives,
not an external release service, new paid enrollment or copied monorepo code.

Pages remains the authoritative public deployment. An image publication is not
backend hosting, customer login or payment delivery. The next hosting change must
pin the verified digest, accept the exact publisher certificate identity/source
claims, configure `/healthz` probes and restricted resources, and independently
prove deployed public paths, TLS and recovery. Production portal activation needs
its existing separate provider, privacy and security prerequisites.
