# 0031: Run the upstream ZITADEL image unmodified

- Status: Accepted, 2026-11-14 (maintainer decision, #577)
- Supersedes: [0022](0022-output-bound-login-icon-inventory.md),
  [0023](0023-corresponding-source-replay.md), [0025](0025-output-bound-text-font-notices.md),
  [0026](0026-output-bound-copied-console-assets.md), [0028](0028-original-identity-release.md)
  and [0029](0029-identity-lifecycle-validation.md)
- Amends: [0021](0021-customer-identity-provider.md), for the identity image only
- Scope: #577; #305 deploys the image; part of devantler-tech/monorepo#5047

## Context

[ADR 0021](0021-customer-identity-provider.md) selected ZITADEL as the customer identity
provider. This repository then built ZITADEL from patched source: it removed fonts and
provider icons, added a source-offer link, admitted every asset in the image, and captured,
replayed and published the corresponding source. The superseded ADRs record that lane.
Since 2026-10-07 it took 22–30% of this repository's commits, for a feature that is
disabled in production. Patching the AGPL source is what creates the duty to publish
corresponding source.

## Decision

- ZITADEL stays the customer identity provider. ADR 0021 stands.
- When the portal is activated, #305 runs the official upstream ZITADEL image, unmodified
  and pinned by digest.
- Branding uses ZITADEL's native
  [Label Policy](https://zitadel.com/docs/guides/manage/customize/branding): logo, colours,
  custom font and hidden watermark. No source or image file is patched.
- We modify and redistribute nothing, so there is no corresponding-source duty.
- The source-build, asset-patching, corresponding-source and replay lane is removed: its
  Dockerfile, publication workflow, CI jobs, scripts, tools, retained notices and build-only
  server commands. The runtime integration (customer identity preparation and the portal's
  Dex sign-in) is unchanged.

## Image verification

The image is pinned by digest. Its signature is not verified, because upstream does not
sign its images: there is no cosign signature and no GitHub attestation, only an unsigned
BuildKit SLSA provenance. The maintainer accepts a digest pin without signature
verification. The request for signed images is
[zitadel/zitadel#12693](https://github.com/zitadel/zitadel/issues/12693); add signature
verification when upstream signs.

For the identity image, this digest pin replaces ADR 0021's "source-owned immutable
provenance" activation gate. ADR 0021's other activation gates still apply.

## Consequences

- Lost: the exact, closed admission of every asset in the image. The upstream image ships
  fonts and provider icons we never display. That is a licence-scope preference, not a
  runtime protection: upstream distributes those files, the Label Policy replaces the font
  that is displayed, and with no external identity providers configured the provider icons
  never render.
- Lost: ADR 0029's lifecycle check, which ran against our own built image. #305 must
  validate the upstream image before activation, as ADR 0021 requires.
- The AGPL-3.0-only grant for the identity build-tool files ends with those files; earlier
  revisions keep the terms they were published under.
- An upstream update is a reviewed digest change, not a rebuild.
