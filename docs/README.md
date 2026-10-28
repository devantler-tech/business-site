# Business-site contributor reference

This directory holds contributor documentation and architecture decisions, not the Astro
application or rendered Starlight content. Start with the [project README](../README.md) for
local first use and [CONTRIBUTING.md](../CONTRIBUTING.md) for the complete contribution path.
The reference below covers editorial standards, bilingual content, catalogue maintenance,
CV data and build-time previews. Application paths are relative to the repository root.

The source-owned `.github/workflows/publish-site.yaml` builds this repository's exact `main`
revision and publishes to its own Pages resource at devantler.tech when its admission gates
pass. It is an ordinary push/manual/scheduled workflow, not a callable reusable workflow.
Historical immutable callers retain their Git revision; no legacy publisher is exported on
the default branch. The current publisher does not deploy the private portal backend.

## Develop

```sh
# From the repository root
npm ci
npm --prefix web ci
npm run dev -- --host 127.0.0.1  # stop with Ctrl+C
LC_ALL=C npm run build          # production build and visitor checks
```

Use the Node line declared in `.node-version` (24) and npm 11. Workflows and the runtime packager
read that declaration; package engine ranges and candidate Node types are checked against it.
CI clean-installs with npm 11.4.2 before repeating the install
with the runner's current npm 11. This also checks older supported versions: they
require a nested optional Markdown peer that newer npm releases can omit when
generating a lockfile. For intentional dependency changes, regenerate rather than
editing the lockfile:

```sh
npx --yes --package=npm@11.4.2 npm install --package-lock-only --ignore-scripts
```

Then run `npx --yes --package=npm@11.4.2 npm ci --ignore-scripts`, ordinary `npm ci` and the build
from the repository root. A newer generator's output is acceptable only when it passes the same
clean-install checks. These commands change local dependencies/lockfile, not the published site.

## Business website

The business homepage is rendered by `src/components/business/BusinessSite.astro` at `/` (English)
and `/da/` (Danish). Its offer amounts and translated copy live together in
`src/components/business/business-copy.ts`; `content.ts` applies Astro's release configuration.
Plain-language quality copy lives in `client-copy.ts`, with its Astro adapter in `client-facing.ts`.
These framework-neutral sources are also used by the isolated Svelte Home candidate; its matching
server-only company/offer configuration and permanent customer invitation are documented in `web/README.md`.
The Svelte Projects candidate also shares `client-products.ts`, `portfolio-content.ts`,
the existing ranking implementation and complete dated snapshot with Astro. It preserves
the curated six-tool output, compact public website example, closed historical links and old bookmarks
without changing the source-owned publisher. Its enabled/false/unset and actual snapshot-path
checks are included in `scripts/check-svelte-frontend.sh`.
Prices are introductory guides, not an automatic checkout:
project scope, hosting capacity, external fees and support are agreed in a written proposal.
The app guide covers a focused first version, not a ceiling on application size. Larger builds
use separately agreed iterations with their own scope, schedule and price. Monthly quotes are
rounded totals based on hosting costs plus a fixed service fee to Nikolai, with amounts and
changes agreed before work begins; the site does not invent a fee amount or rounding increment.

`FEATURE_OFFER_COPY=true` previews the revised offer from #15, including larger agreed iterations
and the clarified maintenance terms. Every client project hosted on the platform includes
optimization, accessibility improvements, bug fixes and routine upkeep in its agreed monthly price.
New features and larger iterations require a new agreement. Human support terms are agreed per
project; tailored automation helps with supported incidents without promising recovery from every
incident or 24/7 personal availability. This flag defaults to false: Home, About and Projects retain
their published English/Danish alternatives until reviewed activation. CI builds enabled, explicit
false and entirely unset states, and each build checks all six emitted routes. Adoption, activation,
live verification and removal of the temporary gate are tracked in #18.

