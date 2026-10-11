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
