# Frontend decision samples

Dated, fictional research for [monorepo #4019](https://github.com/devantler-tech/monorepo/issues/4019),
not application routes or production dependencies. The retained Svelte sample uses six project cards, English/
Danish copy, an authored SVG, navigation and a server-validated amount form. The form neither
records an invoice nor authenticates a customer. It is not evidence for real identity, customer
isolation, payments or the existing Go portal's correctness.

## Reproduce

Use Node 24.19.0, npm 11.19.1, Bash and a Unix `time` executable. From this directory:

```sh
npm --prefix svelte ci --ignore-scripts
npm --prefix svelte audit
bash measure-builds.sh
node measure.mjs
```

The retained lockfile is dated evidence, not a second production dependency policy. Audit again before
execution; a later advisory invalidates an earlier clean reading. Stop if the audit fails.
The build script performs three serial repetitions and saves
each full build log and wall/user/system times locally. It includes npm launch overhead and uses
warmed dependency caches. Other work on the host is not controlled, so these times must not be used
to claim a performance winner.

From the repository root, `LC_ALL=C bash scripts/check-frontend-research.sh` performs clean
installs, audits, production builds and the actual HTTP probes for the retained Svelte sample.
It refuses occupied fixed ports and stops only its own child servers. This is also the CI path.

Start the production sample in a separate terminal:

```sh
HOST=127.0.0.1 PORT=5882 npm --prefix svelte start
```

Those fixed loopback origins match each sample's same-origin policy. From this directory:

```sh
SAMPLE_URL=http://127.0.0.1:5882 node --test probe.test.mjs
```

Visit `/`, `/da/`, `/portal/` and `/da/portal/`; try native Tab/Enter and language links.
Stop the server with Ctrl+C. No credentials, account, paid service or production connection
is needed. Do not expose the research servers publicly.

## What the measurements mean

`measure.mjs` counts **all emitted client JavaScript**, even bundles the HTML does not reference.
It sums separately gzipped files, not an observed network transfer. The original comparison and the retained
SvelteKit `csr=false` pages referenced zero scripts; SvelteKit still emits unused client bundles.
Dependency-node counts include build tools and optional/platform packages, not just shipped code
or exploitable attack surface. Lockfile digests identify exactly what was measured.

The local browser tests exercise real native forms and keyboard paths. A narrow viewport does
not simulate a constrained CPU or network, and no Core Web Vitals, interaction-latency, resource
utilization or real-provider result is claimed. Those measurements and the customer-scoped,
federated-authentication integration remain required before a replacement goes live.

## Candidate screening

The historical Astro sample was removed after the Svelte-only decision in #125. Its dated
[results](results.md) remain evidence; its source is recoverable from immutable Git history.
Only the Svelte research sample is executable here. It does not replace the production Go backend.

SolidStart 2.0.6/Solid 1.9.17 with its documented Nitro 3 beta integration was also prototyped.
Its dependency audit on 15 October 2026 reported the unfixed
[braces stack-exhaustion advisory](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
through micromatch/fast-glob. That is an adoption-floor failure for that dependency graph, not
proof of an exploitable website endpoint or a permanent judgment about Solid. Its vulnerable
manifest/lockfile are not introduced here. Initial scaffold metadata/localization defects were
our implementation defects, not framework defects; its preliminary timings are not a fair
parity comparison and are excluded from the decision table.

Qwik's resumability, Next.js server/client components and Nuxt's universal/hybrid rendering were
screened from their primary documentation, not benchmarked here. Popularity and star counts are
not evidence of suitability. See the portfolio decision record for trade-offs and admission gates.
