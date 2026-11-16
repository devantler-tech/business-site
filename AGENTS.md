# AGENTS.md — business-site

Devantler Tech's public English/Danish business website, Projects catalogue, journal and
supporting technical pages. This file is canonical; the monorepo's reviewed
[shared engineering contract](https://github.com/devantler-tech/monorepo/blob/main/AGENTS.md)
also applies. Use isolated worktrees, signed commits, issue-backed draft PRs and current-head
review/CI/user-path evidence. Never use an admin merge bypass or request Copilot review.

## Layout and ownership

- `web/` is the sole public application, built with SvelteKit and Svelte. Root `src/`
  and `public/` retain framework-neutral copy, authored Markdown, images and downloads.
  Local builds, publication and both Go package profiles always use Svelte. The retired
  frontend release flag has no effect; Astro applications, compiler and recovery option are removed.
- English/Danish Home, About and Projects, the complete English Journal, all technical guides,
  RSS, discovery assets and historical redirects are permanent. Preserve incoming heading
  bookmarks, the Devantler Tech favicon (`public/favicon.png`, the lime "D" icon) and the
  header wordmark (`src/assets/brand/`), which the maintainer approved on 2026-11-15. Never
  invent Danish article or guide routes.
- `src/data/build-content.mjs` compiles the maintained Markdown with Satteri, YAML metadata,
  Expressive Code, Sharp and the existing CV renderer. It never evaluates MDX. Historical
  `.mdx` filenames remain content inputs, not executable components. Technical template
  cards come from the maintained template collection. Catalogue/CV legacy metadata remains
  available to the cross-product drift guards; it is not another web application.
- `scripts/build-journal-reference.sh` prepares source-bound content under ignored
  `.generated/`; the normal build invokes it automatically. Use it before standalone
  `web/` builds. Complete own-source Git history, before/after input fingerprints, exact
  inventories, asset digests and independent refusal controls remain mandatory. Diagrams
  use only their locally bundled Mermaid graph; ordinary code retains its native copy controls.
  The `/_astro/` image URL namespace is retained for compatibility and has no Astro dependency.
- `web/src/env.ts` retains strict, independent default-off company/offer settings. The postal
  address is confined to the two enabled disclosure documents. Shared layout/load data never
  carries it; runtime template placeholders remain escaped. Public invitation copy does not
  activate Go portal capabilities. Read `web/README.md` for builds and verification.
- Framework-neutral homepage and quality copy lives in `business-copy.ts` and `client-copy.ts`
  under `src/components/business/`. Portfolio descriptions, presentation and bookmark aliases
  live in `client-products.ts` and `portfolio-content.ts`; preserve complete dated rankings
  and the shared bookmark-disclosure controller.
- The adapter supplies canonical sitemap membership, excluding redirects/noindex disclosures.
  The publication validator independently checks the full route inventory, local dependencies,
  visitor privacy, forbidden private output and this build's complete file hashes/nonce.
  It adds no CNAME and refuses a CNAME or copied `publication-source.json` receipt.
- `docs/README.md` contains the editorial, localization, catalogue and feature-flag standards.
- `server/` contains the default-off Go/PostgreSQL browser identity boundary. Read
  `server/AGENTS.md` before changing it. The platform runtime ships its handlers default-off;
  production authentication, customer intake and payments remain disabled.
  Customer identity source preparation uses dedicated customer Dex and self-hosted
  ZITADEL; read `server/CUSTOMER-IDENTITY.md`. The explicit offline preparer has no
  deployment, user-provisioning or portal-activation authority. Social logins are deferred.
- ZITADEL is never built, patched or redistributed here. When the portal is activated, #305
  runs the official upstream image unmodified, pinned by digest and branded only through its
  native Label Policy. Upstream does not sign its images, so no signature is verified. Read ADR 0031.
- Private portal presentation uses Go templates and native forms. Contact responses
  buffer the complete private frame and revalidate the original durable session/client
  after SDK waits. The separate mandatory `contactintegration` suite exercises the
  retained HTTP journey and authority checkpoints without a Node worker. ADR 0017
  records the rejected private-frontend experiment; public Svelte remains selected.
- `.github/workflows/ci.yaml` validates source and uploads a downloadable production preview.
- `deploy/` is the Kustomize package for the namespace-bound Platform WebApp contract;
  read `deploy/README.md` before changing it. One runtime serves `devantler.tech` (public pages)
  and `portal.devantler.tech` (the portal), routing by host so nothing appears twice, and it
  grants no portal activation. Platform hostname approval and registration remain separate.
  The source-owned runtime release/publisher callers are default-off behind the
  explicit `RUNTIME_PUBLICATION_ENABLED` repository variable. Original main-CI
  archive admission, the accepted original-image verifier and independent paired
  publication readback are mandatory. Keep private dependencies and migration inputs absent;
  portal settings and their private inputs come only from the platform's projected
  `business-site-runtime` Secret, and the schema migration Job reads only the platform's
  `business-site-portal-db-migration` Secret.
- The platform serves devantler.tech from that signed runtime; GitHub Pages is retired
  (devantler-tech/platform#6177). `scripts/publishing-contract.test.sh` refuses any workflow that
  builds or deploys a Pages site. Both former Pages publishers remain only at historical
  immutable Git revisions.
- New architecture decisions belong only in `docs/adr/`. Existing monorepo decisions remain
  in their original repository; the source snapshot and asset provenance are linked in the README.

## Maintenance

Use the Node line in `.node-version` (24), npm 11 and `LC_ALL=C`. Both package engines and the
candidate's Node type definitions must match that line; application workflows read the same file.
The immutable shared release-only workflow separately uses Node 22 and aligns npm 11 before
running semantic-release. This is not an application build or a relaxation of its toolchain.
From this repository's root:

```sh
npm ci
npm --prefix web ci
npm run build
node --test scripts/public-build.test.mjs scripts/public-build-contract.test.mjs
bash scripts/check-svelte-frontend.sh
# Also requires the exact Go compiler and BOTH disposable database fixture URLs
# from server/README.md; missing database proof fails, never skips:
bash scripts/check-go-contact.sh
node --test scripts/go-contact.test.mjs
node --test scripts/identity-cohorts.test.mjs
bash scripts/npm-toolchain.test.sh
bash scripts/dependency-overrides.test.sh
bash scripts/audit-dependencies.test.sh
bash scripts/scan-go-source.test.sh
bash scripts/publishing-contract.test.sh
bash scripts/runtime-release.test.sh
bash scripts/runtime-verification.test.sh
bash scripts/runtime-publisher-contract.test.sh
node --test scripts/tenant-package.test.mjs
# Actual Docker runtime check; CI supplies its newly built production image:
node scripts/tenant-runtime-smoke.test.mjs
bash scripts/package-go-notices.test.sh
bash scripts/check-active-projects-drift.test.sh
bash scripts/check-cv-drift.test.sh
node scripts/check-cv-drift.mjs src/content/docs/about.mdx src/data/cv.ts
```

Audit dependency changes with `scripts/audit-dependencies.sh` from the repository root. The real
cross-product drift check runs in the monorepo against its pinned site and maintained `.github` catalogue;
standalone CI exercises the checker's hermetic fixtures and the actual CV. Do not substitute
fixtures for the aggregator's cross-product validation or weaken a failing check.

Exercise English/Danish Home, About and Projects, journal and technical navigation, old
project redirects/bookmarks, the CV, keyboard disclosures, mobile layouts and synchronized
System/Light/Dark appearance before promotion. Stop any local preview server after checking it.
`CI - Required Checks` aggregates every applicable CI job with `always()`. Merge is squash,
pinned to the reviewed head, after the repository's ordered review lanes and complete readiness
preflight; a source merge is not publication. Visitors see a change only after a runtime release
(`release-runtime.yaml`, then `publish-runtime.yaml`) and the platform's rollout of it. GitHub
Pages publication is retired; see docs/adr/0003-source-owned-publication.md.

## Content boundaries

Read `docs/README.md` before editing public content. Pages and journal posts live in
`src/content/docs/`; this authored content collection is not the contributor-docs directory.
Edit bilingual catalogue descriptions in `src/data/public-products.json`; set changes must also
update the inventory in `src/content/docs/projects/active.mdx` and the dated stars snapshot.
Keep KSail brief and linked to ksail.devantler.tech rather than duplicating its documentation.
`src/data/cv.ts` is the CV source; builds generate the PDF.

Write concise, reader-facing present-state copy, verified outcomes and a clear next step.
Follow the editorial standard for posts and material updates; never invent experience or numbers.
Unreleased UI/content stays behind a default-off server-only Svelte release flag until reviewed activation
and live proof; remove the flag once shipped. Never hand-edit generated output or edit a journal
post during a project-description sync.

Describe an honest one-person business. Do not invent company/contact/customer facts, reveal a
private address, claim statutory compliance, or add purchases/subscriptions. Confirmed company
identity lives in `src/data/company.json`: Devantler Tech, CVR 46830385, an ordinarily
VAT-registered sole proprietorship, Nikolai Emil Damm and `ned@devantler.tech`. The maintainer
confirmed the VAT effective date as 20 October 2026; this is owner confirmation, not an agent's
independent registry inspection. Registration/contact follow-up
[monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917) remains open for the public
required address disclosure and deployed English/Danish proof. The maintainer has confirmed
mail receipt and allows the registered address on the website only where legally required;
keep the street address out of public backlog evidence. The default-off
`FEATURE_COMPANY_IDENTITY` source default keeps ordinary local builds closed; the
production-template packager explicitly enables the footer, email route and required disclosure,
whose address the runtime reads from its private projected file.
The same release enables the EN/DA privacy notice and customer terms (`/privacy/`, `/terms/`),
whose copy lives in `src/components/business/legal-copy.ts`. The privacy notice's portal
section stays behind the default-off `FEATURE_PORTAL_PRIVACY`, and the standard add-ons list on the
home page behind the default-off `FEATURE_ADDONS`, until the portal is activated;
the activation flips their `false` pins in `scripts/package-runtime.sh` and
`scripts/vat-activation.test.mjs`. They link to the address rather
than repeat it. Change them with any change to hosting, providers, retention, prices or terms
(#19); never call the business compliant.
Publish the maintainer's fixed VAT-inclusive prices (15 November 2026): DKK 6,000 plus optional
hosting 300/month for a website, 15,000 plus 800/month for a focused first app version, and
9,500 plus 500/month for a service. Each total includes 25% Danish VAT; never add VAT again.
No hourly rates: work beyond the agreed scope gets its own fixed-price quote, and the first
consultation is free. The launch offer (`launchDiscount` in `business-copy.ts` and
`server/internal/identity/offerings.json`) gives the first 50 customers who order with hosting
25% off the one-time price only; remove it from the site when the 50 places are taken.
VAT-inclusive prices and current company facts are permanent in the frontend and every package
profile. The production-template packager enables company identity and
the reviewed private/business customer invitation for Denmark and the EU; revised-offer copy stays
disabled. Scope, price and applicable VAT are confirmed before agreement, and outside-EU projects
require a separate assessment before any offer. Every package uses the same approved price state;
no historical no-VAT setting remains.
This public invitation does not authorize portal orders or change server-side country eligibility.
Monthly quotes are rounded totals based on hosting costs plus a fixed service fee to Nikolai;
amounts and any changes are agreed before work begins. The portal's generated draft starts from
the guide prices (ADR 0030); the operator reviews it before sharing, and the customer accepts it.
The standard add-ons in `server/internal/identity/offerings.json` are the only extras with fixed
prices; anything else is a fixed quote. When a customer asks for something outside the offer, file
or bump an `addon-candidate` issue that describes only the request, never the customer.
The revised offer is preview-only under default-off `FEATURE_OFFER_COPY`. Hosted client projects
include optimization, accessibility improvements, bug fixes and routine upkeep in their agreed
monthly price; new features and larger iterations require a new agreement. Human support is not
24/7 and no universal incident-recovery guarantee is offered. Keep the published alternatives
until reviewed adoption, activation and bilingual live proof; retire the gate with #18.
Preserve real portrait/screenshots/diagrams and generated-art provenance.

Scripts are Bash or Go, never Python. Stage explicit paths; never discard another session's work.
