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
  The Svelte candidate preserves the two default-off company/offer settings
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
- `third_party/zitadel-build-tools/scope.json` defines the exact 37 authored
  identity-build files and three license documents granted under AGPL-3.0-only.
  No directory-wide, whole-module or other application license is implied.
  Preserve its closed scope, private SDK metadata and literal terms; source capture
  and distribution admission remain separate. The CI-only scope checker and tests
  are outside that grant and never activate or publish an identity.
- `server/` contains the default-off Go/PostgreSQL browser identity boundary. Read
  `server/AGENTS.md` before changing it. Its handlers are not deployed by Pages;
  production authentication, customer intake and payments remain disabled.
  Customer identity source preparation uses dedicated customer Dex and self-hosted
  ZITADEL; read `server/CUSTOMER-IDENTITY.md`. The explicit offline preparer has no
  deployment, user-provisioning or portal-activation authority. Social logins are deferred.
- `scripts/prepare-zitadel-source.sh` explicitly prepares the frozen upstream source
  without the unadmitted Ailerons assets or JSONPath machine-ID selector. Configured
  metadata selectors fail explicitly; the native preparation test uses Go 1.26.10.
  Read `docs/ZITADEL-SOURCE.md`; the offline
  output and receipt are not a build, complete license admission or image release.
- `scripts/build-zitadel-linux.sh` explicitly builds that source on a clean Linux
  amd64 host with its frozen upstream toolchain. Ordinary builds never invoke it.
  CI retains build diagnostics only; successful compilation is not image admission,
  distribution clearance or portal activation. Read the same source/build guide.
- `scripts/zitadel-source-bundle.go` is an explicit, otherwise inert source-input
  snapshot/archive verifier. Its prepared-source operation admits only the complete
  frozen tree after API serialization, preserving empty files and executable modes.
  Archive replay checks closed member identities without executing source. It does
  not yet supply dependency acquisition, native offline compilation or publication.
  The Linux builder's explicit `--capture-source` option captures before install,
  retains twenty-eight literal build/modification records and rechecks the snapshot/archive
  before binding their identities to the completed binary's build receipt. CI
  unpacks that actual archive; diagnostics exclude its full source bytes.
  Its explicit `--restore-build` operation restores the sealed files and literal
  records into a new native layout. The Linux builder's mutually exclusive
  `--prepared-source`/`--prepared-sha256` mode reuses the existing frozen capture
  admission before entering the unchanged build phases and joins the final recipe
  hash to that admitted input. CI rebuilds from the unpacked source and retains
  its joined receipt; this connected rebuild is not
  complete dependency capture or native offline replay. Keep both modes' private
  Go/pnpm/schema acquisition caches, empty schema credentials/configuration,
  public checksum verification and confined records.
- `scripts/replay-zitadel-inputs.sh` explicitly assembles only the seven sealed
  input components and their original build receipt using the archive engine's
  distinct retained-input profile. That profile preserves private capsule modes
  and cannot impersonate prepared source. The Linux builder's exclusive
  `--retained-inputs`/`--retained-sha256` mode authenticates and reconstructs native
  Go, pnpm and schema stores before the existing full pipeline. Only the outer
  replay command may emit a whole-build success receipt, after that pipeline and
  same-build notices finish in a separate network namespace as the original
  nonroot user, with no new privileges and all capabilities dropped. Component
  receipts remain scoped and false for whole-build proof. CI retains compact
  evidence only; image/source-offer admission and portal activation stay separate.
- `server/cmd/zitadel-module-sources/` is an otherwise inert build command using
  checksum-pinned `golang.org/x/mod` for complete module ZIP/content validation.
  It belongs to the existing Go 1.27.3 module and its standard validation. CI
  builds it before selecting the unchanged frozen identity compiler. The Linux
  builder requires its explicit executable path when source capture is enabled
  and binds its compiler, command identity and byte digest to the receipt.
  Explicit capture retains the frozen service's 231 original/effective selections
  and 466 source/metadata files from that build's private cache. The root command's
  complete dependency set must equal the compiler stream. External-seal readback
  rejects changed, missing, extra, linked and wrong-mode inputs; CI verifies an
  actual fresh copy. Only compact manifests/diagnostics are uploaded. This is
  selected service-Go retention, not generator-tool/pnpm/BSR capture, complete
  corresponding-source admission, offline compilation or portal activation.
