# Private contact maintenance inventory

The shared test-only inventory checks the presentation requirement in ADR 0014,
using the actual Go page builders/templates and the actual compiled Svelte pipe
worker. It does not start a database, authenticate a person or serve an HTTP
response. All supplied display values and the form nonce are fictional.
The checks also compare each contact body's error, confirmation and saved-value
display with the unchanged Go page builder. Native save inputs, form association
and submit behavior must remain intact. Counting several identical normal bodies
does not establish state coverage.
Each contact view supplies a nonempty fictional company and form nonce to both
real renderers. The Go contact inventory uses its own source-rendered fixtures,
not the link inventory's empty nonce. Each visible input must keep one uniquely
targeted native label with its unchanged bilingual text and limit. Refusal
controls cover lost saved values, nonce values and label associations.
Hidden or inert ancestors cannot supply counted native forms, messages or help.

## Complete frame and contact-body oracles

Every one of the 120 documents also matches a frozen parsed-document frame,
including its brand, skip link, language navigation, metadata, stylesheet and
script. The frame projection excludes only the independently checked main's
children; main attributes remain exact. The one uniquely located theme label is
projected from the validated task wording back to its original wording. The
control and its label must be active HTML, not hidden or inert. No other frame
change is canonicalized.

All 12 contact mains have an additional whole-tree oracle from the ordinary
immutable `a1be78797e7886e3eb57605792027af6d4811add` source. This covers the
main, title, introduction, enclosing section, forms, state messages and both
reload/workspace links, not only input controls. The actual compiled Svelte
worker must match that same complete contact tree in every state.
Its raw fragment must contain exactly one explicitly balanced main, apart from
complete compiler comments and literal ASCII formatting whitespace. A tokenizer
checks this closed grammar before DOM parsing: explicit document wrappers,
declarations, incomplete comments, unmatched tags and additional roots are not
repaired into acceptable fragments. The complete DOM oracle then checks every
element and attribute inside the main. Extra outside scripts, styles, links and
prose cannot enter the Go frame.

The contact projection ignores compiler comments, attribute order and ASCII
indentation directly between block containers (`main`, `div`, `section`, `form`).
Adjacent text split by compiler comments is joined. All remaining text is exact,
including label/prose boundary spaces and non-breaking characters. Ordinary and
task bodies use separate explicit expected digests. The complete task hint,
all descendants/attributes and its input association remain in the fingerprint;
no task subtree is projected away. Raw rendered evidence and parsed input trees
are never rewritten.

The frame and ordinary body tables were recorded twice from the ordinary immutable
source with the shared fictional fixtures, Go 1.27.2 and `LC_ALL=C`; the recordings
agree. The explicit task-body table starts from that same ordinary output and
adds only the registered literal name association and text-only paragraph
immediately after the name input. Two recordings agree and reproduce all 12
unchanged ordinary digests. This is expectation construction, not execution of
either separately frozen task source. No task renderer supplies an expected
value. Refusal controls
damage each complete frame and remove each structural contact element while
retaining the forms. The compiled controls alter actual worker output. Those
altered validator inputs are never comparative task samples.

## Frozen non-contact state oracle

The other 108 documents must preserve their entire parsed `main` tree, not only
their shared frame. Expected SHA-256 digests were generated from an ordinary,
immutable export of `a1be78797e7886e3eb57605792027af6d4811add`, before executing
either maintenance-task fixture. All production identity source and the link
fixture are byte-identical between that reference and this harness's base.
The registered task changes their common header outside `main`; no non-contact
body change is part of the task.

The frozen fixture is `portal_links_test.go` blob
`056f8203b2ebd224e1f9f2caed2eb411f9189f0d`. Its four template blobs are:

| Template        | Git blob                                   |
|-----------------|--------------------------------------------|
| `portal.html`   | `b2edf7e0cd15385c1d7f15a44bd2253f46ecc84d` |
| `requests.html` | `f491986e1d19ebbfd7c838f6777fd638fe09d5a9` |
| `triage.html`   | `fd22f5ee2716adb1a9d2c0cfa5736b1b8c1890f9` |
| `credits.html`  | `dd0840fb0a42a2e82a6f08eaca4a7d0838eb6ed3` |

`maintenanceCases` records the explicit state order, with all English cases
followed by all Danish cases. URI alone is not an identity: several states use
the same URI. The oracle stores 28/38/30/12 digests respectively. There are 106
distinct reference bodies: `sharing-invalid` and `sharing-conflict` intentionally
have identical portal mains in each language. This cannot establish a difference
between those aliases, and refusal controls do not pretend otherwise.

`maintenanceMainDigest` parses exactly one active HTML main. It recursively
projects node type, text, namespace, ordered attributes and ordered children into
`maintenanceDOM`, serializes it with Go's JSON encoder and hashes those bytes.
Unlike the contact label-name helper, it retains select-option text. The raw
rendered HTML and parsed tree are not modified or substituted as task evidence.

One fixture field varies: `proposalPresentation` calls `crypto/rand.Text` for a
draft form's hidden `submission_key`. Only the exact native proposal form input
is projected to `validated-fictional-draft-key`, after requiring one value, the
Go 1.27 toolchain's 26-character RFC 4648 base32 shape, and exactly one such input
in each expected draft state. Full history has no draft form. All other keys,
values and markup remain exact. Missing, duplicate, malformed or misplaced
inputs fail; independently rendered valid random keys keep the same digest.
This is fingerprint canonicalization, not a change to the comparative output.

To reproduce the reference, export that exact commit's `server/` tree into a new
private directory, add this shared test file, and add a temporary recording test.
For each of the four non-contact families, it calls `renderedLinkPages(name)`,
requires `2*len(maintenanceCases[name])` entries and records
`maintenanceMainDigest(page.markup, maintenanceDraftKey(name, index))` in order.
Run only that recording test with `GOTOOLCHAIN=local`, Go 1.27.2 and `LC_ALL=C`.
Two independent ordinary-source recordings must agree. Never regenerate the
expected table from a maintenance-task source or alter it to accept a failed
trial. Source admission separately binds the actual task inputs to the frozen
authority. The two-file overlay and original dependency identities are unchanged.

Refusal controls remove every non-contact main body, change its heading and
substitute a genuinely different same-language state while retaining the frame.
They also challenge the random-key boundary. These are validator controls, not
rendered task samples, browser tests or permission to revise comparison budgets.

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
