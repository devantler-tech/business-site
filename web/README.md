# Business frontend candidate

The English/Danish Home, About and Projects pages in SvelteKit 3 and Svelte 5, using the website's existing
homepage content, artwork, portrait, visual identity and appearance preference. Public reading uses
prerendered HTML without application hydration; the existing local theme controller is the
only script referenced by Home and About. Projects also uses the existing bookmark-disclosure
controller, while two historical entrypoints use a minimal fragment-preserving redirect.
This is a migration candidate, not the published site.

## Try it locally

Use Node 24, npm 11 and Bash. From the repository root, install the root and candidate's pinned
dependencies, then build its explicitly enabled output. Root dependencies resolve the shared
content's TypeScript configuration; neither install publishes anything:

```sh
npm ci
npm --prefix web ci
FEATURE_SVELTE_FRONTEND=true npm --prefix web run build
npm --prefix web run preview
```

Open the printed loopback address at `/`, `/da/`, `/about/`, `/da/about/`, `/projects/`
or `/da/projects/`. Stop the preview with Ctrl+C.
Unmigrated navigation links deliberately go to the existing public website. No account, database,
publishing credential or private customer information is needed. The build generates optimized
image formats from the original local artwork and photographs; fonts and scripts are also served locally.

Home includes the introduction, quality/security highlight, three service guides, one compact
public website example, process and contact journey. Home, About and Projects link locally;
language links retain the current page. Journal, CV and unmigrated destinations stay on
the published website. Prices and translated copy come from the same framework-neutral sources
as Astro, not a second translation catalogue.

Projects preserves the six eligible tools selected by the shared ranking implementation,
the actual observation timestamp, the restrained KSail workflow and masked desktop detail,
one compact public website example, hosting context and native research disclosure.
The complete thirteen-entry inventory and old product/research anchors remain authoritative.
`/projects/active/` and `/projects/completed/` retain incoming fragments and have no-JavaScript
fallback links. No GitHub request or ranking token is sent to a visitor's browser.

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
checks, then builds enabled, explicitly false and entirely unset settings. Both disabled builds
must remove all six candidate pages and both redirects, including output left by an earlier enabled
build. The enabled artifact must contain both locales, metadata and only the declared local scripts.
The check exercises the actual build-local ranking path with a deliberately reordered complete
fixture and eight rejected inputs; it does not make a network request.
The check finishes with the candidate off. `web/build/` and `web/.svelte-kit/` are generated;
do not edit them or the generated lockfile. Regenerate dependencies with npm and test both clean
installs used by CI (npm 11.4.2 and the current supported npm 11).

## Migration boundary

The Astro/Starlight application, source-owned publisher and Go/PostgreSQL portal remain
unchanged owners. The candidate is not included in Pages, the runtime package or the container.
There are no private routes, identity integrations, customer records, payment operations or
new hosted services here. Do not put secrets or private environment values into public output.

This candidate uses the published offer wording and the existing LinkedIn contact alternative.
It does not read `FEATURE_OFFER_COPY`, `FEATURE_COMPANY_IDENTITY`, `COMPANY_POSTAL_ADDRESS` or portal
settings. It does not render company disclosures or activate email/ordering; the live website
retains its separate reviewed configuration. Full production configuration/metadata parity is
still required before cutover under #130. Do not use this candidate as a substitute for the
published company-information pages.

[Issue #124](https://github.com/devantler-tech/business-site/issues/124) owns the About foundation and
[#139](https://github.com/devantler-tech/business-site/issues/139) owns the homepage slice and
[#141](https://github.com/devantler-tech/business-site/issues/141) owns Projects parity;
[portfolio spike #4019](https://github.com/devantler-tech/monorepo/issues/4019) owns the comparative
decision and [epic #4222](https://github.com/devantler-tech/monorepo/issues/4222) tracks adoption.
The [source decision](../docs/adr/0011-isolated-svelte-frontend.md) describes this slice's boundaries.
[Issue #125](https://github.com/devantler-tech/business-site/issues/125) reviews the temporary gate
by 15 November 2026. Full public-route parity, portal integration, production cutover and rollback
must be separately reviewed and proved before replacing the existing site. Existing production
portal launch prerequisites are not waived by selecting a frontend.

Follow the repository's [contribution guide](../CONTRIBUTING.md), [security instructions](../AGENTS.md)
and existing asset provenance. Public visibility does not grant an application-source license.