The VAT-inclusive guide prices decided in #154 are permanent: DKK 3,750 plus 125/month for
a website, 10,000 plus 375/month for a web app and 6,250 plus 250/month for a web service.
Each price is marked "incl. VAT" / "inkl. moms" and the note states the included 25% Danish VAT.
The totals live in `vatInclusiveOffers` in `business-copy.ts`; `scripts/vat-prices.test.mjs`
guards the exact approved totals, net floors and no double VAT. Current ordinary company facts
are shared by both frontends and every package profile. The temporary VAT gate and historical
comparison inputs are removed in source. Reviewed publication and fresh bilingual live verification
remain tracked in #346 until their completion is recorded.
The publisher and production-template packager retain company identity enabled and the
revised-offer release disabled. The customer invitation is permanent. The ordinary Astro comparison
and explicit recovery also retain current prices. This does not activate orders, payments or additional customer eligibility.

The site keeps the original green palette and locally served Matrix artwork. Its appearance
selector offers System, Light and Dark in both languages. The small head script applies the saved
choice before painting, follows system changes in System mode, and shares Starlight's
`starlight-theme` preference with supporting pages. If browser storage is blocked, switching still
works for the current page. Without JavaScript, the page follows the system theme and hides the
inactive selector. `scripts/theme.test.mjs` tests the actual controller as part of every build.

The introduction identifies Nikolai with the existing public `profile.jpg` photograph, biography
and GitHub links. First-person English/Danish copy explains the independent business without
inventing client endorsements; the featured business presentation omits personal/family framing. The real photograph
also supplies the sharing image. Built-page checks verify the portrait and profile journey.

Selected work uses the same `WorkExample.astro` card on Home and Projects in both languages.
AS Coaching og Vaner includes a real public homepage capture, a compact responsive thumbnail
linked to its original local JPEG in a new tab, and a separate link to the public website.
The explicit featured-portfolio rollback shows the Wedding App's documented local guest demo after sign-in, with names, date,
venue/address, countdown values and the venue background removed before capture. The card
explicitly labels it an anonymized demo and does not link to the private invitation site.
No production account or guest invitation code is used. Built-page checks verify the normal
website example and both rollback thumbnails, their readable larger images and localized visit action.

Home puts plain-language quality and security highlights before prices and one compact public website example.
Projects leads with six maintained public tools, followed by the compact website example and hosting.
Earlier project references stay in a closed native disclosure outside the section index.
Home avoids source-code links; Projects offers controlled public project destinations
and wider-work navigation, not raw code files or workflows. The journal and existing technical documentation retain their own detail. The copy
describes verified practices rather than universal coverage, certification, vulnerability-free
software or unlimited maintenance. Project-specific checks and ongoing support remain scoped.

`FEATURE_CLIENT_PORTFOLIO` defaults to true in ordinary production builds.
`FEATURE_CLIENT_PORTFOLIO=false` is a temporary build-time rollback to the previous presentation;
it requires a rebuild and publication, not a browser setting. CI builds enabled, explicit-false
and entirely-unset states and uploads only the final normal production output.
`scripts/check-client-facing.mjs` exercises the emitted EN/DA visitor pages;
`scripts/client-rollout.test.mjs` guards the production default and actual CI invocation.
The source-owned publisher runs after merges to this repository's `main`. A merge still needs a
successful Pages deployment, a matching live source/run receipt and visitor verification before
it counts as published. After live visitor verification, remove this short-lived release flag.

### Featured portfolio (#5)

`FEATURE_FEATURED_PORTFOLIO` defaults to true after the separately reviewed implementation and
activation. `FEATURE_FEATURED_PORTFOLIO=false` is a temporary build-time rollback that requires
rebuilding and republishing, not a browser setting. Normal English/Danish Home and Projects show
the actual public AS Coaching og Vaner website as a compact example and remove personal/family
framing and the anonymous Wedding App demo from these business entrypoints. It does not invent
a paid commission, endorsement or measured client outcome. Original technical inventory and
historical bookmarks remain available; the wider public-work link leads to the organization.

