# Private contact comparison: current shared-authority comparator

Status: dated amendment to
[ADR 0014](0014-private-contact-renderer-experiment.md), preregistered
2026-10-19 for #199; delivery #211. ADR 0014 and its evidence stay unchanged.
Comparative samples may start only after this amendment has a substantive
current-head review and has merged. Record this amendment's reviewed commit and
merge commit with the eventual evidence bundle, alongside ADR 0014's.

## Why the comparator changes

ADR 0014 named `27411f3ff7dfa752627f08c436dbfb9c8580c962` (#198) as the Go
baseline. The contact HTTP integration in #210 (closing #209) changed the
shared Go contact authority that both sides run, not only the candidate. At the
old baseline, the Go contact page wrote headers and streamed the template
directly after its scoped read. Since #210, every contact response, with or
without a renderer:

- keeps the original session token, principal, deadline and the client used by
  the scoped read (`contactAuthority`), instead of re-reading identity later;
- buffers the complete private document before headers are committed;
- before release, rechecks provider and flag readiness and any displayed
  confirmation gate, reloads the original durable session through SCS's
  context-aware store and codec, and runs one final SQL check scoped to the
  original client, the hashed session key and the exact session bytes.

Comparing the old Go source with a candidate that contains these safeguards
would mix an authority change into the presentation experiment. Both sides must
run the same current authority.

## Bound identities

| Identity | Commit |
| --- | --- |
| ADR 0014 reviewed plan head (#201) | `5689b7b7f0bd50579b209c614dd94192a3726539` |
| ADR 0014 plan merge | `868d7acdeb4f822d0f15758e15b1539c245f5b53` |
| Original baseline (#198), retained as history only | `27411f3ff7dfa752627f08c436dbfb9c8580c962` |
| #210 reviewed head | `f9940ba7e329ccfdd81535abdcc61322eb4dc757` |
| #210 merge, the new shared source | `a1be78797e7886e3eb57605792027af6d4811add` |

The four dependency inputs are byte-identical at the original baseline, the
plan merge and the #210 merge, so ADR 0014's pins stand unchanged:

| Input | SHA-256 |
| --- | --- |
| `package-lock.json` | `fec6fd85de707023da30c1ccb6984921ffde4424153014db03042123a2408be7` |
| `web/package-lock.json` | `af00c10f41f41cbc3605f93b7a68d9d133395d1ebfbecae38b1632eee4720d45` |
| `server/go.mod` | `fbbd640cf43af89f1974724f3c59bff49889b8e9cab7b70cc80d14eca840b54e` |
| `server/go.sum` | `e9d42cc756bf9c1790418b0cb257d5621599d5ebceb30be9238180ef12d3aa07` |

The pinned Go toolchain, PostgreSQL 17, Node 24, npm 11, C locale and the
Svelte, Kit, Vite and Svelte Vite plugin versions are those ADR 0014 records.

## Every difference from the original baseline

Between `27411f3` and `a1be787`, the non-test server changes are:

| Path | Kind | Effect on the comparison |
| --- | --- | --- |
| `internal/identity/contact.go`, `contact_verification.go` | Shared authority | Scoped read returns its client; contact, save-error and confirmation responses carry the original authority into the final checkpoint. Validation, compare-and-set and 200/400/409/303 semantics are unchanged. |
| `internal/identity/contact_renderer.go` | Shared authority, plus candidate composition | `renderContact` buffers and calls `contactResponseAllowed` on both sides. Only when a renderer is composed does it also call the renderer and recheck the renderer's release gate. |
| `internal/identity/browser.go` | Composition | `Browser` passes no renderer. `DeveloperContactBrowser` requires one. `browserMember` factors the existing 401 response without changing it. |
| `internal/contactrenderer/supervisor.go`, `internal/renderwire/codec.go` | Candidate only | Bounded Node child supervision and the strict message codec. Not reachable from `Browser` or the executable. |
| `AGENTS.md`, `CONTACT-RENDERER.md`, `CONTACTS.md`, `README.md`, `RENDERER-PROTOCOL.md` | Documentation | No runtime effect. |

The private page templates, the shared frame, the public artifacts and the
production runtime packaging are unchanged. The remaining changes are the
candidate component under `web/`, Go and Node tests, two CI steps that run the
compiled component, runtime-contract and spelling checks, ADR 0014 itself and the
root `AGENTS.md`.

## Comparator and candidate

Both sides are built from `a1be787` and are then given the identical frozen
maintenance task from ADR 0014 on separate trial branches.

- **Go comparator:** `Foundation.Browser` with no renderer, the same composition
  `internal/runtimeapp` serves. Its contact responses run the full current
  identity, session, client and record checkpoint described above.
- **Candidate:** `Foundation.DeveloperContactBrowser` with the fixed reviewed
  compiled contact bundle under `contactrenderer.Supervisor`. It runs the same
  checkpoint, the same native Go writes and the same independent-actor fixture.
  Its additional release-gate recheck and child process are candidate costs and
  count against its budgets.

No candidate logic or response rewriting enters the comparator. Any shared
trial harness is reviewed and frozen separately, as ADR 0014 requires.

## Go-only startup needs no Node

`internal/runtimeapp` constructs only `Browser`; `DeveloperContactBrowser` and
`contactrenderer.New` are called only from tests. The production image is
`gcr.io/distroless/static-debian13`, which contains no Node, and the
`publish-container` job passed at `a1be787`. With Node removed from `PATH`,
`go test ./cmd/business-site` passes at `a1be787`, built with the local Go
1.27.2 toolchain. The trial still records the Go side's process tree, which must
contain no Node process.

## Re-exercised integration

The required CI run at the #210 merge,
[run 39243869240](https://github.com/devantler-tech/business-site/actions/runs/39243869240),
passed `test-portal-identity`. That job builds the compiled component and runs
the real contact HTTP path with the actual broker, disposable database and
bounded renderer. It is correctness evidence, not a measurement.

## What stays frozen

Everything else in ADR 0014 is unchanged: the complete presentation boundary,
the runtime and failure contract, the frozen bilingual maintenance task and its
source-count objective, every protected holdout, the three independently
restarted Go/Svelte, Svelte/Go, Go/Svelte pairs, 20 warmups and 100 journeys a
side, the nearest-rank p50/p95 paired estimator with 10,000 bootstrap resamples
and seed 199200, the resource and build bounds, every budget, the decision rule
and the same-record rollback drill. No threshold is tuned and no failure is
discarded. Samples taken against the old baseline are not comparable and are not
reused.

## Observation window

ADR 0014's window, 2026-10-19T02:00:00Z through 2026-10-26T02:00:00Z, is
unchanged. No comparative sample has been collected. The fixed trial entrypoint
`scripts/compare-private-contact.sh` does not exist yet and must be built,
reviewed and merged first. If the three pairs and the rollback drill cannot be
completed inside this window, a further reviewed, dated window amendment is
required before any new measurement. The gate's lifecycle task, #207, keeps its
2026-10-26 removal-or-renewal review and is not extended here.

This amendment starts no measurement or schedule and claims no adoption,
performance or maintenance result. #199, #142 and #131 stay open until their
actual outcomes are decided.
