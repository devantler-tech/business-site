# AGENTS.md — business-site

Devantler Tech's public English/Danish business website, Projects catalogue, journal and
supporting technical pages. This file is canonical; the monorepo's reviewed
[shared engineering contract](https://github.com/devantler-tech/monorepo/blob/main/AGENTS.md)
also applies. Use isolated worktrees, signed commits, issue-backed draft PRs and current-head
review/CI/user-path evidence. Never use an admin merge bypass or request Copilot review.

## Layout and ownership

- The Astro + Starlight application lives at the repository root: `src/`,
  `public/`, `scripts/` and root package/configuration files.
- `web/` is the isolated SvelteKit migration candidate for #124/#139. Its framework-native
  `FEATURE_SVELTE_FRONTEND` setting is default-off and admits bilingual Home, About and Projects output,
  with the two historical English project redirects, the English Journal's paginated main archive,
  complete unpaginated topic and author archives,
  and one complete reading route, `/blog/building-ksail-from-shell-to-dotnet-to-go/`.
  Follow `web/README.md`; do not point the publisher or Go package at it in this slice.
- Framework-neutral homepage and quality copy lives in `business-copy.ts` and `client-copy.ts`
  under `src/components/business/`; the existing Astro adapters retain its release configuration.
  The Svelte candidate uses published offer copy and LinkedIn contact, never company/private inputs.
- Framework-neutral portfolio descriptions, presentation and bookmark aliases live in
  `client-products.ts` and `portfolio-content.ts`; both frontends use the same ranking implementation
  and complete dated snapshot. Projects also uses the shared bookmark-disclosure controller.
- The Journal candidate reads the actual Astro collection at build time through `src/data/journal.mjs`,
  using Astro's frontmatter parser and the published excerpt renderer. Both frames share archive
  configuration, introductory copy and the native-link card controller. Reading routes and RSS
  remain on the published Astro site except that one admitted article. Its body reuses an
  input-bound generated Astro reference; run `scripts/build-journal-reference.sh` before standalone
  enabled candidate builds. Normal production builds and publication do not consume that reference.
  Never invent Danish article routes.
- `docs/README.md` contains the editorial, localization, catalogue and feature-flag standards.
- `server/` contains the default-off Go/PostgreSQL browser identity boundary. Read
  `server/AGENTS.md` before changing it. Its handlers are not deployed by Pages;
  production authentication, customer intake and payments remain disabled.
- `.github/workflows/ci.yaml` validates source and uploads a downloadable production preview.
- `.github/workflows/publish-site.yaml` owns main/manual/scheduled publication after the
  reviewed #25 cutover; production deployment still requires the enabled source switch and
  protected main-only environment. Manual artifact-only previews can run with the switch off.
  `publish-pages.yaml` retains legacy immutable callers, not live ownership.
  This static publisher does not deploy a private portal
  backend, identity, customer isolation, checkout or paid subscription system.
- New architecture decisions belong only in `docs/adr/`. Existing monorepo decisions remain
  in their original repository; the source snapshot and asset provenance are linked in the README.

## Maintenance

Use the Node line in `.node-version` (24), npm 11 and `LC_ALL=C`. Both package engines and the
candidate's Node type definitions must match that line; all workflows read the same file.
From this repository's root:

```sh
npm ci
npm run build
npm --prefix web ci
bash scripts/check-svelte-frontend.sh
bash scripts/npm-toolchain.test.sh
bash scripts/dependency-overrides.test.sh
bash scripts/audit-dependencies.test.sh
bash scripts/publishing-contract.test.sh
bash scripts/site-publication.test.sh
bash scripts/package-go-notices.test.sh
bash scripts/check-active-projects-drift.test.sh
bash scripts/check-cv-drift.test.sh
node scripts/check-cv-drift.mjs src/content/docs/about.mdx src/data/cv.ts
```

Audit dependency changes with `scripts/audit-dependencies.sh` from the repository root. The real
cross-product drift check runs in the monorepo against its pinned site and Actions products;
standalone CI exercises the checker's hermetic fixtures and the actual CV. Do not substitute
fixtures for the aggregator's cross-product validation or weaken a failing check.

Exercise English/Danish Home, About and Projects, journal and technical navigation, old
project redirects/bookmarks, the CV, keyboard disclosures, mobile layouts and synchronized
System/Light/Dark appearance before promotion. Stop any local preview server after checking it.
`CI - Required Checks` aggregates every applicable CI job with `always()`. Merge is squash,
pinned to the reviewed head, after the repository's ordered review lanes and complete readiness
preflight; a source merge is not publication. The #25 source-owned publication cutover is complete;
production deployment still uses the enabled source switch and protected main-only environment;
manual artifact-only previews do not require the production switch.
See docs/adr/0003-source-owned-publication.md.

## Content boundaries

Read `docs/README.md` before editing public content. Pages and journal posts live in
`src/content/docs/`; this inner Starlight collection is not the contributor-docs directory.
Edit bilingual catalogue descriptions in `src/data/public-products.json`; set changes must also
update the inventory in `src/content/docs/projects/active.mdx` and the dated stars snapshot.
Keep KSail brief and linked to ksail.devantler.tech rather than duplicating its documentation.
`src/data/cv.ts` is the CV source; builds generate the PDF.

Write concise, reader-facing present-state copy, verified outcomes and a clear next step.
Follow the editorial standard for posts and material updates; never invent experience or numbers.
Unreleased UI/content stays behind a default-off `astro:env` release flag until reviewed activation
and live proof; remove the flag once shipped. Never hand-edit generated output or edit a journal
post during a project-description sync.

Describe an honest one-person business. Do not invent company/contact/customer facts, reveal a
private address, claim statutory compliance, or add purchases/subscriptions. Confirmed company
identity lives in `src/data/company.json`: Devantler Tech, CVR 46830385, PMV, Nikolai Emil Damm
and `ned@devantler.tech`. Registration/contact follow-up
[monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917) remains open for the public
required address disclosure and deployed English/Danish proof. The maintainer has confirmed
mail receipt and allows the registered address on the website only where legally required;
keep the street address out of public backlog evidence. The default-off
`FEATURE_COMPANY_IDENTITY` source default keeps ordinary local builds closed; the reviewed
source-owned publisher explicitly enables the footer, email route and required disclosure.
Keep the introductory guides at DKK 2,995 plus optional hosting 99/month for a website, 7,995
plus 299/month for a focused first app version, and 4,995 plus 199/month for a service.
These starting prices do not cap application size: larger builds use agreed, priced iterations.
The VAT-inclusive guides decided in #154 (3,750 + 125, 10,000 + 375 and 6,250 + 250 DKK) are
preview-only under default-off `FEATURE_VAT_PRICES`; keep the prices above published until the VAT
registration's effective date is confirmed and that switch is reviewed. Never add VAT to them again.
Monthly quotes are rounded totals based on hosting costs plus a fixed service fee to Nikolai;
amounts and any changes are agreed before work begins, never inferred from the starter guide.
The revised offer is preview-only under default-off `FEATURE_OFFER_COPY`. Hosted client projects
include optimization, accessibility improvements, bug fixes and routine upkeep in their agreed
monthly price; new features and larger iterations require a new agreement. Human support is not
24/7 and no universal incident-recovery guarantee is offered. Keep the published alternatives
until reviewed adoption, activation and bilingual live proof; retire the gate with #18.
Preserve real portrait/screenshots/diagrams and generated-art provenance.

Scripts are Bash or Go, never Python. Stage explicit paths; never discard another session's work.
