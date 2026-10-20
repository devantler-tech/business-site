# Analyze private contact timings

`contact-trial-report` checks a complete timing input and writes reproducible
statistics without contacting a database, broker, renderer or network service.
It is an explicitly invoked developer tool, not part of portal startup or the
public build. No portal gate or production mapping is added.

The report always says `decision: inconclusive` and `kind: timing-analysis-only`.
A successful exit means a report was written, **not** that the experiment passed.
Check each trial's state and failure counts. Process/RSS, restart, build, browser,
accessibility, maintenance, independent holdouts and same-state rollback evidence
remain separate requirements under [the comparison protocol](PRIVATE-FRONTEND-COMPARISON.md)
and [ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md), with
the shared-authority comparator in [ADR 0015](../docs/adr/0015-private-contact-comparator-amendment.md).
Issue #214 owns this estimator; #213 owns the complete comparison runner.

## Run explicitly

Use the exact Go version in `go.mod`, from `server/`:

```sh
GOTOOLCHAIN=local go build -o /absolute/private-directory/contact-trial-report ./cmd/contact-trial-report
/absolute/private-directory/contact-trial-report \
  --input /absolute/private-directory/raw-timings.json \
  --output /absolute/private-directory/new-report
```

Resolve parent directories first: symlink parents, symlink input, an existing
output directory and overwrite attempts are refused. The input must be an
ordinary file owned by the current user, without group/other permissions or
execute/special bits. The new report directory is owner-only and
`timing-report.json` is created exclusively with mode `0600`. A failed write may
leave a partial private directory; preserve/inspect it and choose a fresh path,
never treat partial output as a completed report. Errors omit input values and
paths. Keep raw evidence and reports private; do not add them to Git.

The report binds the exact input bytes with SHA-256 and hashes the on-disk
executable identified by `os.Executable` at invocation. Retain that built artifact;
this is an artifact receipt, not memory attestation or independent provenance.
Source revisions, dependency hashes, generations, run order and process exit
codes in the input are **declared provenance**, preserved explicitly as
`declared_provenance_not_verified`. Matching their syntax is not proof of an
actual process, clean source, build or measurement window. The collection runner
must supply independently checked receipts before any adoption decision.

## Closed input: `private-contact-timings/v1`

Only the following lowercase JSON keys are admitted. Unknown or duplicate keys,
additional documents, depth over 16, input over 16 MiB and incomplete coverage are
rejected before output is created. Numeric observations are integers, never
floating-point durations. Do not include bodies, cookies, headers, identifiers,
URLs, connection strings or provider diagnostics.

| Object   | Required fields                                                                                                                                 |
|----------|-------------------------------------------------------------------------------------------------------------------------------------------------|
| Root     | `schema`, `plan_revision`, `amendment_revision`, `authority_revision`, `baseline_revision`, `candidate_revision`, `dependency_hashes`, `trials` |
| Trial    | `number`, `order`, `runs`                                                                                                                       |
| Run      | `side`, `generation`, `artifact_sha256`, `exit_code`, `warmups`, `journeys`                                                                     |
| Journey  | `ordinal`, `sign_in_ns`, `responses`                                                                                                            |
| Response | `actor`, `phase`, `status`, `bytes`, `elapsed_ns`                                                                                               |

The frozen plan revision is `868d7acdeb4f822d0f15758e15b1539c245f5b53`,
the shared-authority amendment is `a4ee85c7b8e7a776a43dcc37071d39bbf0b956df`,
and the authority revision is `a1be78797e7886e3eb57605792027af6d4811add`.
Baseline and candidate source revisions are nonzero 40-digit hexadecimal IDs.
`dependency_hashes` contains exactly the four file/hash pairs in ADR 0014.

Trials are numbered 1–3, with orders `["go", "svelte"]`, `["svelte", "go"]`,
`["go", "svelte"]`. Runs follow those orders. Each declares a distinct nonzero
32-digit hexadecimal generation, a nonzero 64-digit artifact hash, an exit code
from 0–255, exactly 20 warmups and exactly 100 measured journeys. Ordinals are
1–20 and 1–100 respectively, with identical ordinal pairing across the two sides.

Each journey has three `sign_in_ns` observations, in client-a, client-b, operator
order. Its 21 responses appear in the following fixed actor/phase order:

| Actor                   | Phases                                                                    | Expected statuses                      |
|-------------------------|---------------------------------------------------------------------------|----------------------------------------|
| client-a, then client-b | workspace, contact, save, reload, stale-edit, recover, logout, signed-out | 200, 200, 303, 200, 409, 200, 303, 401 |
| operator                | workspace, operator, contact-denied, logout, signed-out                   | 200, 200, 403, 303, 403                |

Status is an integer from 0–599; bytes and elapsed nanoseconds are nonnegative
signed 64-bit integers. Negative or missing observations are malformed. Zero
duration represents a missing/failed timing, not a fast success. Unexpected
statuses, zero timings and nonzero process exits remain visible failures,
including in warmups. A trial with any such failure has `state: observed-failure`
and no survivor-only quantiles. A truncated collection is rejected; it cannot
be padded, filtered or called a complete comparison.

## Fixed estimator

Each trial is analyzed separately, with 40 excluded warmups and 200 measured
journeys (100 paired ordinals). Each actor's sign-in is separate from the 21
response phases. p50 and p95 use nearest rank, `ceil(n × p)`, without interpolation.
Differences are **Svelte minus Go quantiles**, not quantiles of paired differences.

The 95% percentile intervals use 10,000 resamples of whole paired journeys with
the fixed Go `math/rand.NewSource` seed `199200`, reset per trial. Each resample
draws 100 ordinals with replacement, reuses the same index vector for both sides
and all phases, recomputes both side quantiles, then subtracts them. Interval
endpoints are nearest-rank 2.5% and 97.5% quantiles of those differences.
Nanosecond outputs are decimal strings so JSON consumers do not round int64 data.
There is no outlier trimming, trial pooling, budget tuning or winner selection.

From `server/`, run:

```sh
GOTOOLCHAIN=local go test -race ./internal/contacttrial ./cmd/contact-trial-report -count=1 -timeout=90s
```

These synthetic command, pairing, uncertainty, malformed-input, failure-retention
and private-file controls do not collect real trial evidence or replace the
ordinary portal/database correctness suite.