Projects selects exactly six curated maintained tools from complete public GitHub metadata,
ordered by descending stars and repository-name ties. Games, templates, legacy Actions, forks,
archived and private repositories cannot enter this showcase. All twelve entries remain in the
underlying inventory. KSail is labelled source-available with its PolyForm Shield terms, not
unrestricted open source. Stars indicate popularity, not a quality guarantee.

The source-owned publisher refreshes the complete metadata before every build with read-only GitHub
authority; no GitHub API call or token is sent to a visitor's browser. The page displays the actual
UTC observation timestamp. Failed, partial or malformed reads abort publication and leave the
previous live artifact with its previous timestamp; they are never presented as a fresh read.
The source-owned publisher refreshes the ranking on main pushes, admitted manual publications and
its daily schedule. GitHub scheduled runs can be delayed or disabled
after inactivity, so the displayed observation time is the freshness signal, not a promised SLA.
Implementation, activation, caller adoption, bilingual live proof and release-flag cleanup stay
tracked on #5. The disabled Codex chat schedule is unrelated and remains disabled.

Every build runs ranking, rollout and publisher-boundary controls plus emitted bilingual-page
checks. CI builds enabled, explicit false and entirely unset states before uploading normal
production output. Existing quality/security, prices, inquiry, theme, historical-bookmark and image checks
remain active in the new presentation.

The business identity also covers `/about/` and `/projects/`, with Danish counterparts at
`/da/about/` and `/da/projects/`. About introduces the founder of a one-person business; Projects
distinguishes maintained tools and a public website example without inventing client outcomes. The journal and technical
pages reuse the business navigation, typography, colors, footer and appearance control through
Starlight component overrides. Their search, sidebar, RSS and historical articles remain available.
Only Starlight Blog's preview cards receive whole-card mouse navigation. Each native title link
provides the single keyboard destination without making its article a second focus stop. Links,
form controls, disclosure toggles and editable descendants retain their own interaction; individual
journal and technical articles remain reading surfaces. `scripts/journal-navigation.test.mjs`
executes the actual head script as part of every build.
The isolated Svelte candidate shares that controller, archive configuration and introductory copy.
Its main English archive reads the authored Astro collection at build time, with exact generated-card
comparisons across all main, topic and author archive pages. Topic and author archives retain every
matching post on one page, while the main archive retains its five-post pagination. One full reading route, `/blog/building-ksail-from-shell-to-dotnet-to-go/`,
uses an input-bound build-only reference from the maintained Astro renderer. The candidate retains
its complete prose, bookmarks, code/copy controls, metadata, dates and actual local assets, without
a second Markdown renderer. `scripts/build-journal-reference.sh` builds that private generated input;
the normal production publisher neither runs nor consumes it. Topic and author browsing stays local;
author cards retain their separate external profile links. Other reading routes and RSS stay on their
published destinations; Danish business navigation does not invent translated Journal articles.
There is one appearance picker, including on mobile; the documentation header measures its height
so the reading tools do not overlap the business navigation.

All builds use the editorial Journal frame.
Its introduction, lead story, two-column archive and narrower reading pages use the homepage's
type and palette. A native “Browse the Journal” disclosure keeps the existing post/topic menu
available without a permanent documentation sidebar. Search, RSS, authors, tags, pagination,
article bookmarks and copy controls retain their existing behavior. Technical guides keep their
normal Starlight frame. Every build verifies the professional presentation across all generated
Journal routes, including archives, authors, tags and reading pages. CI checks the final production
output before uploading its preview. The source-owned publisher deploys this repository's reviewed
main revision. Presentation rollback requires a reviewed source revert and publication.

