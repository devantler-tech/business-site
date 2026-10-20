# Business public frontend

The English/Danish Home, About, Projects and gated company-information pages, English Journal and six technical guides in SvelteKit 3 and Svelte 5, using the website's existing
homepage content, artwork, portrait, visual identity and appearance preference. Public reading uses
prerendered HTML without application hydration; the existing local theme controller is the
only script referenced by Home and About. Projects also uses the existing bookmark-disclosure
controller, while two historical entrypoints use a minimal fragment-preserving redirect.
Journal uses the shared native-link card controller, with no application hydration.
The source-owned publisher and production-profile Go image select this frontend.

## Try it locally

Use the Node line declared in the root `.node-version` (24), npm 11 and Bash. The candidate's
engine range and Node type definitions follow that same line. From the repository root, install the root and candidate's pinned
dependencies, then build its explicitly enabled output. Root dependencies resolve the shared
content's TypeScript configuration; neither install publishes anything:

```sh
npm ci
npm --prefix web ci
bash scripts/build-journal-reference.sh
FEATURE_SVELTE_FRONTEND=true npm --prefix web run build
npm run preview -- --outDir web/build
```

Open the printed loopback address at `/`, `/da/`, `/about/`, `/da/about/`, `/projects/`
or `/da/projects/`, and `/blog/`, `/blog/2/`, `/blog/3/` for the current Journal archive.
The subscription link opens the complete existing `/blog/rss.xml` locally.
Topic links and the author browse link open the complete corresponding collections locally.
Technical guides are at `/agentic-engineering/`, `/templates/` and the four existing template paths. Stop the preview with Ctrl+C.
This serves the final static adapter artifact, including its generated sitemap files;
the framework's development preview does not serve those post-adapter files.
Unmigrated navigation links deliberately go to the existing public website. No account, database,
publishing credential or private customer information is needed. The build generates optimized
image formats from the original local artwork and photographs; fonts and scripts are also served locally.

Home includes the introduction, quality/security highlight, three service guides, one compact
public website example, process and contact journey. Home, About and Projects link locally;
language links retain the current business page. Journal navigation stays local; its Danish language
link clearly returns to the Danish business homepage rather than implying translated articles.
The CV and research PDF download locally; unmigrated destinations stay on the published website. Prices and translated copy come from the same framework-neutral sources
as Astro, not a second translation catalogue.

Projects preserves the six eligible tools selected by the shared ranking implementation,
the actual observation timestamp, the restrained KSail workflow and masked desktop detail,
one compact public website example, hosting context and native research disclosure.
The complete thirteen-entry inventory and old product/research anchors remain authoritative.
`/projects/active/` and `/projects/completed/` retain incoming fragments and have no-JavaScript
fallback links. No GitHub request or ranking token is sent to a visitor's browser.

Journal derives its complete ordered inventory from `src/content/docs/blog/`, not a copied list.
Build-time frontmatter parsing, excerpt rendering, author configuration and five-post boundaries
match the Astro archive; a build-only virtual module keeps filesystem access out of prerendered routes.
The candidate carries local optimized unique covers, native title links, independent author/topic
links, the existing recent-post/topic browse choices and previous/next pagination. The complete
reading routes for all 15 articles stay local. Topic and author archives
retain all matching posts on one page, matching Astro rather than truncating to the main archive's
five-post limit. Author cards keep their separate public profile link. The two diagram articles
render their authored flowcharts and mindmap locally, with named keyboard-scrollable views,
native text disclosures and clear failure messages. Technical-page links
stay local on their original English routes. Missing or unsupported metadata fails the
build rather than silently dropping a post. Tests compare every candidate card against the actual
Astro build, including date labels, excerpts and destinations.

RSS reuses the complete actual Astro XML, including its English channel identity, item order,
stable GUIDs, dates, descriptions, categories and full HTML content. A build-only XML parser is
declared at the version already in the lockfile; no parser reaches the browser. The input-bound
feed reference captures every local image and responsive variant. Native prerendered image
endpoints preserve those existing canonical asset URLs; missing or unsupported dependencies fail
the build. Feed discovery and visible subscription links stay local. The current complete feed
contains all 15 posts; a future partial/archive feed fails closed until archive support is explicitly
migrated. No Danish feed, subscription service, customer data or portal route is introduced.

Public discovery captures the freshly generated CV, maintained thesis PDF, favicon, robots file
and optimized sharing portrait from that same input-bound Astro build. These exact dependencies
are native prerendered endpoints, not a wholesale copy of public or deployment output. Missing,
corrupted or stale inputs fail before use. Home, About and Projects retain their English/Danish
canonical, language and social metadata; Journal and guides retain their own article metadata.
After the static adapter completes, the sitemap is derived from the actual canonical HTML output,
excluding redirect duplicates and noindex disclosures. There is no second route catalogue.
The publisher's CNAME and publication receipt are not application-owned assets and are not copied.

