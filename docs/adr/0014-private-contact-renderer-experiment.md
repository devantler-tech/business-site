# Bounded private contact presentation experiment

Status: preregistered design for #199; delivery #200. This record chooses an
experiment, not a private frontend replacement or a customer launch. Candidate
implementation and measurements may start only after this plan has a substantive
current-head review and has merged. The immutable plan identity is that reviewed
commit and its merge commit; record both with the eventual evidence bundle.

## Outcome and alternatives

Test whether Svelte can simplify maintained private presentation without weakening
the existing Go portal or adding disproportionate browser, runtime or build cost.
The public SvelteKit publication does not answer that question. Keep Go responsible
for identity, authorization, persistence, native routes, forms and response policy.

| Option | Decision and consequence |
| --- | --- |
| Current Go templates | Proven comparator and recovery path; one shared private frame already serves five page families. Retain unless the complete experiment demonstrates an improvement. |
| Server-compiled Svelte component | Chosen bounded candidate: maintained Svelte compilation and `svelte/server.render`, with one supervised Node child and no browser hydration. Server compilation still requires request-time JavaScript execution. |
| Request-time SvelteKit | Not chosen for this contact-only experiment. The current Kit app is static; a Node adapter, HTTP listener/proxy and private routing are additional deployment and failure boundaries, not existing infrastructure. Reconsider only with evidence that the bounded candidate cannot answer the product question. |
| Browser-rendered Svelte | Not chosen. A complete no-JavaScript contact form would still need a server-rendered presentation, adding a second path and browser work. A decorative widget cannot establish a private frontend replacement. |
| Custom compiler or Go JavaScript emulator | Not chosen. Neither is the maintained Svelte server runtime; introducing and validating one is unrelated scope. |

## Exact comparator and dependency identities

The clean current baseline is
`27411f3ff7dfa752627f08c436dbfb9c8580c962`, the merged #198 source. The earlier
`0fd607782623d6e469c511c5ebbab194ab676ac1` comparator in
[the comparison protocol](../../server/PRIVATE-FRONTEND-COMPARISON.md) has the same
production Go presentation/authority source: changes through #198 concern the
protocol, README and test fixtures. Use the corrected three-actor isolation
preflight at the current baseline, never older invalid samples.

SHA-256 identities at this baseline:

| Input | SHA-256 |
| --- | --- |
| `package-lock.json` | `fec6fd85de707023da30c1ccb6984921ffde4424153014db03042123a2408be7` |
| `web/package-lock.json` | `af00c10f41f41cbc3605f93b7a68d9d133395d1ebfbecae38b1632eee4720d45` |
| `server/go.mod` | `fbbd640cf43af89f1974724f3c59bff49889b8e9cab7b70cc80d14eca840b54e` |
| `server/go.sum` | `e9d42cc756bf9c1790418b0cb257d5621599d5ebceb30be9238180ef12d3aa07` |

Use the pinned Go toolchain, PostgreSQL 17, Node 24, npm 11 and C locale. The
current web lock pins Svelte 5.57.2, Kit 3.1.0, Vite 8.3.4 and the Svelte Vite
plugin 7.3.1. Do not upgrade dependencies during the comparison. Record exact
installed versions, OS/architecture and process/artifact hashes privately for
each trial. A changed baseline, dependency, workload or budget requires a reviewed
dated amendment before measurements; preserve this record and prior results.

## Complete presentation boundary

The candidate replaces the whole contact-page body: introduction, localized
normal/invalid/stale/verification recovery, all labels and limits, saved/draft
values, native save and optional confirmation forms, and recovery/workspace links.
The existing Go document/header/footer and public-only appearance assets remain
the private frame. Measure and inspect the complete Go-wrapped response, not an
isolated component. Do not reuse the public shell's canonical, social or discovery
metadata on private pages.

Go constructs an explicit versioned presentation projection, not a serialization
of `contactPageData`, a request or a principal. Allowed fields are the `en`/`da`
locale, bounded localized display strings, bounded name/email/company values,
decimal-string revision, email-confirmation display state, and the request's
masked, rendered CSRF form value. That last value is the same public form nonce
Go already emits, not the CSRF cookie or server-side secret. Its presence is not
authority to submit: Go still validates the real cookie, session, origin and form.
Never include session cookies/tokens, broker attempts/proofs, client secrets,
issuer/subject, client identifiers, database handles or authorization decisions.
The renderer receives no ability to approve a write or select a record.

Use closed enums for display states and fixed local form/link destinations in
reviewed components. Never accept supplied markup, executable expressions, URLs or
template names. Svelte text/attribute escaping remains mandatory; no raw-HTML
escape hatch or response-rewriting proxy. The revision is never a JavaScript
number. Existing Go validation, durable compare-and-set and 200/400/409/303
semantics remain unchanged. No private responses or projections enter static
generation, a public directory, a shared cache, analytics or diagnostics.

## Runtime and failure contract