Projects presents six stars-ranked maintained tools, followed by one compact public website example
and hosting. KSail's product introduction explains setup, deployment and operation rather
than leading with a dashboard. The real desktop cluster-overview capture is an optional link,
with private details masked and the redaction disclosed in both languages. Historical English
references and diagrams remain in the closed “Earlier project links” disclosure, without a feature
heading, hero or section-index link. Astro recovery renders the original
`src/content/docs/projects/completed.mdx` there. The legacy
active/completed URLs redirect to the public catalogue or historical disclosure of `/projects/`; the
documentation sidebar links only to that canonical page. Browser redirects preserve incoming
heading fragments, which land on the corresponding public tool, website example or research content.
The renamed `/templates/gitops-tenant-template/` entry likewise preserves heading bookmarks through
an authored redirect to the sole Platform Tenant Template guide. Its canonical/noindex metadata,
top-of-guide fallback and no-JavaScript continuation are checked against actual build output.
Only the canonical destination guide is advertised in the sitemap, not the redirect entry.
The deployed-platform bookmark lands on the separate Platform hosting-project example, not the
reusable Platform Template in the public software catalogue. The explicit legacy rollback opens
a bookmarked card's containing disclosure when one exists. Links without a fragment and the no-JavaScript
fallback use the relevant section. Root horizontal overflow is
clipped without creating a non-scrolling ancestor
that would break the documentation header's sticky positioning.

Journal covers and project illustrations use the green/charcoal workshop series in
`src/assets/editorial/`. [Asset provenance](../src/assets/PROVENANCE.md) distinguishes generated
illustrations from the real portrait, product captures and authored diagrams; the complete prompts
are recorded alongside the assets. Every post has its own topic-specific cover, and unrelated
site subjects do not share those covers. Translations, responsive sizes and repeated views of
the same subject may use its same image. Source and emitted-image checks reject duplicated
paths or decoded pixels. Covers do not replace factual inline screenshots or diagrams.

`npm run build` renders the business experience directly and verifies its English/Danish visitor
journeys and supporting pages. The same command is used by CI and GitHub Pages publication.
The core business pages are always rendered. Short-lived presentation flags protect reviewable
redesigns; reverting the publication change and redeploying remains the recovery path.

Business copy describes websites, web apps and services without size-based marketing qualifiers.
Starting prices cover an agreed first version, not a ceiling on eventual application size; further
development is scoped and quoted separately. This neutral wording is shared by published, preview
and rollback presentations. Keep current registration wording consistent across maintained journeys; historical technical
writing retains its own context. The generated Home, About and Projects checks cover both languages and
sharing metadata. The offer-release check distinguishes actual maintenance terms, not app labels
or inquiry wording; the separate revised-hosting activation in #18 is unchanged.

The public customer invitation covers private customers and businesses in Denmark and the rest
of the EU. Scope, price and applicable VAT are confirmed before an agreement. Projects outside
the EU need a separate eligibility assessment before an offer; this is not automatic worldwide
availability. `customer-scope.ts` supplies this boundary to Astro and the Svelte
candidate independently of offer and identity release flags. Following verified source-owned
publication and English/Danish live visits, the invitation is permanent in ordinary, explicitly
false and entirely unset builds. The temporary customer-scope release setting has been removed
from both frontends, publication and packaging. Every build checks all six English/Danish
Home, About and Projects routes, including emitted metadata and deliberate output mutations.
This copy is not order authorization: tax confirmations remain under #147,
server-side eligibility under #146, and the separate hosted-offer assessment under #18.