Technical guides reuse the same input-bound Astro reference rather than maintaining another
Markdown copy or renderer. The complete Agentic Engineering guide and template overview, Go,
.NET, platform and platform-tenant guides retain their original titles, descriptions, canonical
URLs, committed modification dates, body text, bookmarks, code-copy payloads, native tables,
template cards and authored diagram. A quiet reading frame shares the site's palette, typography,
theme control and keyboard behavior. Collection navigation and previous/next destinations stay
local when already migrated; deliberate external destinations, including the authored sponsor
badge, remain unchanged. The badge is not fetched during the build. There are no invented Danish
guide routes: DA returns explicitly to the Danish business homepage. Unsupported nested template
sources or body interactions fail closed. All six actual outputs are compared independently with
Astro, including negative controls for lost content/bookmarks/copy, missing inventory and a missing
guide stylesheet beside healthy assets. The legacy `/templates/gitops-tenant-template/` entrypoint
redirects to the current platform-tenant guide, retaining incoming heading bookmarks through the
existing local redirect controller and a native no-script continuation. It is absent when the
candidate is disabled. The Agentic Engineering diagram uses the same sanitized
local renderer and accessible text disclosure as Journal diagrams.

The reading routes reuse the actual Astro-rendered bodies and Expressive Code assets.
`build-journal-reference.sh` runs the explicit Astro builder (`npm run build:astro`) between
input-fingerprint checks, then stores a private generated reference under `.astro/`.
The candidate rejects missing/stale references. Common public builds run this bridge only when
Svelte is selected; the explicit reference command remains available. The generated reference
is never published. Renderer inputs,
the authored collection/assets and every article's committed modification date bind the reference.
Only required local image/stylesheet bytes enter the candidate, through SvelteKit's immutable
asset directory. Unreviewed body scripts or interactive content fail rather than being dropped.
The reading frame preserves all prose, code, bookmarks, author/topic links, sharing metadata,
structured data, dates and title-labelled adjacent articles. Native copy controls use the renderer's
actual payload and provide success or actionable failure feedback, without application hydration.
Diagram definitions are independently parsed from the authored Markdown and compared with the
complete Astro placeholder inventory. The reference bundles the already pinned Mermaid dependency
with the candidate's own Vite toolchain and records every local module and dependency. Missing
definitions, diagram figures, entrypoints or transitive modules reject the reference. Only diagram
articles load this renderer; ordinary pages do not. Rendering follows the selected appearance,
serializes overlapping changes and leaves the text alternative usable if a module or diagram fails.
Renderer output is explicitly sanitized at the DOM boundary using DOMPurify, now a direct dependency
at the same version already locked through Mermaid. The existing HTML-label integration is retained;
page paragraph styles cannot override diagram label colors or spacing.
The complete emitted renderer graph is about 4.4 MB uncompressed; browsers request only the modules
needed for the visited diagram types. This is not a performance improvement claim.
Prose-only articles omit the empty contents disclosure. Native comparison tables retain their
headers/cells inside named, keyboard-scrollable boundaries; article images and long inline text
remain contained on narrow layouts. One route dispatches decimal archive pages and exact admitted
article slugs; unknown articles return 404. Unsupported nested article slugs
fail the build explicitly rather than silently omitting a post. Local article, author and topic
destinations remain local; unmigrated and external links retain their original destinations.

The network-free build uses the tracked dated snapshot. For an explicitly refreshed build,
run `bash scripts/refresh-public-stars.sh /absolute/path/to/snapshot.json` from the repository root,
then set `PUBLIC_STARS_SNAPSHOT` to that file for the candidate build. Refresh uses authenticated
read-only public repository metadata. Invalid, partial, non-regular, future or more-than-one-hour-old
explicit input fails the build; it never becomes a fresh-looking zero-star catalogue.

From the repository root, run the complete candidate check:

```sh
LC_ALL=C bash scripts/check-svelte-frontend.sh
```

It audits all candidate dependencies, runs release-policy tests and strict Svelte/TypeScript
checks, then builds the production Astro reference and enabled, explicitly false and entirely unset candidate settings. Both disabled builds
must remove all six business pages, both redirects, every Journal archive (including topics and
authors), every reading route and technical guide with their dependent assets, the feed XML and its image dependencies,
and all public discovery files, PDF downloads, favicon and sharing dependencies,
even after an enabled build.
The enabled artifact must contain both locales, metadata and only the declared local scripts.
Independent artifact controls reject missing downloads, partial asset closures, stale sitemap members,
lost Danish metadata and publisher-file leaks. Capture controls also reject symlinks and exercise
a pathname substitution without consuming replacement bytes.
Every archive retains exactly one social-sharing description; reading pages supply their own article metadata.
The check exercises the actual build-local ranking path with a deliberately reordered complete
fixture and eight rejected inputs; it does not make a network request.
The check finishes with the candidate off. `web/build/` and `web/.svelte-kit/` are generated;
do not edit them or the generated lockfile. Regenerate dependencies with npm and test both clean
installs used by CI (npm 11.4.2 and the current supported npm 11).

## Migration boundary

