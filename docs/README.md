# devantler.tech

The [devantler.tech](https://devantler.tech) site — an [Astro](https://astro.build) +
[Starlight](https://starlight.astro.build) static site. Its source lives in
`devantler-tech/business-site` at the repository root (`src/`, `public/`, `scripts/`).
This `docs/` directory holds contributor documentation and architecture decisions. The callable
`.github/workflows/publish-pages.yaml` publishes the reviewed source selected by the monorepo's
`applications/business-site` pin to the existing monorepo Pages resource and domain.

## Develop

```sh
# From the repository root
npm ci
npm run dev      # local dev server
npm run build    # production build (this is what CI validates)
```

Use Node 24 and npm 11. CI clean-installs with npm 11.4.2 before repeating the install
with the runner's current npm 11. This also checks older supported versions: they
require a nested optional Markdown peer that newer npm releases can omit when
generating a lockfile. For intentional dependency changes, regenerate rather than
editing the lockfile:

```sh
npx --yes --package=npm@11.4.2 npm install --package-lock-only --ignore-scripts
```

Then run both CI install commands and the build. A newer generator's output is
acceptable only when it passes the same clean-install checks.

## Business website

The business homepage is rendered by `src/components/business/BusinessSite.astro` at `/` (English)
and `/da/` (Danish). Its offer amounts and translated copy live together in
`src/components/business/content.ts`. Prices are introductory guides, not an automatic checkout:
project scope, hosting capacity, external fees and support are agreed in a written proposal.

The site keeps the original green palette and locally served Matrix artwork. Its appearance
selector offers System, Light and Dark in both languages. The small head script applies the saved
choice before painting, follows system changes in System mode, and shares Starlight's
`starlight-theme` preference with supporting pages. If browser storage is blocked, switching still
works for the current page. Without JavaScript, the page follows the system theme and hides the
inactive selector. `scripts/theme.test.mjs` tests the actual controller as part of every build.

The introduction identifies Nikolai with the existing public `profile.jpg` photograph, biography
and GitHub links. First-person English/Danish copy explains the independent business without
inventing client endorsements; the family projects remain labelled as such. The real photograph
also supplies the sharing image. Built-page checks verify the portrait and profile journey.

Selected work uses the same `WorkExample.astro` card on Home and Projects in both languages.
AS Coaching og Vaner includes a real public homepage capture, a compact responsive thumbnail
linked to its original local JPEG in a new tab, and a separate link to the public website.
The Wedding App shows its documented local guest demo after sign-in, with names, date,
venue/address, countdown values and the venue background removed before capture. The card
explicitly labels it an anonymized demo and does not link to the private invitation site.
No production account or guest invitation code is used. Built-page checks verify both
thumbnails and their readable larger images, as well as the localized visit action.

Home focuses on services, two visual examples and a compact engineering-quality summary. A short
bridge points to the public portfolio instead of repeating product cards or the About biography.
Projects owns the complete catalogue and a collapsed evidence disclosure, linked from Home:
actual KSail test/lint and CodeQL workflows, Wedding App accessibility tests, and portfolio
maintenance/review rules. A direct fragment opens that disclosure in either language. The copy
describes verified practices rather than universal coverage, certification, vulnerability-free
software or unlimited maintenance. Project-specific checks and ongoing support remain scoped.

`FEATURE_CLIENT_PORTFOLIO=true` previews the client-facing redesign requested in business-site#5.
The preview puts plain-language quality and security highlights on Home before the prices and
examples. Projects leads with the real family website/app examples, keeps all thirteen public
products visible, and retains the hosting and research bookmarks. Business Home/Projects do not
link to source code; the journal and existing technical documentation retain their own detail.
The flag is default-off and does not change the publication revision or turn on the redesign in
production. CI builds enabled, explicit-false and entirely-unset states and uploads only the final
normal production output. `scripts/check-client-facing.mjs` exercises the emitted EN/DA visitor
pages; `scripts/client-rollout.test.mjs` guards the actual CI invocation and its negative controls.
After a reviewed activation and live visitor verification, remove this short-lived release flag.

The business identity also covers `/about/` and `/projects/`, with Danish counterparts at
`/da/about/` and `/da/projects/`. About introduces the founder of a one-person business; Projects
distinguishes open-source tools and family examples from client work. The journal and technical
pages reuse the business navigation, typography, colors, footer and appearance control through
Starlight component overrides. Their search, sidebar, RSS and historical articles remain available.
Only Starlight Blog's preview cards receive whole-card mouse navigation. Each native title link
provides the single keyboard destination without making its article a second focus stop. Links,
form controls, disclosure toggles and editable descendants retain their own interaction; individual
journal and technical articles remain reading surfaces. `scripts/journal-navigation.test.mjs`
executes the actual head script as part of every build.
There is one appearance picker, including on mobile; the documentation header measures its height
so the reading tools do not overlap the business navigation.

`FEATURE_JOURNAL_PRESENTATION=true` previews the editorial Journal frame from business-site#11.
Its introduction, lead story, two-column archive and narrower reading pages use the homepage's
type and palette. A native “Browse the Journal” disclosure keeps the existing post/topic menu
available without a permanent documentation sidebar. Search, RSS, authors, tags, pagination,
article bookmarks and copy controls retain their existing behavior. Technical guides keep their
normal Starlight frame. The flag defaults to false; every build checks all generated Journal
routes, and CI validates enabled, explicit-false and entirely-unset states. Activation and removal
are tracked in [#12](https://github.com/devantler-tech/business-site/issues/12), separately from
source adoption and live publication.

Projects presents one complete, stars-ranked public software catalogue, followed by family examples
and earlier research. The real KSail terminal capture appears in its product card; expandable English
research and diagrams are sourced from `src/content/docs/projects/completed.mdx`. The legacy
active/completed URLs redirect to the public catalogue or research section of `/projects/`; the
documentation sidebar links only to that canonical page. Browser redirects preserve incoming
heading fragments, which land on the corresponding public product card or family/research content.
The deployed-platform bookmark lands on the separate Platform hosting-project example, not the
reusable Platform Template in the public software catalogue. A bookmarked card
in the collapsed remainder opens that disclosure. Links without a fragment and the no-JavaScript
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

The company contact email and CVR number are not known yet, so neither is invented or published.
LinkedIn is the existing verified inquiry route. The maintainer has authorized publication with
registration/contact details deferred to [#3917](https://github.com/devantler-tech/monorepo/issues/3917).
That checklist covers the confirmed registered name, CVR, approved public business address and
working email; publication does not claim verified statutory compliance.
There is no contact-form backend, automatic booking,
payment flow or paid product subscription. Home, About and Projects are translated; the journal,
CV and detailed technical documentation remain in English and are labelled accordingly.

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

The Projects page's “Built in the open” shelf lists public tools, libraries, templates and
source-available inspiration, not tenant deployments. `src/data/public-products.json` holds the
curated bilingual descriptions and is the source rendered by both language routes. The
`public-products:` inventory in `src/content/docs/projects/active.mdx` is checked against this JSON;
that MDX file holds legacy portfolio metadata, not the public description-editing surface.
The production contract verifies the rendered catalogue. Shared Actions live in `.github`; the
old `actions` repository has a separately labelled legacy card for existing consumers and bookmarks,
and directs new projects to `.github`. Repository licences govern reuse; World at Ruin is a pre-alpha game
whose source is available for study, not unrestricted reuse or hosting.

`src/data/github-stars.json` is generated with an explicit UTC observation date. Cards sort by GitHub
stars descending, then repository name; the leading six are visible and the rest sit in a native
disclosure directly below. Builds and visitors need no GitHub connection. Refresh the snapshot when
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

The production build (CI / `publish-pages.yaml`) leaves the flag unset, so it stays off.

### Lifecycle — remove the gate once shipped

A *release* flag is **short-lived**. Once the content/UI is live for good:

1. Delete the flag from the `env.schema` in `astro.config.mjs`.
2. Inline the gated markup (drop the `{ FLAG && (...) }` wrapper) or delete the example component.
3. Remove the flag from any preview-build invocations.

A growing set of stale flags is debt, not progress — retire each one as soon as its content ships.
Only a genuine kill-switch or a permanent setting is long-lived (and a permanent setting belongs in
plain config, not a flag).
