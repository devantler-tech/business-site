# Svelte public frontend

SvelteKit and Svelte render the complete public site. The backend remains Go under
[server/](../server/README.md); Pages publication serves only static public output.
Every local build and runtime package uses Svelte. The frontend release gate and Astro
application/compiler are retired by [ADR 0027](../docs/adr/0027-svelte-only-public-build.md).

## Develop and build

Use Node 24, npm 11, Bash and a complete Git checkout. From the repository root:

```sh
npm ci
npm --prefix web ci
npm run dev -- --host 127.0.0.1
LC_ALL=C npm run build
npm run preview -- --host 127.0.0.1
```

Stop local servers with Ctrl+C. The normal build writes canonical `dist/`; the Svelte adapter
writes `web/build/`. Neither command publishes. Before a standalone `web/` build, run
`bash scripts/build-journal-reference.sh`. Regenerate content and restart development after
changing authored Markdown or shared content inputs. Never edit generated output.

## Content and routes

The public application retains English/Danish Home, About and Projects, all English Journal
articles, three main archive pages, complete topic/author archives, six technical guides,
RSS, CV/thesis downloads, sitemap, robots, sharing images and the existing green-orb favicon.
Historical project/template redirects preserve heading fragments and native continuation links.
Do not invent Danish articles, feeds or guides.

The maintained content remains under `src/content/docs/`. `src/data/build-content.mjs`
compiles Markdown with Satteri, YAML metadata and Expressive Code, resolves local images with
Sharp, and renders the CV from `src/data/cv.ts` with the existing PDFKit layout/font packages.
It never executes MDX imports or expressions. The template index derives cards from the actual
maintained collection. Legacy catalogue/CV metadata still serves cross-product drift checks.

Ignored `.generated/` records bind full bodies, heading IDs, copy payloads, tables, adjacent
navigation, metadata, committed dates, RSS and assets to the actual source fingerprint. Capture
and readback reject changed inputs, incomplete inventories and missing/altered dependencies.
The compiler uses inspected regular files. Local Mermaid modules load only for diagram articles;
accessible text remains available without JavaScript. Ordinary code uses a separate copy controller.
The retained `/_astro/` image namespace preserves public URLs without an Astro dependency.

The adapter owns canonical sitemap membership and excludes redirects/noindex pages. The promoter
checks the independent complete route inventory, resource closure, visitor privacy, private-output
refusals and this build's nonce/full file hashes before replacing `dist/`. Only it adds CNAME;
publication receipts remain publisher-owned. A failed build removes publishable-looking output.

## Independent business settings

`web/src/env.ts` declares server-only build settings. `FEATURE_COMPANY_IDENTITY` and
`FEATURE_OFFER_COPY` independently default to false and reject ambiguous values. Identity enables
the footer, email and two company disclosures. Its validated address appears only in those two
HTML documents; shared data, scripts and unrelated routes must never carry it. Local fixtures use
fictional addresses. `PUBLIC_CONTENT_PROFILE=production-template` retains the existing Go runtime's
escaped address placeholders. Invalid profiles fail. Revised-offer copy remains separately gated.

The approved VAT-inclusive guide prices, public/private customer invitation, six-tool catalogue
and bilingual content are permanent. The old frontend variable has no effect. Unsupported old
preview/client/featured presentation combinations fail rather than silently changing the result.
None of these settings activates Go portal sign-in, customer intake, country eligibility or payments.

## Verification and publication

```sh
LC_ALL=C bash scripts/check-svelte-frontend.sh
bash scripts/journal-history.test.sh
node --test scripts/public-build.test.mjs scripts/public-build-contract.test.mjs
```

The frontend check audits dependencies, runs native unit/type checks, validates source-bound
content, all visitor routes and negative controls, then exercises independent company/offer states,
invalid settings and private-diagnostic/address confinement. The history test uses real commits,
actual workflow checkout depths and shallow/incomplete-history refusals. Native Markdown tests
cover complete tables/bookmarks/copying/diagrams and rejection of executable content.

The source-owned publisher admits only exact main sources, explicit modes, the enabled publication
switch and protected deployment environment. Manual dispatch defaults to an artifact-only preview.
It has one public frontend. Frozen artifacts and immutable Git revisions remain recovery evidence.
A merge/build is not live proof: verify the successful deployment, public source receipt and journeys.

With Go 1.27.3, `bash scripts/package-runtime.sh --release` creates `runtime-dist/`;
`--production-template` creates `runtime-production-dist/`. Both use Svelte and ignore ambient
presentation inputs. The optional historical `--svelte` argument is a compatibility alias.
CI tests each package, extracted archive and restricted container. Packaging does not deploy.
