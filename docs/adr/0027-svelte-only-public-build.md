# 0027: Use Svelte as the sole public application

Date: 2026-10-28

Status: Accepted

## Context

The public site already publishes Svelte, and the private backend remains Go. Local builds and
content compilation still relied on the previous Astro application. The maintainer confirmed
that the site should use Svelte and Go and requested removal of the unused Astro site (#125).
The existing green-orb favicon is the approved identity and stays unchanged.

## Decision

Every public build and Go package uses Svelte. Remove the Astro application, compiler dependencies,
research sample, frontend release gate and publisher recovery input. Retain frozen publication
artifacts and immutable Git revisions as recovery evidence.

Compile the existing Markdown collection directly with Satteri, YAML metadata and Expressive Code.
Use Sharp for local images and the existing PDFKit CV renderer with native font-package reads.
Never execute MDX imports or expressions. Historical MDX filenames and catalogue/CV metadata
remain data for the content and cross-product drift checks, not a second application.

The source-bound generated content retains full articles, guides, RSS, diagrams, dates, assets
and discovery downloads. Svelte owns routing and presentation. Keep the historical image URL
namespace for compatibility, without an Astro dependency. Preserve complete-history admission,
input fingerprints, file-descriptor checks, asset closure, publication nonce/file inventory,
visitor privacy and private-output refusals. The independent company and offer settings remain.

## Consequences

Contributors install the root content-tool and web lockfiles. `npm run build` prepares native
content and verifies the complete Svelte artifact. Standalone web builds first run
`scripts/build-journal-reference.sh`. Both default and production-template Go packages use the
same application and retain their existing runtime checks. This decision grants no private
portal, authentication, customer-intake or payment activation.
