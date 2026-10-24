# Prepare identity service source

The explicit offline preparer produces the frozen ZITADEL 4.20.0 source without
the unadmitted Ailerons font assets or the JSONPath machine-ID metadata selector.
It does not fetch, compile, publish an image,
configure an account or activate the portal. Ordinary builds never invoke it.

Use Bash, Git, tar, Perl's `shasum`, jq and standard Unix file tools:

```sh
work="$(mktemp -d)"
git clone --filter=blob:none --depth=1 --branch v4.20.0 \
  https://github.com/zitadel/zitadel.git "$work/upstream"
bash scripts/prepare-zitadel-source.sh --prepare \
  --source "$work/upstream" --output "$work/prepared"
```

The command reads the following committed source revision:
`8afa5dd37a8e723efb9eace8d09fd5c1cccef694` and a new output location.
It uses immutable Git objects, not the input checkout's current files. Local
changes and untracked files remain untouched and do not enter the result.
Replacement Git objects are ignored. Unsupported entry types, incomplete or
failed archive reads, changed affected inputs and existing output are refused.
The input must already contain every required Git object: lazy fetching and Git
transports are disabled. The output must be outside the input repository.
An output inside a different Git worktree is supported. Hashing uses absolute
extracted paths, not that worktree's index or root. Regular source files retain
their committed 0644/0755 modes independently of the caller's umask; every
surviving mode and the complete executable-path set are checked before completion.

The result contains:

- `source/`: committed source with two font files, two font-face sources and their
  two imports removed, plus the narrowly modified machine-ID function described
  below. Every unrelated source file is preserved, including the
  native API, protobuf, assets, statik, console and pack build graph.
- `source-preparation.json`: the upstream revision, preparation recipe/input
  hashes and exact modified paths. Overall license closure and image admission
  remain explicitly `unknown`.

No argument is an inert, successful no-op. Explicit preparation requires both
paths; there is no default destination and no overwrite mode. A successful receipt
does not prove a complete upstream build or confer release authority.
An ordinary final-move failure or catchable termination removes this invocation's
incomplete reserved output; a competing creator's output is preserved. During
reservation, signal handling defers exit until mkdir's ownership result is recorded.
An uncatchable termination such as
SIGKILL can still leave an incomplete directory, which is never a success receipt.

## Distribution boundary

The exact removed font bytes are identified in
`scripts/zitadel-source-inputs.sha256`, not included as a binary patch or fixture.
An applicable distribution grant was not established for them. This is an
admission decision, not a finding of infringement.

The `machine-id-no-jsonpath-v1` preparation profile removes the only linked use of
`github.com/jarcoal/jpath`. The exact selected module archive did not establish an
applicable distribution grant. This is an unresolved rights question, not a claim
that no grant exists elsewhere. The initial customer profile does not need this
optional selector: any non-nil metadata `JPath` setting now returns an explicit
unsupported-profile error, including an empty selector. It never silently falls
back to a different machine ID. Metadata responses without a selector retain their
exact raw bytes; hostname, private-IP and configured machine-ID behavior is unchanged.

The transform checks the complete original and resulting `internal/id/sonyflake.go`
bytes. Upstream `go.mod` and `go.sum` remain unchanged; a declaration there is not
evidence of linkage. The actual Linux compiler graph and final binary build metadata
must both exclude the module, including replacements, before a success receipt.
Reintroducing JSONPath support requires a separately admitted implementation and
compatibility validation, not removal of these exclusion checks. This restriction
does not change the portal → customer Dex → ZITADEL login architecture.

Retain the upstream root and component licenses and this modification recipe in
corresponding source. Before distributing an image, #308 still requires a clean
Linux build, complete output-bound asset/module notices, actual restricted-image
execution, corresponding-source retrieval and authenticated signed publication.
The console and Login V1 fallback appearance must be exercised on that built
distribution. This preparer alone provides none of those results.

## Validate

```sh
bash scripts/prepare-zitadel-source.test.sh
```

The test requires Go 1.26.10 and uses the real frozen public source. It exercises
the actual prepared machine-ID package for exact raw-response preservation,
explicit selector refusal and propagation through its HTTP metadata caller. It also
exercises default-off behavior,
unchanged input, complete transformed output, existing destinations, missing
source, partial metadata transforms and a plausible partial archive followed by
failure. An optional local
repository argument reuses its immutable objects without changing that repository:

```sh
bash scripts/prepare-zitadel-source.test.sh /path/to/zitadel-source
```

CI runs the network-backed control as a mandatory read-only job in both required
aggregates. The test owns and removes its disposable checkout and outputs.

## Build the complete Linux service

The explicit build profile requires a clean Linux amd64 host, Go 1.26.10,
Node 22.23.3, pnpm 10.30.3, GNU time and binutils. It prepares a fresh source
archive itself; it never resumes generated output from an earlier experiment.