Build the component with the pinned maintained Svelte Vite plugin in SSR mode,
bundle its required Svelte runtime, and execute the fixed local ESM artifact with
Node 24. Do not implement a template compiler. Keep generated files outside both
public artifacts. The experiment package includes its actual Node executable and
runtime dependencies; it cannot rely on an undeclared host installation. Neither
the current Pages workflow nor the normal production Go image gains this runtime
or an activation mapping through this plan.

Go owns a renderer instance with an explicit close/shutdown operation. Start at
most one child lazily, only after the healthy false-default OpenFeature release
gate explicitly returns true. False, absent, wrong-type, error and stale release
evaluations retain the Go path without starting or contacting a child; the
existing identity gate can still refuse the request entirely. No fallback bypasses
that gate or SDK readiness.
The constructor accepts a reviewed local artifact path, never a request-selected
executable or shell command. Pass only the minimum non-secret process environment;
no inherited provider/database credentials or listening port. This is a supervised
trusted child, not a claim of an operating-system security sandbox.

Freeze these transport bounds before implementation:

- One in-flight render, no internal waiting queue; excess requests receive a
  generic 503. At most one renderer process exists, including during restart.
- Versioned, length-prefixed UTF-8 JSON over anonymous stdin/stdout pipes; maximum
  request 8 KiB and output envelope 64 KiB, checked before allocation/read growth.
  Require exactly one response with the fresh opaque correlation ID, correct
  version and only the declared fields. Reject duplicate/unknown fields,
  malformed/trailing data, missing/empty body and mismatched IDs.
- A render has at most 250 ms, including cold start, and honors earlier request
  cancellation/deadlines. A timeout, cancellation, protocol error, output overflow
  or child exit discards the whole buffer, kills and reaps the child, and releases
  no candidate bytes. Restart only on a later eligible request, at most once per
  second, not an automatic retry loop. There is no silent success fallback after
  an enabled renderer fails.
- Buffer the complete private document in Go before committing headers. Render
  errors produce only the existing generic unavailable page. Never log projection,
  output, child stderr or a raw provider/store error. Shutdown cancels, closes
  pipes and waits for the owned child, without killing unrelated processes.

The lifecycle/release task is filed when the gate is implemented, with a dated
review and removal criterion. The default-off experiment does not map its new gate
from the environment or enable it in production.

## Authorization after the new wait

Capture the originally authorized session, principal expiry, client membership
and record scope in Go before projecting contact data. After rendering and every
subsequent SDK/store wait, recheck current durable session existence/expiry,
identity expiry, provider health, identity/release gates and membership against
that original scope. A replacement membership is not permission to release the
previous client's buffer. Re-evaluate any displayed optional confirmation gate
and discard inconsistent output. Cancellation or any failed/unknown checkpoint
discards all private output before headers; never revive a deleted session during
a read. This is a final authorization checkpoint, not atomic wire-time revocation.

Test logout, invitation/membership revocation, expiry, role/client rebinding,
closed/error/stale gates and cancellation while a real render is in flight.
Tests control the renderer's actual bounded transport, not a hook bypassing the
production authorization path. Verification and native writes retain their own
current Go checks independently of rendered controls.

## Frozen maintenance task and protected holdouts

Before candidate code, use this concrete bilingual presentation task: change the
shared private appearance label to “Appearance” / “Udseende”, and add an accessible
contact-field hint explaining that saved details can be revised later. Inventory
the actual rendered page families `portal.html`, `contact.html`, `requests.html`,
`triage.html`, `credits.html`, both locales and each relevant error state.
Count independently edited presentation sources and declarations, not generated
files, lines removed or framework popularity. Apply the identical requested change
to separate baseline/candidate trial branches and prove every affected page.

The baseline already has one common frame. Retaining that frame plus a Svelte
contact body may produce an equal or worse source count; that is not a maintenance
improvement. Do not invent a duplicated Go baseline, omit the retained Go fallback,
or enlarge the task after seeing results. No source-count improvement means this
candidate cannot justify adoption under the existing protocol, even if fast.

Keep independent adversarial holdouts separate from the normal tuning workload:
cross-client access; revocation/rebinding during render; forged identity/state;
foreign/missing/duplicate CSRF; int64 boundary and stale revisions; expiry/logout;
closed/error/stale flags; cancellation, malformed/oversized/late/foreign responses;
contextual escaping; EN/DA; keyboard/focus/error recovery; no-JavaScript native
forms; and System/Light/Dark continuity. Freeze the candidate hash before running
holdouts. Use them only for falsification, not performance tuning; retain every
failure and any later corrective experiment as a new revision. A substantive
external review must challenge both the authority boundary and the measurements.

## Trial interface and retained evidence

The current Go fixture command remains executable from `server/` with the two
disposable database URLs provisioned as documented in its README:

```sh
GOTOOLCHAIN=local go test ./internal/identity -run '^$' \
  -bench '^BenchmarkPrivateContactJourney$' -benchtime=100x -count=3 \
  -timeout=180s -json > private-journey.jsonl
```

