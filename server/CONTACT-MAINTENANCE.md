# Private contact maintenance inventory

The shared test-only inventory checks the presentation requirement in ADR 0014,
using the actual Go page builders/templates and the actual compiled Svelte pipe
worker. It does not start a database, authenticate a person or serve an HTTP
response. All supplied display values and the form nonce are fictional.
The checks also compare each contact body's error, confirmation and saved-value
display with the unchanged Go page builder. Native save inputs, form association
and submit behavior must remain intact. Counting several identical normal bodies
does not establish state coverage.

Ordinary source validation checks the unchanged presentation:

```sh
cd server
GOTOOLCHAIN=local go test -race ./internal/identity \
  -run '^TestContactMaintenance' -count=1 -timeout=30s -v
```

The required compiled-renderer suite also selects
`TestCompiledContactMaintenance` through its existing `^TestCompiledContact`
selector. It requires the actual Node 24 executable and compiled worker, never
an HTML substitute. Missing or incomplete artifacts fail. The ordinary Go suite
does not need Node for its template inventory.

## Frozen task exercise

ADR 0016 permits this bounded early rejection only after its reviewed merge.
Its negative decision cannot establish adoption or clear an unmeasured budget.

Freeze the reviewed merge and individual test-file hashes before exercising the
task. Reuse these exact two files as the separately identified shared harness:

- `internal/identity/contact_maintenance_test.go`
- `internal/identity/contact_maintenance_integration_test.go`

Create separate baseline and candidate source fixtures from ADR 0015's immutable
shared-authority commit. Apply ADR 0014's identical bilingual task to source,
never to rendered responses: the common appearance label and native contact
hint on both sides, plus the candidate's compiled contact component. Keep the
Go fallback. Change no authority or dependencies and commit each task fixture.
Use the merged source-admission command before adding the shared test overlay;
retain its receipt and complete source diff separately from the harness.

Copy only the two frozen test files into each disposable build tree after source
admission. Record their hashes again and prove that all original source blobs
remain unchanged. This is an identified shared test overlay, not a claim that
the augmented build tree is a clean source input. Build the candidate component
with its pinned toolchain and dependencies and retain its executable/bundle
identities. From each fixture's `server/` directory:

```sh
GOTOOLCHAIN=local go test -race ./internal/identity \
  -run '^TestContactMaintenance' -count=1 -timeout=30s -v \
  -contact-maintenance-task

# Candidate only: absolute paths to the real admitted Node and compiled worker.
RENDERER_TEST_NODE=/absolute/node24 RENDERER_TEST_ARTIFACT=/private/worker.mjs \
GOTOOLCHAIN=local go test -race -tags=rendererintegration ./internal/identity \
  -run '^TestCompiledContactMaintenance$' -count=1 -timeout=30s -v \
  -contact-maintenance-task
```

The option is a test-binary input, not an application release flag. An unchanged
source presented as a completed task fails; a successful ordinary run without
the option does not prove the task. Retain complete output and every exit status.
Go must report 28 portal, 12 contact, 38 request, 30 triage and 12 credit documents
(120 total); the actual Svelte worker must report 12 contact bodies. These cover
both languages and the maintained normal/error variants. The Go inventory keeps
the unmodified real frame, not the frame-substitution probes used by other tests.

Count independently edited **authored presentation files** from each admitted
source diff. Report individual declarations separately. Generated files, the
shared test overlay, removed lines and framework popularity are not substitutes
for that metric. Missing coverage is inconclusive, not an improved count.

## Evidence boundary

The checks prove rendered markup, native form attributes, limits and the name
hint's unique accessible association; they are not browser accessibility proof.
They establish neither authenticated routes nor actor isolation, timing, RSS,
clean-build budgets, holdouts or same-state rollback. Those remain separate
requirements of the complete comparison. No customer, identity, payment or
deployment gate is enabled. A conclusive source-count failure disqualifies this
candidate under ADR 0014; it does not turn unperformed budgets into passing ones.
