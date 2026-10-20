# Retain Go for private portal presentation

Status: proposed decision, 2026-10-20, for #199 and #142. This records the
maintenance-only outcome permitted by [ADR 0016](0016-contact-maintenance-early-rejection.md).
It does not activate a portal or change the selected public Svelte frontend.

## Decision

Retain the existing Go private presentation and stop expanding the compiled
Svelte contact candidate. The identical frozen maintenance task required edits
to **two** authored presentation files on the Go side and **three** on the
candidate side, including its real Go fallback. This fails the preregistered
requirement for a strict maintenance improvement. Faster rendering, if later
demonstrated, could not compensate for that mandatory failure.

Do not implement #213's remaining timing/resource collector for this rejected
candidate. Its existing admission, report and host deliveries are not a completed
collector or a passing comparison. Reconcile #213 as no longer planned after
this decision merges; preserve its evidence and links rather than ticking its
unimplemented acceptance criteria. A different private frontend proposal needs
new evidence and a separately reviewed plan, not a retuned task or threshold.

## Bound evidence

The task was executed only after #233's reviewed harness merged. The dated
[result on #199](https://github.com/devantler-tech/business-site/issues/199#issuecomment-6300781620)
records the complete counts and limitations. Raw logs, source admission, complete
diffs, generated artifacts and the manifest remain in the private evidence bundle;
they contain fictional fixtures, not customer records.

| Input                                      | Immutable identity                                                 |
| ------------------------------------------ | ------------------------------------------------------------------ |
| Plan, ADR 0014 merge                       | `868d7acdeb4f822d0f15758e15b1539c245f5b53`                         |
| Shared-authority amendment, ADR 0015 merge | `a4ee85c7b8e7a776a43dcc37071d39bbf0b956df`                         |
| Common authority                           | `a1be78797e7886e3eb57605792027af6d4811add`                         |
| Maintenance harness reviewed head          | `c8e5f3bfa92d770b1d38f55eea85a4d2f553fbb0`                         |
| Maintenance harness merge                  | `5368aef551598d1300f69fc3817fdc83fb7e7f39`                         |
| Go task commit                             | `b46e433daad1731c06cc3717102b53cec03cb285`                         |
| Candidate task commit                      | `9bba31668d9e23cd9e85e4008b34257e513264f9`                         |
| Private evidence manifest, SHA-256         | `ddb5285e35506bf4e575d9ece956d2d04008ada4af61ac7a7c145c5a94d4f30c` |

The four dependency hashes are the unchanged values in
[ADR 0015](0015-private-contact-comparator-amendment.md). Both complete task
diffs are counted against the common authority. The candidate task's parent is
the Go task commit; comparing only against that parent would hide the fallback
changes and produce the wrong count.

Both disposable task exports received exactly the same two test files from the
merged harness. After execution, all 521 original source blobs on each side still
matched that side's frozen task commit. The source-admission tool accepted the
exact input identities, unchanged dependency hashes and allowed presentation
deltas. No response substitution or post-result task-source repair was used.

| Actual rendered task check    |    Complete count | Exit |
| ----------------------------- | ----------------: | ---: |
| Go comparator                 |     120 documents |    0 |
| Candidate's Go fallback       |     120 documents |    0 |
| Actual compiled Svelte worker | 12 contact bodies |    0 |

Each 120-document inventory includes 28 portal, 12 contact, 38 requests, 30
triage and 12 credits documents across both languages and the declared error
states. The compiled worker covers six contact views in both languages. The
candidate used its pinned component build and actual supervised worker, not a
replacement HTML fixture. Complete logs, counts, exits, build output and notices
were retained; no task failure or sample was discarded.

The authored task changes were:

- Go: the shared appearance labels in `server/internal/identity/browser.go` and
  the bilingual name hint and input association in
  `server/internal/identity/web/contact.html`.
- Candidate: those same two files **plus**
  `web/src/lib/private-contact/Contact.svelte`. Disabled/error paths still need
  the genuine Go fallback, so its template is not optional maintenance.

Generated files, test-only overlays, deleted lines and individual declarations
are not alternative winning metrics. The retained complete diffs expose the
changed declarations separately: two shared theme literals; the native
name-input association and bilingual hint; and, additionally for the candidate,
the compiled name-input association and bilingual hint. The initial result
received adversarial self-review. A separate read-only agent then independently
verified the retained hashes, full task diffs, original source blobs, dependency
pins and complete rendered counts without finding a substantive mismatch.
Neither is an external-provider review of the measurement bundle.
The decision requires substantive current-head review through the repository's
normal review lanes before merge.

## What this does not establish

This is a conclusive failure of one candidate's mandatory maintenance objective,
not a complete benchmark, a verdict against Svelte generally or proof that Go is
the best frontend for every product. The public Svelte selection is unchanged.

The exercise did **not** measure three timing pairs, browser work, process-tree
RSS, clean-build comparisons, protected holdouts, same-record rollback or real
provider/staging/customer readiness. Test execution durations are not latency
measurements. Prior correctness tests remain separate evidence; they cannot be
relabelled as this exercise's missing comparative observations. No passing
performance, accessibility, authentication or rollback result is asserted here.

## Removal and programme boundaries

This decision-only change removes no implementation or test. Complete #207 by
removing the experimental consumer, gate, worker and unused collection machinery
in a separately reviewed cleanup. Preserve the immutable plans and evidence.
The existing 2026-10-26 removal review date is not extended.

Cleanup must retain Go's shared private frame and native writes, complete
response buffering, original durable-session/client revalidation after SDK or
store waits, read-only session semantics, and the ordinary authorization,
expiry, cancellation, origin, CSRF and bilingual form controls. Move any shared
security regression out of an experiment-specific test before retiring that
test. Keep the ordinary 120-document Go maintenance inventory; do not remove
public Svelte dependencies or production build checks with the private worker.

#199 stays open through removal and its explicit outcome reconciliation. #142
can consume this narrow private-presentation decision but remains open for its
other acceptance criteria. #131 remains the portal programme, not a synonym for
the frontend experiment. Nothing here clears identity-provider, deployment,
recovery, privacy, legal, payment or customer-activation prerequisites. Private
release gates remain default-off, with no new environment mapping or schedule.
