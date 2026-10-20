# Local results — 15 October 2026

Host: Apple M2 Pro, macOS 27.0.1; Node 24.19.0, npm 11.19.1, `LC_ALL=C`.
Shared host and warmed caches, not a controlled mobile-performance laboratory.
The protocol was recorded before sampling in
[issue comment 6201670881](https://github.com/devantler-tech/monorepo/issues/4019#issuecomment-6201670881).

| Candidate | Build wall seconds, three repeats | All emitted JS bytes | Sum of individually gzipped JS bytes | Lock dependency nodes |
| --- | --- | --- | --- | --- |
| Astro 7.3.6 | 1.97 / 1.31 / 1.24 | 0 | 0 | 308 |
| SvelteKit 3.1.0 / Svelte 5.57.2 | 2.32 / 1.96 / 2.05 | 93,917 | 37,360 | 73 |

Both samples' actual public and form HTML reference **zero browser scripts**. The Svelte emitted
bundle count is not a download claim. Wall times include npm startup and uncontrolled host load;
their large variance precludes a build-speed ranking. They are not the complete business site.

The final three-repeat batch above uses the formatted checked-in sources; its generated
records are retained in `results.jsonl`, measured with pinned Node 24.19.0. An earlier pre-format
batch measured Astro 12.02 / 7.64 / 3.86s and SvelteKit 14.04 / 8.28 / 9.82s. This is not discarded
as a failure or combined into one uniform sample: formatting changed the source, the earlier
compression utility was not runtime-pinned, and host contention was uncontrolled. Neither batch
establishes a user-perceived speed difference.

Lockfile SHA-256:

- Astro: `aa1086caf43f2145825c4f677f5c1261096d6caaadf96a10c23017504a84185f`
- SvelteKit: `6789be912b833aefb90aafd332cc6ac8b92539b448eb75676fc0c3b108d8b75c`

Initial functional probes passed five controls for each of Astro, SvelteKit and SolidStart.
The strengthened metadata/localization probe exposed errors in our Solid scaffold. Those were
not used to rank frameworks. Subsequent audit excluded that version's dependency graph; the
retained eligible samples are checked with the stronger probe.

The final actual production probe passes seven controls for each retained candidate, including
HTML content negotiation, both locale results, invalid amounts and foreign-origin refusal.
An intermediate probe omitted the browser's `Accept: text/html` header and received SvelteKit's
legitimate JSON action response; the probe was corrected rather than treating that as a renderer
defect or accepting JSON as rendered-page proof.

Actual local SvelteKit browser observation: native amount-field Tab to submit/Enter renders the
server-validated result, English-to-Danish navigation renders six cards in the right locale,
and public/form documents contain no script tags or module preloads. Astro's actual native
Enter path also renders its server-validated result with no script tags. The first malformed
native input-pattern setup was corrected in all samples, not counted as a framework limitation.

The separate real-business About candidate was built and visited in English/Danish: the local
optimized portrait loads, System/Light/Dark remains synchronized across language navigation,
native keyboard navigation works, and a 390px viewport has no horizontal overflow. It references
only the existing local 1.62kB theme controller, not Svelte hydration. Its enabled/false/unset
artifact controls pass; ordinary Astro output still passes the existing visitor checks.

Unmeasured: constrained-device load/interaction distributions, production resource use, real
federated-provider integration and customer-scoped replacement behavior. No numerical weighted
winner or universally fastest framework is claimed. These results support a reversible first
SvelteKit increment, not production cutover or closure of the whole portfolio spike.