`src/data/company.json` holds the maintainer-confirmed registered name, CVR, ordinary sole-proprietor
type, VAT effective date, owner and business email. These current facts apply to every price
state. With the default-off `FEATURE_COMPANY_IDENTITY=true` release preview,
the shared footer identifies Devantler Tech (CVR 46830385), and Home offers a direct email
link to `ned@devantler.tech` with LinkedIn as an alternative. False and entirely unset builds
retain the existing LinkedIn contact and footer until reviewed activation and live proof.
The email link opens the visitor's email application; the site does not send messages itself.
The maintainer has confirmed receipt at the business email. Required company-address
disclosure and deployed English/Danish proof remain under
[#3917](https://github.com/devantler-tech/monorepo/issues/3917) and the legal audit in #19.
The supplied registered address is also private: publish it on the website only where legally
required, and never duplicate it in public backlog evidence. The English/Danish company-information
pages group the registered facts and physical address behind native footer links. The address is
rendered as escaped text only on those pages and excluded from the site's search index. Publication
does not make the address confidential: it will be visible on the public disclosure page.

The source-owned publisher enables `FEATURE_COMPANY_IDENTITY` and supplies the two-line
`COMPANY_POSTAL_ADDRESS` repository secret to the build step. The build aborts before artifact
upload if the setting is missing, empty, malformed or contains control characters. The secret is
server-only configuration, not a browser environment variable; the required address still appears
in the two public HTML pages. CI uses a fictional fixture, never the registered residential address.
For a local preview, provide an explicitly fictional two-line `COMPANY_POSTAL_ADDRESS` together
with `FEATURE_COMPANY_IDENTITY=true`. Explicit-false and entirely-unset builds require no address,
omit disclosure links and redirect the company routes to their localized homepage. The generated
disclosure checks run in every build. This resolves only the identification/contact portion of #19;
it does not establish blanket Danish-law compliance or activate the revised offer, portal orders,
authentication, agreements, payments or subscriptions. The wider audit remains open.
There is no contact-form backend, automatic booking,
payment flow or paid product subscription. Home, About and Projects are translated; the journal,
CV and detailed technical documentation remain in English and are labelled accordingly.

## Visitor statistics and browser storage

The separate Go module in `server/` provides default-off invitation-only browser
sign-in through one identity broker, with durable private sessions and current
client/operator authorization. It is not included in the static Pages artifact
and has no production listener or provider wiring. See its [runtime boundary and
validation guide](../server/README.md); deploying and enabling the real portal
requires the remaining provider, operational and legal release gates.

Public pages do not load a visitor-statistics client or tag outbound links for analytics.
Both the business layout and supporting Journal/documentation layout serve their scripts locally.
Every production build checks all emitted HTML for automatic external scripts and statistics
attributes, including explicit rollback builds. Links to other websites remain ordinary links;
public repository metadata is refreshed by the publisher, not by visitors' browsers.

The appearance selector uses the local `starlight-theme` preference only to remember System,
Light or Dark. Removing the statistics client does not eliminate requests needed to serve the
website or establish the absence of hosting logs. Processing purposes, recipients, retention,
the required privacy notice and any applicable consent remain under the audit in #19.
Analytics may be introduced only after that processing and its release requirements are assessed
and reviewed; self-hosting or a cookie-free claim alone does not clear them. Do not claim legal
compliance, zero processing or measured discovery outcomes from this build check.

## CV download

The About page offers the CV as an A4 PDF at `/pdfs/nikolai-emil-damm-cv.pdf`. It is not a checked-in
file: the static endpoint in `src/pages/pdfs/` renders it during `npm run build` (and on request in
`npm run dev`) from `src/data/cv.ts`, using the same palette as the site theme.

`src/data/cv.ts` is the single source for the CV. The detailed background in
`src/content/docs/about.mdx` retains a hand-written experience roster.
`scripts/check-cv-drift.mjs` (run in CI) fails when that roster and the data disagree on a role title,
period, or organisation line. When the content changes,
bump the `updated` date in `src/data/cv.ts` so the PDF says when it last changed.

## Blog editorial standard

The article footer's modification date comes from the committed history of that content file,
not the build or deployment date. The publication date remains the post's explicit frontmatter.
All maintained Astro builders fetch complete Git history. The shared production build rejects
shallow, absent, enclosing or unreadable history rather than publishing misleading freshness.
Use a complete checkout for local production builds; for an existing shallow clone, run
`git fetch --unshallow` before rebuilding. Git-less source archives cannot establish these dates.
An untracked file has no committed modification date, so Starlight omits that footer date.
`bash scripts/journal-history.test.sh` exercises real commit histories and the checkout depths
configured by CI, source publication and legacy callers. Article revisions advance the date;
unrelated commits do not. Publication, provider authority and portal rollout boundaries are unchanged.

The blog is a maintained product for people outside the repository, not a release-note feed or an
internal engineering diary. A worthwhile post starts from a real audience and problem, helps readers
understand why the work matters, and gives them a useful next step.

Use this high-level story shape: **Problem → Why it matters → What Devantler Tech built → Verified
outcome and trade-offs → Next step**. Define unavoidable jargon and explain where the product fits in
the wider portfolio. Link to deep implementation detail instead of making it the opening premise.
Never invent first-person experience, users, testimonials, adoption numbers, or precision that the
available evidence cannot support.

For an honest update on work still under way, use **Problem → Why now → Current status → Shipped
versus planned → Known unknowns and trade-offs → Next step**. Label shipped and planned work plainly;
do not turn intent into an implied outcome.

New posts and material updates to existing posts follow the same quality bar:

- Start from current, privacy-safe quantitative or qualitative evidence: recurring questions,
  adoption/onboarding friction, a meaningful shipped outcome, stale positioning, or an important
  lesson whose claims can be verified. Page views alone are not proof of value.
- Use complete frontmatter: intentional title, date, authors, useful tags, distinct description and
  excerpt, and a relevant cover image with descriptive alt text.
- Verify every command, product/version/license statement, screenshot, example, and link against the
  current portfolio. Refresh useful old posts when those facts or their positioning change.
- Keep the presentation skimmable and professional: a clear opening, descriptive headings, short
  paragraphs, purposeful visuals, and a relevant call to action.
- Verify follower-facing distribution: RSS inclusion, social/OG presentation, and a measurable CTA.
  Preview the result across mobile, tablet, and desktop, then run `npm run build` from the repository root before
  opening the draft PR.

Publication cadence is a prompt to review opportunities, never a reason to create filler. Record the
intended reader outcome before publishing and revisit privacy-safe aggregate signals after the chosen
measurement window to improve, redistribute, update, or retire the content.

For a substantive publication or refresh, keep one experiment issue open with the audience, evidence,
hypothesis, success proxy, measurement window, and follow-up date. Close its delivery child when the
post merges; close the experiment only after recording the measured outcome and resulting decision.
Keep this lane single-flight—maintain or measure the current post before starting another.

## Public product catalogue

Normal Projects output selects six maintained public tools as described under Featured portfolio,
alongside the public website example and hosting, with historical links available separately. The old “Built in the open” shelf of
tools, libraries, templates and source-available inspiration appears only when both
`FEATURE_CLIENT_PORTFOLIO` and `FEATURE_FEATURED_PORTFOLIO` are explicitly false.
`src/data/public-products.json` holds the full curated bilingual descriptions. The
`public-products:` inventory in `src/content/docs/projects/active.mdx` is checked against this JSON;
that MDX file holds legacy portfolio metadata, not the public description-editing surface.
The production contract verifies the rendered catalogue. Shared Actions and reusable workflows
live in `.github`; historical automation bookmarks now identify that maintained entry rather than
advertising a retired repository. Repository licences govern reuse; World at Ruin is a pre-alpha game
whose source is available for study, not unrestricted reuse or hosting.

`src/data/github-stars.json` is generated with an explicit UTC observation date. Cards sort by GitHub
stars descending, then repository name. In the legacy rollback, its leading six cards are visible
and the remainder sits in a native disclosure. Normal output has only the six selected maintained
tools; `more-public-products` remains a hidden historical anchor, not a remainder disclosure.
Builds and visitors need no GitHub connection. Refresh the snapshot when
updating the catalogue and during the monthly site content review:

```sh
bash scripts/refresh-public-stars.sh
```

The refresh requires authenticated `gh` and `jq`. It validates every selected repository as public,
unarchived and present exactly once before atomically replacing the snapshot. Failed or incomplete
reads leave the existing file untouched; missing or invalid counts fail the build rather than
silently becoming zero. The production build checks ranking, the collapsed remainder, bilingual
routes and failure cases. Review and commit the generated snapshot alongside catalogue changes.

## Feature flags (build-time)

Part of the portfolio-wide **feature-flag-first delivery** program
([monorepo#2059](https://github.com/devantler-tech/monorepo/issues/2059)): unreleased content or UI
lands **behind a default-off flag** so it can ship latent and be previewed before it goes live.

The site is a **pure static build**, so flags are **baked at build time** — there is no runtime,
per-user, or percentage evaluation. Flipping a flag means a **rebuild + redeploy**. (Live/per-user
rollout would require an SSR/hybrid adapter or a client-side [OpenFeature](https://openfeature.dev/)
web island — explicitly out of scope for the static site today.)

### The convention — `astro:env`

The isolated SvelteKit migration candidate in `web/` uses its own framework-native
`FEATURE_SVELTE_FRONTEND` setting, not the Astro schema. It remains default-off. The common
build can select it, the publisher admits it only for an artifact-only preview, and the Go
packager requires an explicit `--svelte` option. Production publication remains Astro.
See the [candidate guide](../web/README.md) for its enabled,
false and entirely unset checks. Restart the preview process after rebuilding a different
setting: a running preview has already loaded the old compiled server. Gate review is tracked
by [#125](https://github.com/devantler-tech/business-site/issues/125).

Flags are declared in the Zod-validated [`astro:env`](https://docs.astro.build/en/guides/environment-variables/)
`env.schema` in [`astro.config.mjs`](../astro.config.mjs) — type-safe over raw `import.meta.env`:

```js
env: {
  schema: {
    FEATURE_PREVIEW_BANNER: envField.boolean({
      context: "server",   // read at build time in .astro components (SSG)
      access: "public",
      default: false,      // OFF by default — production omits the gated output
    }),
  },
},
```

Gate rendering on the flag by importing it from `astro:env/server` (or `astro:env/client` for a
`PUBLIC_`-prefixed client flag) — see [`src/components/PreviewBanner.astro`](../src/components/PreviewBanner.astro),
the worked example mounted on the English and Danish business homepages. When the flag is off,
the component emits nothing; enabling it adds a localized notice above the homepage introduction.

Flags can also gate **content-collection inclusion** (filter entries out of `getCollection(...)`
when a flag is off) to hold back whole docs sections.

### Preview builds

To review flagged content before production enables it, run a **preview build** with the flag on:

```sh
FEATURE_PREVIEW_BANNER=true npm run build
```

Production builds in CI keep this example preview-banner flag off, testing both an explicit
`false` value and an entirely unset environment. `publish-site.yaml` publication leaves it unset.
CI also tests an explicitly enabled preview build.
This is not a rule for every flag: client-facing and featured-portfolio
presentation default to true, and the source-owned publisher explicitly enables company identity.
The Journal presentation is permanent and no longer has a feature flag.
That identity preview needs the separately documented fictional address for local use; never copy
the production address into a local fixture or public artifact.

### Lifecycle — remove the gate once shipped

A *release* flag is **short-lived**. Once the content/UI is live for good:

1. Delete the flag from the `env.schema` in `astro.config.mjs`.
2. Inline the gated markup (drop the `{ FLAG && (...) }` wrapper) or delete the example component.
3. Remove the flag from any preview-build invocations.

A growing set of stale flags is debt, not progress — retire each one as soon as its content ships.
Only a genuine kill-switch or a permanent setting is long-lived (and a permanent setting belongs in
plain config, not a flag).