- `scripts/zitadel-login-icons.go` explicitly verifies the selected Login V1
  fonts, decoded statik members and retained notices. It is inactive without
  arguments and is mandatory in the Linux build, not the public website build.
  Compile its `zitadel-text-fonts.go` companion too. Explicit `--text-fonts`
  snapshot/capture/read-only verification preserves the 54 frozen text fonts,
  84 actual copies and original attribution; its raw text and inventory enter
  image packaging and retained-input replay. This does not admit an image or
  activate the portal; read ADR 0025 and the source guide before changing it.
  The `--copied-assets` companion also closes 1,858 icon/flag/translation originals,
  3,716 generated copies and their actual Console embeds. Translation admission
  retains the complete frozen license, licensing scope and contribution context,
  checks strict UTF-8 JSON objects and keeps the full translation bodies in source,
  rather than duplicating them into notice text. Read ADR 0026 before changing it.
- `server/cmd/zitadel-bsr-sources/` is an otherwise inert, standard-library build
  command for the frozen three-module schema profile; read its README. It verifies
  native SHAKE256 content, both cache/export representations and a closed externally
  sealed private package, then restores/checks only verified native cache bytes.
  Normal complete server tests include it. Explicit Linux source capture requires
  this command's built path, retains the actual native API/console compiler inputs
  and proves schema-only replay from fresh restored caches without networking or
  privilege recovery. Complete dependency/native service replay remains #343;
  this command grants no deployment authority.
- `scripts/zitadel-console-assets.go` is an explicit, otherwise inert collector
  and verifier for the frozen Linux console generator's three original release
  downloads. It preserves their compressed bytes and checksums in a closed,
  externally sealed private directory. Its fetch mode emits only verified bytes
  without a downloader; failed captures retain bounded, unsealed diagnostics and
  never delete a pathname that may have been replaced. Explicit source capture and
  prepared replay with a sealed console input use its console-only native request
  adapter. Preserve the exact ordered three requests, private new destinations,
  native checksum/unzip/install path and complete post-phase readback. Its source
  is one of the retained build records. Complete dependency capture and service
  replay remain incomplete under #343. Read `docs/ZITADEL-SOURCE.md`.
- `tools/zitadel-pnpm-source/` supplies the pinned native SDK and explicit private
  capture/verify/restore boundary for the actual filtered Linux installation.
  Read its README before changing it. Preserve native peer/optional/workspace
  selection, complete original archive SRI, independent CAFS byte checks and
  native lifecycle classification. Restoration preserves verified literal indexes;
  it never transplants old installation metadata into the application. The
  adapter and both SDK manifests are retained source/build records. Capture and
  reconstruction do not establish an offline install or complete native replay.
- `scripts/zitadel-console-notices.go` captures the actual Angular notice text
  and its same-build bindings. The explicit Linux recipe brackets compilation
  with a checked snapshot; raw diagnostic capture is not complete license admission.
  Its explicit `--toolchain` input also retains the selected Go distribution's
  LICENSE, PATENTS and VERSION in the Linux recipe, not the public website build.
  It checks the frozen profile's complete 16-package compiler-vendor selection
  against that actual distribution and retains all eight vendor term identities
  through checked, byte-equal root spans. Normal unflagged capture remains inert.
  Its explicit `--modules` input retains all selected dependency root components
  plus eleven curated nested notices. A nested component must name its actual
  compiler-selected package, with an exact import-directory and cache binding.
  The same catalogue retains twelve selected source identities and seventeen
  attribution/origin spans, checked against whole-file hashes and actual GoFiles.
  It retains reviewed external terms, binds the PSL embeds and ignored generator,
  and verifies the historical PSL source without emitting its data as diagnostics.
  Read `third_party/zitadel-go-module-notices/README.md` for provenance and limits.
  The explicit `--source-license` input retains the complete frozen root LICENSE
  from this build's prepared source; it is required in both Linux capture phases.
  This is selected text retention, not full distribution or image admission.
  Its explicit `--verify` mode rechecks a completed build and all retained diagnostics
  without rewriting them. It requires the final inventory and build receipt.
  Its explicit `--supplement` input retains eight selected packages' complete
  notice components separately, without changing generated Angular text. Read
  `third_party/zitadel-console-notices/README.md` for exact provenance and limits.
  The opentype.js component checks one exact decoded source-map notice; logical
  source identifiers are inert data, never paths to read or URLs to fetch.
