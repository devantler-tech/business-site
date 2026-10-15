# An isolated HTML-first SvelteKit frontend

Status: first reversible implementation slice of
[#124](https://github.com/devantler-tech/business-site/issues/124).
The portfolio selection spike [monorepo #4019](https://github.com/devantler-tech/monorepo/issues/4019)
and adoption epic [#4222](https://github.com/devantler-tech/monorepo/issues/4222) remain separate.

## Decision

Begin the business frontend migration with Svelte 5 and SvelteKit 3 in `web/`, while retaining
the root Astro/Starlight application as the published source. The first slice is the bilingual
About page and shared visual shell. `FEATURE_SVELTE_FRONTEND` must explicitly equal `true` to
emit those pages; false/unset settings emit neither locale. Unknown values fail the build.
Missing enabled routes and any unreviewed missing route remain build failures.

Use prerendered HTML and `csr=false` for this reading surface. The existing local theme controller
is the only referenced script; no Svelte runtime is downloaded for these pages. Preserve current
factual copy, local font files, brand colors and the original portrait. Generate responsive AVIF/
WebP/JPEG variants at build time rather than transferring the original photograph unchanged.
Reuse framework-neutral content instead of maintaining a second translated biography.

SvelteKit is the best-supported migration direction for the combined website/application
workload, not a claim of universal speed superiority. It provides native typed routes,
server/client separation, static rendering, server rendering and progressively enhanced forms
in one framework. Two existing portfolio websites already use this family. Astro remains a
strong static-content baseline; its lack of default hydration is not a reason to replace it.
The [retained samples and bounded results](../../research/frontend-4019/README.md) distinguish
measured behavior, documented capability, author errors and missing evidence.

## Security, ownership and deployment

The existing Go/PostgreSQL boundary remains authoritative for identity, authorization, sessions,
invitations, customer isolation, agreements, invoice evidence and future queue/API/MCP work.
Do not implement separate Google/Microsoft clients or move private authority into public JavaScript.
Selecting SvelteKit does not choose a second database, authentication service or paid host.

No publisher, runtime package, container, production switch, private route or environment secret
is wired to this candidate. Existing production Astro builds and their public/private boundary
controls remain mandatory. Private application rendering and the Go integration need their own
threat model, actual synthetic-broker/two-customer/operator tests and deployment proof. A public
About page and an amount-validation sample cannot establish those properties.

The candidate has a separate pinned lockfile, complete dependency audit, strict type/Svelte
checks, clean-install compatibility and enabled/false/unset artifact tests in CI. It cannot
silently publish instead of Astro. Unmigrated links deliberately retain the existing live URLs.
No content, license, legal, price or contact policy changes are part of this decision.

## Adoption and exit

Migrate public routes incrementally with bilingual URL/content/asset/metadata/accessibility parity.
Then prove the private frontend against the existing Go authority. Production replacement requires
the source-owned deployment receipt, real EN/DA visitor checks, launch prerequisites and a tested
rollback to the existing source-owned artifact. This slice does not close #102, #19, #21 or
monorepo #3939 and does not authorize portal activation.

The two frontends are temporary migration state, not the intended permanent architecture.
[#125](https://github.com/devantler-tech/business-site/issues/125) reviews the gate by
15 November 2026: remove it after successful reviewed adoption, or remove the dormant candidate
if it is rejected. The current publisher remains the immediate rollback because it is untouched.
No portfolio-wide rewrite follows automatically from a framework preference.

## Primary references

- [SvelteKit rendering options](https://svelte.dev/docs/kit/page-options)
- [Native form actions](https://svelte.dev/docs/kit/form-actions)
- [Server-only modules](https://svelte.dev/docs/kit/server-only-modules)
- [Images](https://svelte.dev/docs/kit/images)
- [Astro client directives](https://docs.astro.build/en/reference/directives-reference/)
