# Production public-content profile for the combined runtime

Status: Accepted for source implementation; not authority for deployment.

## Context

The existing Pages publisher refreshes the complete public ranking and renders
the reviewed company disclosure. The default combined package intentionally
keeps the company rollout off. Moving hosting must not silently remove either
visitor capability. Package visibility is not verified, so restricted disclosure
inputs must never be embedded in a distributable archive or image.

## Decision

Keep two permanent package profiles: `default` preserves the ordinary network-free
build; `production-template` refreshes all catalogue repositories into owned build
scratch and renders company information with two fixed address slots. Astro uses
its validated `astro:env` enum, not a portal feature gate. The packager explicitly
clears ambient address/profile inputs. Incomplete, invalid, future or over-one-hour
old build-local rankings fail; no tracked data is rewritten for packaging.

The production runtime first verifies the unchanged signed template digest. It
then reads a bounded, owner-readable projected two-line disclosure file through
the existing root-confined descriptor boundary. Only native text spans on the
two EN/DA company pages may contain slots. Missing, duplicate or misplaced slots,
bad inputs or placeholders under the default profile fail before listening.
Escaped address text produces a new immutable in-memory public snapshot. Other
routes, template files, image layers and build receipts remain unchanged.

`--version` binds source, template digest and profile. `--check-serving-public`
validates the projection and reports a separate effective public digest, never
the input, path or address. Template and served digests are deliberately distinct;
the signature authenticates the distributable template, not private deployment
configuration. Rotation requires a controlled restart, not request-time reads.

CI retains the default package/container controls and additionally exercises
the production profile, missing-input refusals and fictional projected disclosure.
The source-owned main-only publisher signs and repulls the exact tested production
image. Fictional test input is mounted, never baked into its layer.

## Consequences and boundaries

The current Pages publisher stays live and must never upload template placeholders.
No customer access, intake, payments, provider enrollment, worker/API/MCP execution
or hosting cutover is activated. Business-site #67 gates the eventual combined
hosting change; monorepo #3939 and the outstanding privacy/deployment prerequisites
remain separate. No real residential disclosure belongs in source, test evidence,
public receipts or ordinary visitor pages.
