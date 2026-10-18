# Private frontend comparison protocol

## Scope and frozen comparator

The comparator is the working Go portal at
`0fd607782623d6e469c511c5ebbab194ab676ac1`, including its shared private layout.
The public Svelte site is not evidence of authenticated application behavior.
Issue #142 owns the eventual decision; #197 supplies the reproducible Go baseline.
This protocol is committed before any Svelte private rendering experiment.

The measured task is sequential sign-in, workspace, contact read, native save,
reload, stale-edit refusal, recovery reload and logout for two fictional clients, followed by a
separately invited operator's workspace, operator page, client-route refusal and
logout. Actors have independent cookie jars over the same disposable database
and synthetic TLS broker. No fixture runs concurrently with another database test.
Every measured stale-edit refusal is followed by a real read that must retain the
saved revision and values, not merely an expected error status.
Sign-in includes the real synthetic code exchange and signing cost and is reported
separately from page/form response samples. No customer or real-provider data is used.

## Collection and interpretation

Use the exact Go version in `go.mod` and the disposable PostgreSQL 17 fixture in
`README.md`. Run normal correctness/race tests separately. From `server/`, run:

```sh
GOTOOLCHAIN=local go test ./internal/identity -run '^$' \
  -bench '^BenchmarkPrivateContactJourney$' -benchtime=100x -count=3 \
  -timeout=180s -json > private-journey.jsonl
```

Each benchmark invocation resets the fixture, provisions two fictional clients
and an operator, performs twenty excluded warmup journeys and records exactly
the requested number of complete journeys using `b.Loop()` (no one-iteration
calibration report). Require exactly three successful 100-journey reports.
These repetitions share one test process; they are not independently restarted
service trials. Keep the raw JSON test stream and its
exit status; a partial stream or missing/failed terminal package event is not a
baseline. Record the immutable source SHA, clean-worktree check, UTC interval,
Go/PostgreSQL versions, OS/architecture and command alongside that private output.
Concatenate the test stream's `Output` events before reading report lines: one
large JSON report can span several events. Do not count an event as a whole report.
Do not publish machine names, connection strings or raw provider/HTTP diagnostics.

The `private-journey/v1` report contains only fixed actor/route labels, statuses,
elapsed nanoseconds and response-body byte counts, plus Go version/OS/architecture.
It excludes bodies, cookies, headers, CSRF values, callback parameters and identity
proofs. Body bytes are actual decoded HTTP bodies, not TLS wire bytes, compression
estimates, browser execution work or process memory. This in-process TLS fixture
is a correctness/comparison harness, not a deployed service or production SLA.
Harness RSS must not be called deployed Go service RSS.

## Candidate experiment, declared before implementation

The smallest candidate is contact presentation only over the same Go handlers,
store, sessions, flags and native forms. Compare the complete candidate artifact
with this comparator, not an isolated amount-formatting or public-page sample.
Do not add a Node service merely because SvelteKit supports one. First review the
typed presentation boundary, runtime cost, origin/cookie/CSRF flow and rollback.
Any added rendering wait requires bounded buffered output, cancellation/deadline
handling and fresh authorization/session/flag checks before private bytes leave Go.
No private response may enter public prerendering or a shared cache.

Use three independently restarted paired trials on the same host/runtime/database
conditions, alternate candidate/baseline order, and retain twenty warmups plus one
hundred measured journeys per side. Preserve all raw samples. Report per-phase
p50/p95 and paired uncertainty; do not pool sign-in crypto with warm contact reads.
Do not select a winner from one sample or retune against held-out failures.

The candidate must reduce independently maintained presentation sources for one
declared bilingual visual change; equivalent source count is not an improvement.
Additional warm contact p95 must be at most 25 ms, private compressed browser
JavaScript at most 5 KiB, additional warmed service RSS at most 128 MiB, at most one
additional renderer process, and additional clean build time at most 30 seconds.
Collect browser execution, complete asset and separately launched process evidence
before assessing those budgets; this baseline runner cannot decide them alone.

Protected floors permit zero authorization, privacy, form or accessibility
regressions. Keep independent holdouts for current revocation during rendering,
cross-client access, forged identity/state, foreign/missing CSRF, stale revisions,
session expiry/logout, closed/error flags, cancellation, escaping, EN/DA,
keyboard/focus/error recovery and System/Light/Dark continuity. A correctness
failure stops adoption regardless of speed. An incomplete read is inconclusive,
never green. No payment, approval, queue or maintenance policy changes are allowed.

Rollback must restore the Go renderer over the same saved record and session
without a data migration; re-exercise native read/save/logout after rollback.
The experiment remains default-off, with no production mapping, paid enrollment
or launch clearance. Negative or inconclusive evidence retains Go. Only complete
paired evidence, independent review and a recorded decision can settle #142;
the real-provider, deployment/recovery and Danish-law gates remain separate.