- `Dockerfile.zitadel` and `scripts/zitadel-{image-package,image-smoke,service-smoke}.go` assemble an
  explicit local identity image and exercise its real version command. CI
  checks a closed package, the copied-back image payload and inspected container
  restrictions, then removes only its owner-bound container and local image. The bounded
  diagnostic is `cli-version-smoke`, with admission and licence closure unknown.
  The separate explicit service profile also cold-starts the same image with an
  ephemeral fictional PostgreSQL fixture on an internal Docker network, publishes
  no host ports and reads actual Linux process restrictions and RSS/peak
  measurements. Preserve its closed network, writable-path and owner-bound cleanup
  controls. HTTP connects only to the inspected owned private endpoint; its fictional
  authority is never resolved through DNS and redirects cannot leave that authority.
  Read the actual process mount table too; Docker's legacy tmpfs option is not
  guaranteed to appear among regular mount points. Keep the kernel read-only
  root and bounded, non-executable, owner-bound tmpfs admission mandatory.
  HTTP discovery/console availability is not browser, invitation, MFA, production
  TLS, complete-stack acceptance or activation. No image is published.
  The separately explicit `--browser-smoke` profile also uses the runner's Chrome
  and matching WebDriver inside the owned service network namespace as a non-root
  user. Its native browser sandbox remains enabled. It exercises only fictional
  Login V1 and authenticated Console presentation, with source/image-bound receipts
  and screenshots; it grants no production login, mail, invitation or activation
  clearance. The Go browser worker is reachable only through the explicit profile.
  No arguments are inert; the real smoke entrypoint requires native Linux amd64
  and Go 1.26.10. Read `docs/ZITADEL-SOURCE.md` before using it.
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
bash scripts/scan-go-source.test.sh
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
bash scripts/zitadel-build-tools-license.test.sh
bash scripts/build-zitadel-linux.test.sh
bash scripts/zitadel-build-contract.test.sh
(cd scripts && go test -race zitadel-login-icons.go zitadel-text-fonts.go zitadel-copied-assets.go zitadel-login-icons_test.go zitadel-text-fonts_test.go zitadel-copied-assets_test.go)
(cd scripts && go test -race zitadel-console-notices.go zitadel-console-notices_test.go zitadel-console-supplement_test.go zitadel-toolchain-notices_test.go zitadel-source-notices_test.go)
(cd scripts && go test -race zitadel-image-package.go zitadel-image-smoke.go zitadel-service-smoke.go zitadel-browser.go zitadel-image-package_test.go zitadel-image-smoke_test.go zitadel-service-smoke_test.go zitadel-browser_test.go)
(cd scripts && go test -race zitadel-source-bundle.go zitadel-source-bundle_test.go)
(cd scripts && go test -race zitadel-console-assets.go zitadel-console-assets_test.go)
npm ci --prefix tools/zitadel-pnpm-source --ignore-scripts
node --test tools/zitadel-pnpm-source/index.test.mjs
(cd server && GOTOOLCHAIN=local go test -race ./cmd/zitadel-module-sources)
(cd server && GOTOOLCHAIN=local go test -race ./cmd/zitadel-bsr-sources)
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
VAT-inclusive prices and current company facts are permanent in both frontends and every package
profile. The source-owned publisher and production-template packager enable company identity and
the reviewed private/business customer invitation for Denmark and the EU; revised-offer copy stays
disabled. Scope, price and applicable VAT are confirmed before agreement, and outside-EU projects
require a separate assessment before any offer. The ordinary Astro comparison and explicit
Astro recovery use the same approved price state; no historical no-VAT setting remains.
This public invitation does not authorize portal orders or change server-side country eligibility.
Monthly quotes are rounded totals based on hosting costs plus a fixed service fee to Nikolai;
amounts and any changes are agreed before work begins, never inferred from the starter guide.
The revised offer is preview-only under default-off `FEATURE_OFFER_COPY`. Hosted client projects
include optimization, accessibility improvements, bug fixes and routine upkeep in their agreed
monthly price; new features and larger iterations require a new agreement. Human support is not
24/7 and no universal incident-recovery guarantee is offered. Keep the published alternatives
until reviewed adoption, activation and bilingual live proof; retire the gate with #18.
Preserve real portrait/screenshots/diagrams and generated-art provenance.

Scripts are Bash or Go, never Python. Stage explicit paths; never discard another session's work.