```sh
bash scripts/build-zitadel-linux.sh --build \
  --source "$work/upstream" --output "$work/linux-build"
```

With no arguments it is inert. The selected upstream root/console/client/proto
workspace closure is installed from the frozen lock. Root pnpm 10.30.3 is used
in both root and console, despite the console's older package-manager declaration;
both actual versions are checked. API generation finishes before console generation
can replace their shared OpenAPI plugin. The recipe changes only the native
generator-install target's command concurrency to sequential, preserves its original
configuration, and bounds outer tasks and Go compilation independently.

The final Go compiler receives the frozen upstream version, full revision and
commit date explicitly, because the prepared archive has no Git metadata. The
output contains build logs, per-phase GNU time measurements, dependency/package
metadata, generated-file digests, an ELF/build-info record and a success receipt
only after the binary executes its version command. Failed outputs remain for
diagnosis without a success receipt; do not treat their presence as readiness.

CI uses a new standard public Ubuntu VM, admits at least 12 GiB RAM/disk, and
uploads diagnostics only—not the service binary, console or other unadmitted
distribution assets. Ordinary website jobs retain their own Node 24 toolchain.
The profile does not publish an identity image or activate login. Full asset and
linked-module notices, corresponding-source publication, restricted container
execution and actual invitation/login browser proof still belong to #308/#305.
Source correspondence is not a bit-for-bit reproducibility claim.

## Selected Login V1 icon inventory

The Linux builder requires the explicit standard-library Go verifier before its
success receipt. It inspects the generated login Go file as syntax and decodes
its ZIP data without executing or extracting it. Every archive member is bounded
and CRC-read completely. The selected four fonts must match the frozen source,
generated-output digests and actual decoded members; changed or extra members
in that font directory are refused. The statik digest must remain unchanged
across service compilation.

The verifier checks all eleven WOFF/TTF tables, the complete EOT-wrapped TTF,
the three TrueType codepoint maps, and all ten SVG-font/vector mappings. The
closed SVG path transform uses x × 32 and 960 − 32y on a fixed eighth-unit grid
only after exact immutable byte checks. This is SVG geometry and wrapper
correspondence, not a general TrueType-outline or reproducible-generation claim.

`third_party/zitadel-login-icons/` retains the complete immutable Icons8 license
and notices and the actual clipboard vector. Its notice records the added final
newline and the discrepancy with the original solid clipboard. The MIT option
is selected; the original source vectors and unmodified font binaries remain
in the prepared upstream source. Original IcoMoon settings/history remain unknown.

The deterministic `login-icons-inventory.json` is a diagnostic distribution
input retained by CI, not an identity service or complete legal admission.
All input identities and the four embedded paths are inspectable there. Once
the build has completed, repeat its inventory without changing that output:

```sh
go run scripts/zitadel-login-icons.go --inventory \
  --build "$work/linux-build" --notices third_party/zitadel-login-icons
```

No arguments are inert. Explicit mode accepts exactly one pending or completed
build receipt and emits JSON only after all checks pass. Missing, duplicate,
substituted, linked, malformed or incomplete selected evidence fails with no
success inventory. The ordinary website build never invokes this tool.

```sh
(cd scripts && go test -race zitadel-login-icons.go zitadel-login-icons_test.go)
```

These local tests use frozen public font/vector fixtures, real ZIP/Go-syntax
decoding and adverse inputs. They do not replace the separate clean Linux build
or actual post-merge inventory collection. The interface and limits are recorded
in [ADR 0022](adr/0022-output-bound-login-icon-inventory.md).

## Same-build Angular notice capture

The explicit Linux build retains `console-third-party-notices.txt` byte for byte
from the actual generated `console/dist/console/3rdpartylicenses.txt`. The matching
`console-notices-inventory.json` binds it to the identical embedded copy, both
generated-manifest entries, the complete compiler observation, frozen dependency
lock, preparation, build recipe and final binary/receipt. Both generated notice
copies and all selected observations must be present and unambiguous.

Before compilation the standard-library Go collector checks and snapshots the
raw bytes and selected input identities. After compilation it revalidates the
same inputs, refuses a replaced snapshot and emits the checked bytes rather than
reopening an unchecked path for copying. The pending receipt is renamed unchanged
only after both notice and icon inventories succeed. A failed phase, including
plausible output followed by a nonzero exit, never publishes a build-success receipt.
Neither capture mode runs without explicit arguments; the ordinary website build
does not invoke it. Existing raw diagnostics are never overwritten.

Within the fresh, single-writer build directory, reads are confined, bounded,
nonblocking for replaced special files and complete. This is not a general-purpose
defence against hostile concurrent filesystem mutation.
The raw text limit is 2 MiB; the compiler-observation limit is 64 MiB and the
selected service-binary limit is 512 MiB. The source and generated notice files
are unchanged. CI retains the raw diagnostic, compact binding record and verifier
identity, not the pre-compilation snapshot, executable, console bundle or prepared
source tree. The raw text can be inspected in that run's evidence artifact.

