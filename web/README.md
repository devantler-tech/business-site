# Business frontend candidate

The English/Danish About pages in SvelteKit 3 and Svelte 5, using the website's existing
portrait, factual copy, visual identity and appearance preference. Public reading uses
prerendered HTML without application hydration; the existing local theme controller is the
only script referenced by those pages. This is a migration candidate, not the published site.

## Try it locally

Use Node 24, npm 11 and Bash. From the repository root, install this candidate's own pinned
dependencies, then build its explicitly enabled output:

```sh
npm --prefix web ci
FEATURE_SVELTE_FRONTEND=true npm --prefix web run build
npm --prefix web run preview
```

Open the printed loopback address at `/about/` or `/da/about/`. Stop the preview with Ctrl+C.
Unmigrated navigation links deliberately go to the existing public website. No account, database,
publishing credential or private customer information is needed. The build generates optimized
portrait formats from the original local photograph; fonts and scripts are also served locally.

From the repository root, run the complete candidate check:

```sh
LC_ALL=C bash scripts/check-svelte-frontend.sh
```

It audits all candidate dependencies, runs release-policy tests and strict Svelte/TypeScript
checks, then builds enabled, explicitly false and entirely unset settings. Both disabled builds
must remove the About pages, including output left by an earlier enabled build. The enabled
artifact must contain both locales, metadata and only the existing local theme script.
The check finishes with the candidate off. `web/build/` and `web/.svelte-kit/` are generated;
do not edit them or the generated lockfile. Regenerate dependencies with npm and test both clean
installs used by CI (npm 11.4.2 and the current supported npm 11).

## Migration boundary

The Astro/Starlight application, source-owned publisher and Go/PostgreSQL portal remain
unchanged owners. The candidate is not included in Pages, the runtime package or the container.
There are no private routes, identity integrations, customer records, payment operations or
new hosted services here. Do not put secrets or private environment values into public output.

[Issue #124](https://github.com/devantler-tech/business-site/issues/124) owns this first slice;
[portfolio spike #4019](https://github.com/devantler-tech/monorepo/issues/4019) owns the comparative
decision and [epic #4222](https://github.com/devantler-tech/monorepo/issues/4222) tracks adoption.
The [source decision](../docs/adr/0011-isolated-svelte-frontend.md) describes this slice's boundaries.
[Issue #125](https://github.com/devantler-tech/business-site/issues/125) reviews the temporary gate
by 15 November 2026. Full public-route parity, portal integration, production cutover and rollback
must be separately reviewed and proved before replacing the existing site. Existing production
portal launch prerequisites are not waived by selecting a frontend.

Follow the repository's [contribution guide](../CONTRIBUTING.md), [security instructions](../AGENTS.md)
and existing asset provenance. Public visibility does not grant an application-source license.
