# Check private comparison source inputs

`cmd/contact-trial-inputs` is the offline source-admission component for #213.
It reads local Git objects, never executes the inspected revisions, and performs
no network, build, database, browser or measurement operation. Production runtime
construction and publication do not import it.

Build it from `server/` with the exact compiler in `go.mod`:

```sh
GOTOOLCHAIN=local go build -o /private/output/contact-trial-inputs ./cmd/contact-trial-inputs
```

Use a clean local source checkout containing the full frozen history and an
existing owner-only (`0700`) absolute output directory:

The checkout must be trusted own-source history. This tool is not a sandbox for
arbitrary repositories or permission to execute an untrusted author's branch.

```sh
/private/output/contact-trial-inputs \
  --repository /private/clean-source \
  --plan 868d7acdeb4f822d0f15758e15b1539c245f5b53 \
  --baseline a1be78797e7886e3eb57605792027af6d4811add \
  --candidate a1be78797e7886e3eb57605792027af6d4811add \
  --output /private/receipt-directory
```

This unchanged-source example validates identities only. It does not implement
the required maintenance task or supply a completed comparison. Each trial side
must name its own immutable maintenance-task commit when those are prepared.

The checker requires the exact merged ADR 0014 plan, the merged ADR 0015 amendment
and their recorded document hashes. Both sources must descend from the amended
shared authority `a1be787` and retain all four dependency hashes. Replacements,
abbreviated IDs, non-commit objects, missing history, dirty tracked/untracked files,
changed dependencies and changes outside the following paths are refused:

- Both sides: `server/internal/identity/web/portal.html` and `contact.html`.
- Candidate only: `web/src/lib/private-contact/Contact.svelte`.
- Both sides: `server/internal/identity/browser.go` only when its entire blob
  equals the frozen shared source with exactly the preregistered `Appearance`
  and `Udseende` literals changed to `Colour theme` and `Farvetema`. Any other
  byte change in this authority-bearing file is refused.

The repository argument must name its root, not a subdirectory that can narrow
an observation. Checkouts with configured clean/process filters are refused
before status inspection; source inspection must not execute those filters.
Changed presentation files must remain ordinary non-executable tracked files;
deletion and symlink replacement are refused. This is a closed *path* boundary,
not a semantic review of template content or proof of the identical maintenance
task. Those outcomes still need the real rendered inventory, independent review
and protected holdouts. Ignored dependency/build directories are not inspected or
executed. Any later build must use immutable Git objects, not those directories.
Ambient Git control variables, replacement objects and filesystem monitors cannot
redirect the reads. Every failed, oversized or incomplete read refuses admission.

Success exclusively creates `inputs.json` (`0600`) beneath the opened private
directory; an existing file or symlink is never replaced. The closed receipt
records scope `source-inputs-only`, plan/amendment/shared-source commits, both
actual tree identities, their complete path deltas and dependency hashes.
Diagnostics contain no paths, source text or raw Git errors. Git reads share a
30-second context deadline; the bounded local receipt write follows admission.

The frozen policy is explicit code, not a parser for prose. A changed baseline,
dependency or allowed boundary needs a reviewed, dated amendment and an updated
checker before measurement. Current main is not an interchangeable comparator.

Run its real-Git and filesystem controls with:

```sh
GOTOOLCHAIN=local go test -race ./internal/trialinput \
  ./cmd/contact-trial-inputs -count=1 -timeout=30s
```

The declared `scripts/compare-private-contact.sh` entrypoint is still unfinished.
Issue #213 retains exact tool/runtime versions, disposable database admission,
immutable
artifact packaging, unchanged host/controller identity, paired timing collection,
RSS/process/build observations, every failure, maintenance/holdout/browser proof
and same-state rollback. Freeze and review that complete runner before collecting
comparative samples. This receipt does not prove a budget, extend the observation
window, select a framework, activate the portal or authorize customer data/payments.