That in-process benchmark is a correctness baseline only. It cannot measure
separately deployed service RSS or independently restarted process trials.
Implementation must add and document a fixed, reproducible trial entrypoint
before collecting candidate evidence: `bash scripts/compare-private-contact.sh
--plan <reviewed-plan-sha> --baseline <baseline-sha> --candidate <candidate-sha>
--output <private-directory>`. This is a declared future interface, not a command
available in this plan-only commit. It must reject dirty/mismatched inputs,
non-disposable database targets, missing versions or incomplete artifacts.

The trial entrypoint builds clean immutable source artifacts and launches actual
Go authority and candidate Node processes over disposable PostgreSQL and the
synthetic TLS broker. Any shared trial harness added after the baseline must be
reviewed, frozen separately, and used unchanged on both sides; record its hash and
prove the Go comparator's production authority/presentation is unchanged. Do not
insert candidate logic or a test-only response rewriter into the comparator.

Run three independently restarted pairs, ordering Go/Svelte, Svelte/Go, Go/Svelte.
Each side gets 20 excluded warmups and 100 complete measured journeys with two
independent clients and a separately invited operator. Prove simultaneous actor
isolation and independent logout before timing each side. Retain all samples and
exit statuses, including failed/cold-start attempts. Report sign-in separately
from warm read/save/conflict/recovery phases; do not pool crypto with contact reads.

For each phase use nearest-rank p50/p95 and candidate-minus-Go paired differences
within each trial, pairing journeys by their fixed workload ordinal. Obtain 95%
percentile intervals with 10,000 bootstrap resamples of whole journey pairs, fixed
seed 199200, preserving within-journey correlation. Compute both sides' quantile
and their difference inside each resample; take the nearest-rank 2.5th/97.5th
percentiles of those differences, rather than subtracting unrelated intervals.
Compare each trial separately; three trials are not a claim of population-level
statistical power. A threshold-straddling interval is inconclusive. Retain raw
timings and the estimator source; never discard slow outliers or select a best run.

Measure complete served assets and compressed browser JavaScript, browser script
execution and no-JavaScript operation in real EN/DA journeys. Measure separately
launched Go/Node process-tree RSS every 100 ms through the same warmed workload
(not `go test` RSS), peak process count and startup/failure recovery. Use the
largest observed candidate total RSS minus the smallest observed baseline total
RSS in each trial as a conservative observed resource bound; this is not a
population confidence interval. Measure three clean builds per side per trial
with warm dependency download caches but empty build outputs, identically on both
sides; include compilation, bundling and runtime packaging. Use the slowest
candidate minus fastest baseline clean build as the conservative observed bound.
Record cold dependency installation separately rather than silently excluding an
added dependency cost. Retain sample/timer resolution; missing process samples or
unbounded measurement error makes the resource/build verdict inconclusive.

## Unchanged budgets, decision and recovery

The protocol's limits remain: additional warm contact p95 **at most 25 ms**,
private compressed browser JavaScript **at most 5 KiB**, additional warmed service
RSS **at most 128 MiB**, **at most one** added renderer process, and additional
clean-build time **at most 30 seconds**. Use the conservative upper latency
interval and the declared observed resource/build bounds in every trial, not only
pooled point estimates. The maintained
presentation-source count must strictly improve for the frozen task. Protected
authority, privacy, correctness, form and accessibility regressions are **zero**;
speed or convenience never compensates for a failure.

Adopt only after all three pairs demonstrate the maintenance objective, every
budget/floor holds, the independent review is clean, and the real rollback below
passes. A conclusive failure retains Go. Incomplete, inconsistent, missing or
boundary-straddling evidence is inconclusive and also retains Go; record that
decision on #142/#131 without claiming they are otherwise complete.

For the real rollback, keep the candidate-written contact record and the existing
client/operator cookie jars in the disposable database. Disable the experiment
gate through the supported fixture provider, stop/reap Node, and restart the Go
authority in its unchanged Go presentation mode over that same database/origin.
Without signing in again, prove each session still belongs to its original actor;
read the exact saved values/revision, make a native save, reload, refuse a stale
edit, and independently log out both clients and the operator. No database reset,
data migration or replacement session may substitute. Record source/artifact hashes
and all statuses. This is a local recovery drill, not a production rollback.

Observation window: 2026-10-19T02:00:00Z through 2026-10-26T02:00:00Z. The Agentic
Engineer owns the next evidence check by 2026-10-20T02:00:00Z. Record actual start/end
UTC before the first trial and keep sanitized measurement summaries with private
raw evidence. If implementation/review misses this window, or a pinned input
changes, preregister a reviewed amendment before collecting results. No schedule
is enabled by this date. This plan has no candidate measurements or adoption
verdict; #199 owns that unfinished delivery.

## Primary API references

- [Svelte compiler](https://svelte.dev/docs/svelte/svelte-compiler)
- [Svelte server rendering](https://svelte.dev/docs/svelte/svelte-server)
- [SvelteKit Node adapter](https://svelte.dev/docs/kit/adapter-node)
- [SvelteKit page options](https://svelte.dev/docs/kit/page-options)

No payment, agreement, work approval, API/MCP execution, provider enrollment,
production deployment, customer-data collection or Danish-law clearance is part
of this experiment. Those launch prerequisites remain separate.
