# AGENTS.md — business-site

Devantler Tech's public English/Danish business website, Projects catalogue, journal and
supporting technical pages. This file is canonical; the monorepo's reviewed
[shared engineering contract](https://github.com/devantler-tech/monorepo/blob/main/AGENTS.md)
also applies. Use isolated worktrees, signed commits, issue-backed draft PRs and current-head
review/CI/user-path evidence. Never use an admin merge bypass or request Copilot review.

## Layout and ownership

- The Astro + Starlight application lives at the repository root: `src/`,
  `public/`, `scripts/` and root package/configuration files.
- `web/` is the SvelteKit public frontend. Its framework-native
  `FEATURE_SVELTE_FRONTEND` setting is default-off and admits bilingual Home, About and Projects output,
  with the two historical English project redirects, the English Journal's paginated main archive,
  complete unpaginated topic and author archives,
  all 15 complete Journal reading routes, and the six existing English technical guides,
  including locally rendered diagrams and the complete template collection. Enabled company
  identity also admits the two English/Danish company-information pages, with the address
  confined to those disclosures and omitted when identity is disabled. The legacy
  GitOps tenant-template URL redirects locally, preserving incoming heading bookmarks.
  Follow `web/README.md`. The normal build selects the verified complete candidate only when
  explicitly enabled and writes the same canonical `dist/` as the incumbent build. Production
  publication explicitly selects Svelte, with main-only artifact previews and explicit Astro
  recovery. Production-profile Go packaging also selects Svelte; the ordinary package remains
  an Astro comparison. Only Svelte packages use the explicit `--svelte` option; omit it for the
  ordinary Astro comparison. Both ignore ambient frontend settings.
- Framework-neutral homepage and quality copy lives in `business-copy.ts` and `client-copy.ts`
  under `src/components/business/`; the existing Astro adapters retain its release configuration.
  The Svelte candidate preserves the four default-off company/offer/customer-scope/VAT settings
  through its native server-only configuration. Shared layout/load data never carries the address;
  the company-page HTML transform retains escaped text and the runtime's template placeholders.
- Framework-neutral portfolio descriptions, presentation and bookmark aliases live in
  `client-products.ts` and `portfolio-content.ts`; both frontends use the same ranking implementation
  and complete dated snapshot. Projects also uses the shared bookmark-disclosure controller.
- The Journal candidate reads the actual Astro collection at build time through `src/data/journal.mjs`,
  using Astro's frontmatter parser and the published excerpt renderer. Both frames share archive
  configuration, introductory copy and the native-link card controller. Technical routes
  reuse their maintained Astro-rendered bodies, bookmarks, native code controls and table semantics;
  only the original English routes are admitted. Admitted bodies reuse an
  input-bound generated Astro reference; run `scripts/build-journal-reference.sh` before standalone
  enabled candidate builds. The selected normal build prepares it automatically through the explicit
  incumbent builder, never recursively through the normal publication entrypoint.
  The reference also bundles the existing Mermaid dependency with the candidate's Vite toolchain.
  Only diagram articles load that local module graph; ordinary code retains its separate copy controls.
  Numeric archives and article slugs share explicit dispatch; unsupported nested article routes
  fail the build. Never invent Danish article routes.
  The enabled candidate retains the complete English `/blog/rss.xml` and its canonical image
  dependencies through an input-bound generated reference and native prerendered endpoints.
  Feed XML, images and discovery stay default-off; do not invent a Danish feed or add a browser parser.
  The same input-bound reference captures the generated CV, maintained thesis PDF, favicon,
  robots file and sharing portrait through inspected file descriptors. Native prerendered endpoints
  retain these downloads locally. The final adapter output supplies canonical sitemap membership,
  excluding redirects and noindex disclosures; never copy publisher CNAME or publication receipts.
  All discovery assets and endpoints are absent when the candidate is false or unset.
- `docs/README.md` contains the editorial, localization, catalogue and feature-flag standards.
- `server/` contains the default-off Go/PostgreSQL browser identity boundary. Read
  `server/AGENTS.md` before changing it. Its handlers are not deployed by Pages;
  production authentication, customer intake and payments remain disabled.
  Customer identity source preparation uses dedicated customer Dex and self-hosted
  ZITADEL; read `server/CUSTOMER-IDENTITY.md`. The explicit offline preparer has no
  deployment, user-provisioning or portal-activation authority. Social logins are deferred.
- `scripts/prepare-zitadel-source.sh` explicitly prepares the frozen upstream source
  without the unadmitted Ailerons assets. Read `docs/ZITADEL-SOURCE.md`; the offline
  output and receipt are not a build, complete license admission or image release.
- `scripts/build-zitadel-linux.sh` explicitly builds that source on a clean Linux
  amd64 host with its frozen upstream toolchain. Ordinary builds never invoke it.
  CI retains build diagnostics only; successful compilation is not image admission,
  distribution clearance or portal activation. Read the same source/build guide.
- `scripts/zitadel-login-icons.go` explicitly verifies the selected Login V1
  fonts, decoded statik members and retained notices. It is inactive without
  arguments and is mandatory in the Linux build, not the public website build.
- Private portal presentation uses Go templates and native forms. Contact responses
  buffer the complete private frame and revalidate the original durable session/client
  after SDK waits. The separate mandatory `contactintegration` suite exercises the
  retained HTTP journey and authority checkpoints without a Node worker. ADR 0017
  records the rejected private-frontend experiment; public Svelte remains selected.
- `.github/workflows/ci.yaml` validates source and uploads a downloadable production preview.
- `deploy/` is a dormant public-only Kustomize package using the namespace-bound
  Platform WebApp contract; read `deploy/README.md` before changing it. It has no
  registered consumer, proposes only `business-site.platform.devantler.tech` for public staging,
  and grants no portal activation. Platform hostname approval and registration remain separate.
  The source-owned runtime release/publisher callers are default-off behind the
  explicit `RUNTIME_PUBLICATION_ENABLED` repository variable. Original main-CI
  archive admission, the accepted original-image verifier and independent paired
  publication readback are mandatory. Keep private dependencies and migration inputs absent.
- `.github/workflows/publish-site.yaml` owns main/manual/scheduled publication after the
  reviewed #25 cutover; production deployment still requires the enabled source switch and
  protected main-only environment. Manual artifact-only previews can run with the switch off.
  The retired reusable publisher is available only at historical immutable Git revisions.
  This static publisher does not deploy a private portal
  backend, identity, customer isolation, checkout or paid subscription system.
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
bash scripts/publishing-contract.test.sh
bash scripts/site-publication.test.sh
bash scripts/runtime-release.test.sh
bash scripts/runtime-verification.test.sh
bash scripts/runtime-publisher-contract.test.sh
node --test scripts/tenant-package.test.mjs
# Actual Docker runtime check; CI supplies its newly built production image:
node scripts/tenant-runtime-smoke.test.mjs
bash scripts/package-go-notices.test.sh
bash scripts/prepare-zitadel-source.test.sh
bash scripts/build-zitadel-linux.test.sh
bash scripts/zitadel-build-contract.test.sh
(cd scripts && go test -race zitadel-login-icons.go zitadel-login-icons_test.go)
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
identity lives in `src/data/company.json`: Devantler Tech, CVR 46830385, an ordinarily
VAT-registered sole proprietorship, Nikolai Emil Damm and `ned@devantler.tech`. The maintainer
confirmed the VAT effective date as 20 October 2026; this is owner confirmation, not an agent's
independent registry inspection. Registration/contact follow-up
[monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917) remains open for the public
required address disclosure and deployed English/Danish proof. The maintainer has confirmed
mail receipt and allows the registered address on the website only where legally required;
keep the street address out of public backlog evidence. The default-off
`FEATURE_COMPANY_IDENTITY` source default keeps ordinary local builds closed; the reviewed
source-owned publisher explicitly enables the footer, email route and required disclosure.
Publish the approved VAT-inclusive introductory guides at DKK 3,750 plus optional hosting
125/month for a website, 10,000 plus 375/month for a focused first app version, and 6,250
plus 250/month for a service. Each total includes 25% Danish VAT and preserves the previous
net-price floor; never add VAT again.
These starting prices do not cap application size: larger builds use agreed, priced iterations.
The source-owned publisher and production-template packager pin `FEATURE_VAT_PRICES=true`,
company identity enabled, and revised-offer/customer-scope releases disabled. Both the selected
Svelte frontend and explicit Astro recovery must retain those current facts. VAT=false/unset
selects explicitly historical PMV comparison inputs, not a valid current-production rollback.
Retire the temporary VAT gate only after reviewed publication and actual bilingual live proof.
This does not activate portal orders or expand eligible customer countries.
Monthly quotes are rounded totals based on hosting costs plus a fixed service fee to Nikolai;
amounts and any changes are agreed before work begins, never inferred from the starter guide.
The revised offer is preview-only under default-off `FEATURE_OFFER_COPY`. Hosted client projects
include optimization, accessibility improvements, bug fixes and routine upkeep in their agreed
monthly price; new features and larger iterations require a new agreement. Human support is not
24/7 and no universal incident-recovery guarantee is offered. Keep the published alternatives
until reviewed adoption, activation and bilingual live proof; retire the gate with #18.
Preserve real portrait/screenshots/diagrams and generated-art provenance.

Scripts are Bash or Go, never Python. Stage explicit paths; never discard another session's work.