```sh
(cd scripts && go test -race zitadel-console-notices.go zitadel-console-notices_test.go zitadel-console-supplement_test.go zitadel-toolchain-notices_test.go zitadel-source-notices_test.go)
bash scripts/build-zitadel-linux.test.sh
bash scripts/zitadel-build-contract.test.sh
```

These controls cover actual checked-byte output, malformed/partial/duplicate
observations, changed inputs across compilation and failure propagation. Fixtures
are not the hosted Angular notice text. Capturing a notice does not establish that
each bundled package's section is complete: inspect the fresh hosted text and track
missing sections under #308. Overall `licenseClosure` and `imageAdmission` remain
`unknown`; capture grants no image publication, production identity changes or
portal activation.

## Selected console notice supplement

The Linux recipe requires the collector's explicit `--supplement
third_party/zitadel-console-notices` input in both snapshot and inventory phases.
It preserves the Angular file unchanged and retains separate verbatim terms for
the eight selected dependencies documented in that directory's README. No build
fetches replacement license text. A compiled catalogue digest and exact frozen
lock bind the selected versions, package metadata, installed source/header/license
bytes and reviewed upstream texts. Ordinary raw-only capture is not this selected
supplement profile, and ordinary website builds never invoke either profile.

The resolver admits only a normal relative pnpm alias directly into this prepared
source's `.pnpm` store. It checks every alias parent and every resolved regular-file
component; the original notice reader's symlink refusal is unchanged. qrcode is
resolved from angularx-qrcode's exact dependency edge, not a possibly hoisted copy.
The snapshot includes the checked selected bytes, metadata, alias identities and
catalogue; after compilation all are revalidated. Output uses those checked bytes,
not a subsequent copy from an unchecked path.

For opentype.js, one version-3 source map supplies a separately required
tiny-inflate notice. The catalogue binds the complete map, explicit logical
source/index, exact decoded source and retained span, alongside the root license
and both complete module/browser bundle identities. Duplicate keys, ambiguous
sources, malformed arrays, changed decoded bytes and post-snapshot replacement
are rejected. Logical source identifiers are never read as paths or fetched;
no mapping interpretation or emitted-instruction coverage is asserted. The map
and executable bundles remain excluded from diagnostic artifacts.

`console-notices-supplement.txt` preserves each component's exact bytes inside
deterministic framing. The compact supplement inventory records offsets, lengths,
hashes, exact package/source metadata, selected observations and the original
notice-inventory hash, whose receipt/binary/recipe joins are retained. Both pending
inventories are finalized before the unchanged success receipt. Any read/write,
phase or finalization failure leaves no build-success receipt. CI adds only the
exact supplement JSON to its allow-list; its existing text pattern retains the
supplement, not executable JavaScript, tarballs, node_modules or the prepared tree.

This is selected retained-text evidence, not complete distribution admission,
verification of registry provenance signatures or package reproducibility. Other
bundle sections, linked Go/toolchain terms, copied assets, corresponding source,
restricted runtime and authenticated image publication remain #308/#305 work.
`licenseClosure` and `imageAdmission` stay `unknown`; the portal remains off.

## Selected compiler-distribution terms

The explicit Linux recipe obtains `GOROOT` from its selected Go 1.26.10 compiler
and passes it as `--toolchain` to the same before/after compilation capture.
It rechecks the compiler version and distribution path before final capture.
The reader requires complete, regular, nonlinked LICENSE, PATENTS and VERSION
files from that directory. The full LICENSE/PATENTS digests are pinned; VERSION
must name Go 1.26.10 and contain only the optional distribution timestamp.

The same explicit input also checks the eight LICENSE/PATENTS files beneath
`GOROOT/src/vendor/golang.org/x/{net,crypto,text,sys}`. A complete observation of
the frozen profile's 16 selected vendor packages must name standard packages in
this actual compiler directory. Missing, extra, duplicate, partial or escaping
observations fail. Selection uses the same checked package bytes bound in the
snapshot, never a second unchecked read.

`go-toolchain-notices.txt` preserves those checked bytes with deterministic
framing. The existing console inventory's `toolchain` section records their
original relative paths, offsets, sizes and digests, plus the complete text's
digest. Its unchanged receipt, binary-digest, recipe and package-observation
bindings join these terms to this build. Revalidation rejects changed bytes
between compilation phases; failed reads/writes or existing outputs leave no
final success receipt. Ordinary raw-only capture and website builds omit this
explicit input and produce no compiler-notice diagnostic.

The vendor texts are independently read and compared byte for byte with the
checked root terms. Equal texts reuse the root spans without adding duplicate
text. The inventory retains all eleven original component identities, including
the eight vendor paths, and the sorted selected `vendorPackages` list. Each
alias carries its retained span's exact offset, size and digest. Both build
phases require the existing `--toolchain` input; no ambient discovery or separate
vendor switch is used. A changed or unreadable vendor file cannot produce a
final success receipt, even when the root files remain valid.

The existing text diagnostic allow-list retains this file; no compiler, module
tree, executable or additional JSON artifact is admitted. Hermetic fixtures are
in `scripts/testdata/go1.26.10`; the actual Linux job reads its selected installed
distribution, never the fixtures. This is not independent verification of the
compiler executable or a new reproducibility claim. Selected Go dependency,
nested-module and incorporated-source terms remain under #326; complete
distribution/image admission remains unknown under #308.

## Frozen upstream root licence

Both Linux capture phases require `--source-license`. The same collector reads
only `prepared/source/LICENSE` from this build, through the existing bounded,
regular, nonlinked checked reader. It requires the complete frozen ZITADEL
revision's 34,522-byte AGPL v3 text, SHA-256
`4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b`.
No network lookup, current upstream text or test fixture substitutes for that input.

The snapshot stores the checked bytes and their identity; final capture revalidates
them after compilation. `zitadel-root-license.txt` retains those bytes verbatim,
without framing or newline normalization. The existing inventory's `sourceLicense`
section records the original path, diagnostic name, size and digest, bound by its
unchanged source revision and nine build/recipe/compiler-graph/binary/receipt
observations. The supplement binds this final, expanded inventory too.

Missing, truncated, substituted, linked or incomplete/erroring input, changed
capture options or post-snapshot bytes, an existing diagnostic and failed output
all prevent successful completion. The complete inventory is emitted only after
every selected diagnostic is retained; the recipe finalizes no successful build
receipt after a failed phase. The existing text allow-list retains this diagnostic;
no additional JSON, executable, module cache or prepared source is uploaded.

No-argument and ordinary raw-only capture remain inert with respect to root terms.
The byte-exact public fixture in `scripts/testdata/zitadel-source/LICENSE` tests
this boundary; the real Linux job reads its actual prepared upstream root instead.
This does not cover component-specific, dependency, nested-module or incorporated
terms. Those remain under #326; complete `licenseClosure` and `imageAdmission`
remain `unknown`, reproducibility is not established and the portal stays off.

## Selected dependency root notices

Both native Linux capture phases require `--modules
third_party/zitadel-go-module-notices`. The compiled catalogue digest binds all
231 selected dependency identities and the frozen upstream go.mod/go.sum. The
catalogue was curated from checksum-matched complete module archives, not from
licence labels, current upstream files or everything listed in go.sum. It records
262 root text components, including the complete MinIO CREDITS file and qualified
or mixed-case licence names. The lib/pq notice.go and notice_test.go source files
are not misclassified as legal text.

The same collector streams the complete compiler graph from the already checked
bytes bound in its snapshot, never a second path read. It refuses incomplete or
contradictory observations, preserves original and effective replacement
identities, and reads only the resolved cache inside this fresh build. Each
selected version's cached go.mod checksum and archive checksum marker must match
the frozen sums. The recipe verifies the module cache before and after compilation.
Every admitted component must match its exact size and digest through the existing
bounded, complete, regular-file reader; an unrelated global cache is never used.

The snapshot stores the checked bytes. Capture revalidates them and reads build
information from the already checked binary bytes using Go's standard reader,
not an unchecked second path open or a partial textual command result. All
original/replaced binary dependency records, compiler and Linux/static target
settings must match. Missing, extra, duplicate or changed identities refuse
capture without a success inventory or completed build receipt.

`go-module-notices.txt` retains every component's complete verbatim bytes in
deterministic source-labelled framing. The existing console inventory's `modules`
section records the catalogue, both frozen root locks, every original/effective
identity and each component's relative source path, offset, size and digest. Its
existing nine preparation, graph, recipe, binary and receipt bindings remain in
force. Equal text still has a separate retained source identity. The existing
2 MiB text and 4 MiB snapshot bounds are unchanged. The public snapshot writer
counts the complete encoded JSON, including base64, metadata and its trailing
newline, against the same aggregate limit used by capture. An oversized
combination refuses with an explicit size-limit diagnostic before any snapshot
output or service compilation; individually valid text sizes do not bypass it.

The existing root-level text diagnostic pattern retains only this additional
text output; it does not admit executables, module ZIPs, module trees or prepared
source. No-argument, unflagged capture and ordinary website builds remain inert
with respect to these terms. Complete incorporated-source, nested-module and
compiler-vendored exceptions remain explicit #326 work. Root retention is not a
legal classifier, full licence closure, image admission, a reproducibility result,
restricted-runtime proof or customer-login activation.
