# Frontend decision samples

Dated, fictional research for [monorepo #4019](https://github.com/devantler-tech/monorepo/issues/4019),
not application routes or production dependencies. Both samples share six project cards, English/
Danish copy, an authored SVG, navigation and a server-validated amount form. The form neither
records an invoice nor authenticates a customer. It is not evidence for real identity, customer
isolation, payments or the existing Go portal's correctness.

## Reproduce

Use Node 24.19.0, npm 11.19.1, Bash and a Unix `time` executable. From this directory:

```sh
npm --prefix astro ci --ignore-scripts
npm --prefix svelte ci --ignore-scripts
npm --prefix astro audit
npm --prefix svelte audit
bash measure-builds.sh
node measure.mjs
```

Each lockfile is frozen evidence, not a second production dependency policy. Audit again before
execution; a later advisory invalidates an earlier clean reading. Stop if the audit fails.
The build script performs three serial repetitions with alternating candidate order and saves
each full build log and wall/user/system times locally. It includes npm launch overhead and uses
warmed dependency caches. Other work on the host is not controlled, so these times must not be used
to claim a performance winner.

From the repository root, `LC_ALL=C bash scripts/check-frontend-research.sh` performs clean
installs, audits, production builds and the actual HTTP probes for both retained candidates.
It refuses occupied fixed ports and stops only its own child servers. This is also the CI path.

Start each production sample in a separate terminal:

```sh
HOST=127.0.0.1 PORT=5881 npm --prefix astro start
HOST=127.0.0.1 PORT=5882 npm --prefix svelte start
```

Those fixed loopback origins match each sample's same-origin policy. From this directory:

```sh
SAMPLE_URL=http://127.0.0.1:5881 node --test probe.test.mjs
SAMPLE_URL=http://127.0.0.1:5882 node --test probe.test.mjs
```

Visit `/`, `/da/`, `/portal/` and `/da/portal/`; try native Tab/Enter and language links.
Stop both servers with Ctrl+C. No credentials, account, paid service or production connection
is needed. Do not expose the research servers publicly.

## What the measurements mean

`measure.mjs` counts **all emitted client JavaScript**, even bundles the HTML does not reference.
It sums separately gzipped files, not an observed network transfer. Both Astro pages and the
SvelteKit `csr=false` pages reference zero scripts; SvelteKit still emits unused client bundles.
Dependency-node counts include build tools and optional/platform packages, not just shipped code
or exploitable attack surface. Lockfile digests identify exactly what was measured.

The local browser tests exercise real native forms and keyboard paths. A narrow viewport does
not simulate a constrained CPU or network, and no Core Web Vitals, interaction-latency, resource
utilization or real-provider result is claimed. Those measurements and the customer-scoped,
federated-authentication integration remain required before a replacement goes live.

## Candidate screening

Astro 7.3.6 with the Node adapter 11.1.8 is the current content-oriented baseline. SvelteKit 3.1.0,
Svelte 5.57.2 and Node adapter 6.0.0 test the proposed application foundation. The checked-in
sources, lockfiles, scripts and [results](results.md) make this bounded comparison repeatable.

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