The common `FEATURE_SVELTE_FRONTEND=true npm run build` path prepares the input-bound reference,
builds the complete candidate, validates actual route/asset/content parity and replaces `dist/`
without retaining stale incumbent assets. False/unset selects Astro. Install both lockfiles first.
The adapter captures a private, complete file/digest inventory after replacing its output.
The common builder supplies a fresh internal nonce and independently refuses changed, extra,
missing or older-build output before promotion; neither the inventory nor its marker is published.
`npm run build:astro` is the explicit nonrecursive incumbent/reference builder, with npm's
local executable path established for standalone reference builds.
The older preview-banner and legacy non-featured/non-client portfolio variants are not supported
by selected Svelte publication: requesting them fails rather than silently ignoring the setting.

The source-owned publisher's manual `mode=preview, frontend=svelte` dispatch on `main` builds
and uploads the actual selected artifact with its source/run receipt, but cannot deploy it.
Push, scheduled and ordinary production publication select Svelte. Manual dispatch defaults to
Svelte and artifact-only preview; explicit `frontend=astro` retains the incumbent recovery path.
Either frontend still requires the source switch and protected main-only environment to deploy.
See [the cutover and rollback decision](../docs/adr/0013-svelte-public-publication.md).

With Go 1.27.2, `bash scripts/package-runtime.sh --release --default --svelte` builds a clean-source
candidate in `runtime-svelte-default-dist/`. Exercise it with
`node scripts/runtime-smoke.test.mjs runtime-svelte-default-dist`; the same checks run against its
extracted archive in CI. The optional second argument `--production-template` retains the existing
private-address placeholder contract in `runtime-production-dist/`, the existing production
image/archive chain. CI explicitly selects Svelte for that profile. Ordinary
package commands pin Astro and ignore ambient frontend settings. Neither command deploys anything.
The Go/PostgreSQL portal remains the unchanged authority, independently default-off.
There are no private routes, identity integrations, customer records, payment operations or
new hosted services here. Do not put secrets or private environment values into public output.

The candidate preserves the existing company, offer, customer-scope and VAT settings through
SvelteKit's typed, build-time, server-only environment module. All four settings retain their
false default; ambiguous values fail the build. Shared copy/price factories and company data
remain authoritative. No portal setting is read and no setting authorizes orders or payments.
The source-owned publisher and production-template package explicitly select company=true,
VAT=true, revised-offer=false and customer-scope=false after the maintainer's 20 October 2026
effective-date confirmation. That tuple uses the approved gross prices and ordinary company
identity in every footer and disclosure. False/unset VAT inputs retain historical PMV comparisons
only; they are not valid current-production recovery. Astro recovery must also retain VAT=true.
The separate audience and hosted-offer releases remain closed, as do private portal operations.

`FEATURE_COMPANY_IDENTITY=true` adds the native company footer links, verified email contact
with LinkedIn as an alternative, and `/company/` plus `/da/company/`. Supply an explicitly
fictional two-line `COMPANY_POSTAL_ADDRESS` for local evaluation. Missing/malformed enabled
addresses abort the build without echoing the supplied value. The server's HTML transformation
fills two escaped text slots only on those required disclosure pages, excluded from search.
The address never enters load data, page-data JSON, browser code or unrelated output.
Disabled identity omits the two routes and gated contact details; a disabled frontend requires
no address even when the company setting is enabled.

`PUBLIC_CONTENT_PROFILE=production-template` preserves the existing two address placeholders
for the Go runtime's escaped substitution, without consuming the configured private address.
The explicitly selected Go package consumes this same template without changing private authority.
The company/contact matrix compares actual Astro and Svelte artifacts for unset, explicit-false,
current production, historical, revised/VAT, independently selected and template configurations in both languages.
Independent missing-locale, incomplete-disclosure and unrelated-script leak controls must reject
otherwise healthy real output. It also exercises the framework's invalid settings and address
diagnostics. Run it with `bash scripts/check-svelte-company.sh`; the complete candidate check
includes it and still finishes with the candidate off. No production switch changes here.

Issue #193 tracks actual publication and protected rollback/restoration evidence; #130 remains
open until those outcomes are verified. Company-information boundaries remain unchanged.

[Issue #124](https://github.com/devantler-tech/business-site/issues/124) owns the About foundation and
[#139](https://github.com/devantler-tech/business-site/issues/139) owns the homepage slice and
[#141](https://github.com/devantler-tech/business-site/issues/141) owns Projects parity;
[#144](https://github.com/devantler-tech/business-site/issues/144) owns main Journal archive parity;
[portfolio spike #4019](https://github.com/devantler-tech/monorepo/issues/4019) owns the comparative
decision and [epic #4222](https://github.com/devantler-tech/monorepo/issues/4222) tracks adoption.
The [source decision](../docs/adr/0011-isolated-svelte-frontend.md) describes this slice's boundaries.
[Issue #125](https://github.com/devantler-tech/business-site/issues/125) reviews the temporary gate
by 15 November 2026. Public cutover does not retire the reading bridge or enable portal integration.
Existing production
portal launch prerequisites are not waived by selecting a frontend.

Follow the repository's [contribution guide](../CONTRIBUTING.md), [security instructions](../AGENTS.md)
and existing asset provenance. Public visibility does not grant an application-source license.
