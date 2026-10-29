# Prepare identity service source

The explicit offline preparer produces the frozen ZITADEL 4.20.0 source without
the unadmitted Ailerons font assets or the JSONPath machine-ID metadata selector.
It also applies the exact pinned security dependency updates described below.
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
  below, and the two pinned dependency metadata files. Every unrelated source file is preserved, including the
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

The `native-neutral-provider-icons-v1` profile retains the provider names, native
authentication/registration links and form/event bindings. It replaces decoration
in eleven Console HTML, two Login HTML and five SCSS consumers with the existing
Line Awesome and Login arrow glyphs, then removes exactly nine Console and seven
Login image paths. No artwork, font, glyph, dependency or licence grant is added.
The thirty-four original inputs and all eighteen transformed consumers have fixed
identities. The four removed Apple SVGs are the only executable paths subtracted
from the complete surviving-mode comparison.

The native slots are 28 pixels in the table, 36 in the chooser/editors and 46 in
Login/registration with a centered 25-pixel glyph. Decoration is hidden from
assistive technology and adds no focus stop. Surviving ZITADEL-brand dark/light
rules remain active. The complete copied-asset census rejects provider images
and stale URLs in either Console output, while the existing statik decoder
checks the actual Login archive and registration template. These observations
are bound across compilation; they do not enable social federation or admit the
remaining branding/distribution under #384/#308.

The `machine-id-no-jsonpath-v1` preparation profile removes the only linked use of
`github.com/jarcoal/jpath`. The exact selected module archive did not establish an
applicable distribution grant. This is an unresolved rights question, not a claim
that no grant exists elsewhere. The initial customer profile does not need this
optional selector: any non-nil metadata `JPath` setting now returns an explicit
unsupported-profile error, including an empty selector. It never silently falls
back to a different machine ID. Metadata responses without a selector retain their
exact raw bytes; hostname, private-IP and configured machine-ID behavior is unchanged.

The transform checks the complete original and resulting `internal/id/sonyflake.go`
bytes. The JSONPath module remains declared in `go.mod` and `go.sum`; a declaration there is not
evidence of linkage. The actual Linux compiler graph and final binary build metadata
must both exclude the module, including replacements, before a success receipt.
Reintroducing JSONPath support requires a separately admitted implementation and
compatibility validation, not removal of these exclusion checks. This restriction
does not change the portal → customer Dex → ZITADEL login architecture.

The `go-jose-4.1.6-x-net-0.62.0-csrf-1.7.3-v1` dependency profile applies the exact native Go
1.26.10 metadata for `go-jose/v4` 4.1.6, `golang.org/x/net` 0.62.0 and
`gorilla/csrf` 1.7.3: three module lines and six added checksums. Complete original and resulting hashes are checked
offline. The preparer runs only the maintained standard-library Go decoration
transform with `GOTOOLCHAIN=local`; it never downloads a compiler or executes
upstream Go source. Other dependency selections,
the compiler and the upstream 4096-bit RSA key limit are preserved. Original unused
checksum entries remain as produced by the native updates. The selected modules,
compiler metadata and notice catalogue must agree on all three patched versions. Dirty input
checkout metadata is ignored and preserved, just like other working-copy files.
The login interceptor explicitly marks requests as plaintext only when the existing
external TLS setting is disabled. The patched library's HTTPS Origin/Referer and
token checks remain active for TLS-facing deployments; cross-origin and missing-token
requests are rejected in either mode.

Native CI builds pinned `govulncheck` 1.8.0 with the separate verifier compiler,
then analyzes the actual frozen Linux executable in binary mode against
`https://vuln.go.dev`. Findings and analysis errors fail the job before later
image/replay proof; the ordinary text diagnostic is retained in the existing
compact evidence. Do not use JSON mode as a success gate or suppress a finding.
Standard output and standard error share that retained report, including early
scanner errors; redirection preserves the checker's original exit status.
The actual packaged executable preserves function symbols and omits only DWARF
debug data. A complete native symbol read must identify its main entrypoint before
the checker runs; empty, failed or partial symbol reads cannot produce a build receipt.
Binary symbol analysis is scoped to the executable's build configuration and
cannot establish exploitability, complete source reachability or admission;
the selected-version checks remain independent. See the
[official checker documentation](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).

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
go run scripts/zitadel-login-icons.go scripts/zitadel-text-fonts.go --inventory \
  --build "$work/linux-build" --notices third_party/zitadel-login-icons
```

No arguments are inert. Explicit mode accepts exactly one pending or completed
build receipt and emits JSON only after all checks pass. Missing, duplicate,
substituted, linked, malformed or incomplete selected evidence fails with no
success inventory. The ordinary website build never invokes this tool.

```sh
(cd scripts && go test -race zitadel-login-icons.go zitadel-text-fonts.go zitadel-copied-assets.go zitadel-login-icons_test.go zitadel-text-fonts_test.go zitadel-copied-assets_test.go)
```

These local tests use frozen public font/vector fixtures, real ZIP/Go-syntax
decoding and adverse inputs. They do not replace the separate clean Linux build
or actual post-merge inventory collection. The interface and limits are recorded
in [ADR 0022](adr/0022-output-bound-login-icon-inventory.md).

## Same-build text-font attribution

The same entrypoint's explicit `--text-fonts` mode retains the frozen 54 text
fonts: twenty Lato, twelve Roboto, four PT Sans and eighteen Raleway files.
`third_party/zitadel-text-fonts/catalogue.json` binds each original upstream Git
blob, complete file digest and expected generated path. Its output set was
collected from the actual native build at source `817d236be97217caac6abc6f4a8fdf896185699f`,
CI `40480523774`, artifact `12725125374`; the collector rechecks actual bytes on
every build. The catalogue is independently bound to upstream commit
`8afa5dd37a8e723efb9eace8d09fd5c1cccef694`, not to current replacement fonts.

The parser retains all five selected Windows Unicode BMP, en-US records
(platform 3, encoding 1, language `0x0409`, name IDs 0, 3, 5, 13 and 14), including
full copyright, reserved-name, identity/version and license strings. Each string
keeps its original byte span and SHA-256. This is explicitly English metadata,
not an inventory of every language or a font-rendering validation. Duplicate or
overlapping tables, ambiguous records, incomplete strings and invalid UTF-16
fail. Text is never truncated to a display excerpt.

The retained raw notice includes all original declarations, the exact Lato
`OFL.txt` from the prepared source, and the complete Apache License 2.0 text.
The original Lato document's copyright header stays labelled as Lato's; each
other font retains its own embedded copyright and reserved-name declarations.
The selected older Roboto files declare Apache-2.0. The twelve declarations
must not be replaced by the terms of a contemporary Roboto distribution.

Before compilation, `--snapshot` checks the actual source fonts, 84 selected
copies, decoded Login V1 archive, both compiler package selections, preparation,
recipe, verifier sources and retained terms. After compilation, `--inventory`
requires that same snapshot, the generated manifest and the final binary's
receipt. The 44 Login resources must occur byte for byte in the decoded archive;
the twenty embedded Console text-font paths must be selected by the compiler.
The other twenty Console generated copies must also match. Thirty-two existing
Console icon-font copies are separately byte-bound to close the observed font
set; their attribution/admission remains a distinct review under #308.

`text-font-notices.txt` and `text-font-notices-inventory.json` enter the closed
image package. The inventory binds the raw text, exact binary and final receipt;
the retained-input replay receipt binds that inventory. The two verifier files,
catalogue and Apache text are literal corresponding-source records. The original
Lato OFL is already part of the complete prepared source.

```sh
go run scripts/zitadel-login-icons.go scripts/zitadel-text-fonts.go \
  --text-fonts --verify --build /private/build \
  --recipe "$PWD/scripts/build-zitadel-linux.sh" --notices "$PWD/third_party/zitadel-text-fonts"
```

Verification is read-only and requires completed evidence. No-argument operation
and normal website builds remain inert. Fixture controls cover failures;
hosted native output supplies actual build evidence. This increment preserves
`licenseClosure: unknown`, `imageAdmission: unknown` and `reproducible: false`.
It does not establish a distribution-wide grant, public source retrieval,
signed image release, production federation or portal activation. See
[ADR 0025](adr/0025-output-bound-text-font-notices.md).

## Copied Console icons, flags and translations

The explicit `--copied-assets` companion retains the complete 1,561-member Line
Awesome subtree, four Material Icons formats, 271 flag SVGs and 22 translation
JSON files. Its frozen catalogue closes 1,858 original identities, 3,716 generated paths and 1,858
selected Console embeds. This includes the 32 icon-font outputs that the
text-font collector records separately; combining the inventories preserves
their existing byte identities and adds their original attribution.

The three original grants are retained verbatim: the complete Icons8 license
with MIT selected, Material Icons' original Apache-2.0 text, and the flag-icons
MIT text and copyright. Both original Line Awesome CSS files are retained
beside their differing frozen ZITADEL identities. Available selected copyright,
identity, version and license name records from the four TTF inputs retain
their source spans and complete strings; absent icon-font records are not
invented. The five-record requirement for the separate text-font profile stays
unchanged. See [the frozen provenance](../third_party/zitadel-copied-assets/README.md).

The translations contribute 3,281,240 unchanged original bytes. Each strict UTF-8
JSON object has a frozen Git-blob and byte identity, two exact generated copies
and a selected Console embed. The complete frozen source LICENSE, LICENSING.md
and CONTRIBUTING.md are retained and compared with the prepared source. These
describe the source's default license, exceptions and contribution context;
they are not independent certification of every contributor's rights. The
complete JSON bodies stay in corresponding source, rather than notice text.

Before compilation, the snapshot checks the actual connected pnpm aliases,
exact package metadata, frozen lock, original license and asset bytes, paired
generated files and compiler selection. Package links allow only the selected
single-hop pnpm destinations, and are checked again after observation. Full
original-archive capture and integrity verification remain the existing
selected-pnpm source adapter's later boundary; this collector does not replace
that adapter or reinterpret its archives.

After compilation, capture requires the unchanged snapshot and generated
manifest, and independently repeats the text-font collector's completed
preparation/toolchain/recipe/binary/receipt join. The retained font inventory
and notice must also agree exactly. Pending-only capture creates the copied
notice exclusively. Completed `--verify` compares both retained notice and
inventory, emits nothing on failure, and makes no repair.

```sh
go run scripts/zitadel-login-icons.go scripts/zitadel-text-fonts.go scripts/zitadel-copied-assets.go \
  --copied-assets --verify --build /private/build \
  --recipe "$PWD/scripts/build-zitadel-linux.sh" --notices "$PWD/third_party/zitadel-copied-assets"
```

The copied notice and inventory enter the closed 17-file restricted image
payload; the retained-input replay receipt binds the new inventory. The helper,
catalogue, three grants, two original CSS files and three source contexts are ten of the 28 literal
source/build records. The complete prepared upstream profile stays unchanged.
Fixture controls and a full frozen-byte/compiler-record replay are diagnostic
checks; only the hosted build observes actual compilation and native execution.

This scope does not clear other graphics/provider marks, complete distribution
licensing, durable public corresponding-source retrieval, signed image release,
production federation or portal activation. `licenseClosure`, `imageAdmission`
and reproducibility remain unchanged. See [ADR 0026](adr/0026-output-bound-copied-console-assets.md).

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

## Selected dependency root and nested notices

Both native Linux capture phases require `--modules
third_party/zitadel-go-module-notices`. The compiled catalogue digest binds all
231 selected dependency identities and the frozen upstream go.mod/go.sum. The
catalogue was curated from checksum-matched complete module archives, not from
licence labels, current upstream files or everything listed in go.sum. It records
262 root text components, including the complete MinIO CREDITS file and qualified
or mixed-case licence names. The lib/pq notice.go and notice_test.go source files
are not misclassified as legal text.

The same catalogue also retains eleven complete nested notice files (14,604
original bytes) in nine selected modules: goja's Lucent and V8 terms, go-jose's
JSON terms, go-webauthn's revoke terms, golang-lru's list terms, compress's S2
terms, Prometheus's gddo terms, River's Levenshtein terms, testify's difflib and
spew terms, and the Google API module's uritemplates terms. Each nested entry
explicitly names a module-relative package beside or below its notice. That package
must occur in the complete checked compiler graph, in the exact cache directory
implied by its original import path. Replacement modules retain the original
import namespace while using the effective cache identity. An unselected
package, unrelated directory, traversal, alias or changed notice refuses capture;
a root component cannot masquerade as a nested package component.
Prometheus retains its ancestor gddo notice through the selected httputil package;
the notice directory itself does not have to be a compiled package. This explicit
association does not infer applicability to unselected or sibling directories.

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
identity and each component's relative source path, offset, size and digest.
Nested components also retain their selected-package provenance. Its
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
with respect to these terms.

The catalogue's incorporated-source entries retain seventeen verbatim spans from
twelve actual compiler-selected Go files. A whole-file size/hash and the exact
package's GoFiles membership are required before slicing. Terms references name
already retained complete components; equal Go BSD bytes are reused with explicit
source relationships. Eight additional full or extracted external notice files
retain the assessed MIT, zlib, Unicode and PSL material. Read
`third_party/zitadel-go-module-notices/README.md` for their precise provenance.

The PSL entry additionally checks all three actual EmbedFiles and the ignored
generator, including their complete sizes/hashes. The complete historical list
is a checked corresponding-source input in this repository, with a recipient
source reference in the retained text and inventory. Its data and generator code
are not notice diagnostics and the collector never executes that generator.
Static semantic correspondence does not claim byte-for-byte regeneration of its
optimized tables. Unicode and zlib reference terms do not establish the precise
historical source revisions copied by their downstream adaptations.

Wrong or unselected source, changed or overlapping spans, unrelated terms,
changed external inputs, linked files, failed reads and contradictory generator
selection refuse the same snapshot/capture path. Full corresponding-source
packaging and clean replay remain under #343/#308. This is assessed text/input
retention, not a legal classifier, full licence closure, image admission,
reproducibility, restricted-runtime proof or customer-login activation.

## Explicit prepared-source snapshots

`scripts/zitadel-source-bundle.go` admits the complete frozen prepared source
after the Linux recipe's API serialization change and before installation. Its
`--seal-prepared <source> <new-snapshot>` operation compares all sorted file names,
bytes and executable modes with the frozen profile. It preserves empty files and
maintained protobuf bootstrap sources. A substituted preparation or lock fails.
No arguments perform no operation.

`--pack <snapshot> <manifest-sha256> <new-tar>` writes a closed USTAR archive and
reports its separate SHA-256. `--unpack <tar> <archive-sha256> <manifest-sha256>
<new-snapshot>` rechecks exact ordered regular members, complete bytes and declared
metadata. The output must be new. Traversal, links, duplicates, missing or extra
members, changed identities and trailing input fail without publishing a success
receipt. `--verify <snapshot> <manifest-sha256>` reads the closed snapshot without
modifying it. The archive digest is external; it never rewrites the manifest.

These operations do not install dependencies or run the upstream build. A
prepared-source round trip establishes only that source snapshot's correspondence.
The Linux builder's optional `--capture-source` flag invokes `--capture-build`
immediately before installation. It retains the preparation receipt, original and
serialized API configuration, seven-input hash record, existing third-party
catalogues and terms, and a dated modification record. It also retains every one
of the 37 approved authored paths and three scoped terms documents listed in
`third_party/zitadel-build-tools/scope.json`. Twelve existing aliases are reused.
The Console supplement catalogue and historical public-suffix original are also
retained at their literal paths. The closed inventory contains 58 records within
the unchanged 64-record bound. That date belongs to the frozen
modification set, not capture time; update it when the declared modifications change.
Tests recapture identical inputs two simulated days apart to preserve stable seals.
A complete file manifest
keeps those fifty-eight records separate from the upstream source. The completed build
receipt binds the preserved source/archive identities to its actual binary and
recipe, after a full post-build readback. The two notice originals remain inert
records; retaining them does not assemble the complete 41-input notice offer or
establish release admission. Default invocation omits this capture.
CI explicitly enables it and unpacks the actual archive into a new reservation.
Only the compact manifest, round-trip result and completed build receipt enter
diagnostics; the source archive, recipe bodies and executable remain excluded.

`--restore-build <snapshot> <manifest-sha256> <new-build-root>` restores the
checked source files and their executable modes into the original native layout.
It retains all fifty-eight literal records and places only the four fixed build inputs at
their original locations. It does not execute retained recipes or record-supplied
paths. Existing output, an output inside the snapshot, incomplete records and
changed, missing or extra bytes fail. The snapshot is rechecked after copying.
This operation authenticates the externally sealed file set, not an arbitrary
snapshot's suitability for the frozen build; empty directories are not source
members, while valid empty files remain preserved.

`--verify-build-records <snapshot> <manifest-sha256> <repository>` independently
rechecks the complete sealed snapshot and the current repository bodies selected
by the literal capture map. Missing, linked, unsupported-mode or changed records
fail without a success result. Both fresh capture and prepared/native replay
perform this readback before completing their build receipt. Retained records
remain 0644 audit data: no script, Dockerfile or helper module is executed or
restored over the upstream service module. Only the four existing fixed native
inputs receive additional restoration destinations. The scoped grant is unchanged;
this capture does not complete bootstrap-dependency or image admission.

`--restore-tools <snapshot> <manifest-sha256> <new-tool-source-root>` restores
the 37 approved authored files and three scoped terms documents at their original
paths, including `server/go.mod`, `server/go.sum` and both verifier command
directories. Its fresh private root is separate from the sealed snapshot and
the upstream service module. All restored files remain inert 0644 data.
`--verify-tools <snapshot> <manifest-sha256> <tool-source-root>` rechecks that exact
closed layout, complete bodies, canonical modes and private root. Missing, extra,
linked, changed or executable files fail without a successful receipt. Existing
outputs are preserved, and failed restoration removes only its own reservation.
The receipt binds the upstream source seal and complete restored member digest;
`goVerifiersRebuilt` remains false. These operations do not acquire dependencies,
run a compiler or change the native replay's executable inputs.

The Linux entrypoint also accepts the mutually exclusive
`--build --prepared-source <snapshot> --prepared-sha256 <manifest-sha256>
--output <new-build-root>` mode. It restores those records without rerunning
preparation or replacing the original API configuration. Before installation it
uses the existing frozen capture admission and requires the complete source,
transformation and current-recipe seal to match. It then enters the same native
install, generation, console, compilation and notice path. Its `sourceInput`
receipt explicitly keeps `nativeOfflineReplayProved:false`: restoring source
alone supplies neither dependency inputs nor network-denied compilation.
The final recipe hash must still equal the admitted source input, including when
an installation changes the recipe before notice capture. CI rebuilds from the
actual unpacked archive with fresh acquisition caches and retains the resulting
receipt beside the original. It checks the source, recipe, toolchain and preparation
joins without claiming equal binary bytes or complete offline replay.

Both source modes use a fresh private Go module cache, compiler cache and GOPATH,
including the checksum database's state, plus a build-owned pnpm store and npm
cache. Their schema cache and configuration directory are also build-owned, and
an inherited schema-service token is discarded. Inherited npm configuration is
discarded; empty user/global configuration,
the public npm registry and the frozen project's existing configuration apply.
Native installation disables only the side-effect cache, preserving lifecycle
execution and the existing build-approval policy. Capture and cold replay must
regenerate those outputs; a source package must not substitute cached build outputs
for their original archive inputs.
The build selects the public Go proxy and checksum database explicitly, retaining
checksum verification and no private/insecure bypass patterns. These boundaries
prevent an existing host cache or disabled verification setting from silently
supplying acquisition inputs. They are not captured-dependency or offline proof.

The same explicit flag also retains the selected service's complete Go ZIP and
go.mod inputs after compilation and final module verification. The otherwise
inert command in `server/cmd/zitadel-module-sources/` checks the frozen catalogue and locks,
every original/effective replacement, and equality between the root command's
dependency list and the complete compiler stream. It copies only the 231 selected
effective module pairs from this build's private cache. Go's maintained module
library recomputes full archive/go.mod content sums; `.ziphash` is not trusted.

The retained directory contains 462 source payloads and four literal metadata
records plus an externally sealed manifest. Readback verifies complete bytes,
membership, sizes and canonical file modes, rejecting linked inputs and partial
I/O. CI copies the actual retained package into a fresh directory and verifies
it again. Its manifest and compact round-trip result are diagnostics; source ZIPs
and go.mod payloads are excluded. The completed binary/recipe receipt binds the
narrow `sourceSnapshot.goModules` result. The verifier's checksum-pinned module
library is a build-tool acquisition in the existing server Go module, not an
identity service dependency. CI tests/builds that command with Go 1.27.3 before
selecting the unchanged upstream Go 1.26.10 compiler. Source capture requires
`--module-source-verifier /absolute/built-command`; the builder checks its command
identity/compiler and records its unchanged executable digest in
`sourceSnapshot.goModuleVerifier`. The native build never downloads or switches
toolchains to run this verifier.

This package is selected service-Go retention, not generator-tool or pnpm/workspace
source capture. Both `dependencyInputsCaptured` and `nativeOfflineReplayProved`
remain false. No ordinary build, publisher, deployed login or customer access is
enabled by this capture.

The separate generator checkpoint reads all fourteen real API generator binaries
immediately after generation, before console installation replaces OpenAPI 2.28
with 2.22. It preserves complete native Go main/dependency/replacement/settings
records and before/after byte hashes, requires the exact frozen tool identities,
compiler and platform, and constructs an effective union by path **and version**.
This includes gci despite its omission from upstream's output list, and the nested
gRPC generator module. The builder checks the sealed checkpoint again after the
build; CI verifies a copied checkpoint and joins its compact result to
`sourceSnapshot.generatorBuildMetadata`. Generator metadata validation is not
generator source retention or a complete acquisition graph: graph-loading-only
module inputs and cold native replay still remain under #343.

The explicit capture now also retains the checkpoint's selected generator Go
sources. The pinned `x/mod/sumdb.Client` authenticates ZIP and separate go.mod
checksums against Go 1.26.10's fixed official verifier key using only this build's
owned signed lookup, tile and latest-tree records. Remote reads are denied;
latest-tree updates remain in memory. The authenticated ZIP line must match the
original native build metadata. Complete original ZIP and go.mod bytes are then
independently checked with Go's module ZIP and content-sum implementations.
The bounded proof set is selected by the maintained client's actual reads, not
by copying the entire checksum cache. Admission happens before allocating each
proof read. Literal `.info` bytes retain only a version equality check; their
timestamp and Origin are not checksum-authenticated facts or execution authority.

The closed capsule retains the checkpoint, originals and exact required proofs.
Externally sealed readback repeats authentication and payload checks; CI verifies
a copied capsule and joins it to `sourceSnapshot.generatorGoSources` and the
original checkpoint. Missing/changed proofs, altered payloads, alternate signing
keys, extra members and contradictory receipts fail. Failed capture leaves a
private unsealed reservation and emits no success receipt. This selected component
does not enumerate graph-loading-only Go metadata or prove full cold native
compilation. Both overall proof flags stay false. Native Linux validation must
establish this new path before its source evidence is used for release admission.

The selected Go input restoration authenticates the service, generator and graph
metadata capsules before reserving a fresh output. It joins identities, refuses
literal conflicts and reconstructs only documented native download/checksum paths.
It never copies an extracted module tree, `.ziphash` or compiled cache. The fixed
catalogue, service locks and official checksum key cannot be replaced by CLI data.
CI reads these inputs with the frozen Go 1.26.10 client in a network namespace as
the original non-root user, with fresh caches and remote module acquisition off.
It compares all selected service/generator checksums, checks unchanged source locks
and requires a missing signed proof to fail in a separately reconstructed cache.
This is native input readback, not generation or compilation from those inputs.
The two whole-build proof flags remain false; issue #343 retains full cold replay.

The same explicit capture also requires
`--bsr-source-verifier /absolute/built-command`, built separately with Go 1.27.3.
Before native API generation, the builder creates a private Buf cache. It retains
the actual API 1.67.0 and console 1.72.0 native commands, checks their original and
copied byte hashes throughout, and binds those identities to the sealed schema
package. Literal registry metadata and separate complete exports cover only the
three immutable references in the frozen v1 lock. Native cache content, exports
and the prepared-source/recipe identities must pass the schema verifier.

Each compiler then compiles the local proto workspace from the restored sealed
source archive, with separate connected and network-denied caches. Linux network
namespaces run as the original non-root UID/GID, with privilege recovery disabled
and the kernel barrier checked before every invocation. An independent reachable
control, failed empty-cache compilation and refused missing/changed cache members
must all pass. Connected and offline descriptor bytes must match. The completed
receipt embeds `sourceSnapshot.bsrSources`, `bsrVerifier` and `bsrSchemaReplay`;
CI verifies an actual copied package and equality with the separate replay record.
Diagnostics retain compact manifests, hashes and results, excluding source payloads
and native executables. This is schema-only replay, not offline service compilation.

Issue #343 also requires actual selected dependency capture and a clean,
network-denied native replay before its full acceptance criteria are complete.
Source/image publication and the recipient's source offer remain #308.

### Original console generator downloads

The explicit `scripts/zitadel-console-assets.go` collector retains the original
Linux amd64 downloads selected by the frozen console install script: grpc-web
1.5.0, the protobuf-javascript 4.0.2 release ZIP and OpenAPI 2.22.0. Its hardcoded
URLs and SHA-256 identities match that immutable source. This console OpenAPI
version is separate from the earlier API generator's 2.28.0 input; their native
ordering remains unchanged.

```sh
go run scripts/zitadel-console-assets.go capture \
  --capsule /absolute/private-parent/new-console-capsule --curl /absolute/native/curl
go run scripts/zitadel-console-assets.go verify \
  --capsule /absolute/private-parent/new-console-capsule --sha256 <manifest-sha256>
go run scripts/zitadel-console-assets.go fetch \
  --capsule /absolute/private-parent/new-console-capsule --sha256 <manifest-sha256> \
  --url <one-exact-frozen-url>
```

No arguments are inert. Capture requires a new destination beneath an existing
0700 physical parent, checks all three downloads against independent frozen
checksums and returns a separate manifest seal. Download sizes and duration are
bounded. Failed captures remain private diagnostics with no returned success seal;
they are never removed by pathname, so a replacement destination is preserved.
Verify and fetch require the external seal and reject changed, missing, extra,
linked, wrong-mode or differently identified members before emitting bytes. Fetch
has no downloader and cannot fall back to the network. These operations never
execute or extract the retained downloads.

Explicit source capture acquires this capsule before the console phase. Only that
phase receives a strict `curl` adapter and a fresh build-owned temporary directory.
The unchanged upstream installer must make exactly its three ordered
`curl -fsSL URL -o destination` requests. The adapter serves verified original
bytes, refuses other flags/URLs, existing or escaped destinations and incomplete
request records, and never invokes a downloader. The upstream checksum, ZIP
extraction and installation steps remain active. API OpenAPI 2.28.0 is verified
before console installation; installed grpc-web and OpenAPI 2.22.0 must match their
retained originals afterward.

An explicit prepared rebuild accepts the complete `--console-inputs <capsule>
--console-sha256 <manifest-sha256>` pair. It verifies that capsule and enters the
same adapter/console path without reacquiring those downloads. A partial pair,
combination with original-source capture or failed verification refuses the build.
The capsule is rechecked after console generation; successful ordered request
records, source manifest, helper source, compiled adapter, launcher and immutable installer
identities are bound to the completed build receipt. The helper source is retained
as a literal source/build record. The native pnpm adapter and both SDK manifests
are three of the fifty-eight literal records. The text-font entrypoint, its
companion, frozen catalogue and Apache text are four further records; the
original Lato OFL is already retained in the complete prepared source.

### Selected pnpm originals and native cache reconstruction

Explicit source capture now invokes the pinned native SDK in
`tools/zitadel-pnpm-source/` after the actual filtered installation and compilation.
Its observed four workspace roots, current/wanted locks and actual optional skips
must agree with the frozen Linux/x64/glibc graph. The installation's complete
workspace-wide skip metadata is checked separately, without acquiring sources
from unselected roots. An early read-only metadata check runs before generation;
final capture repeats the joins after compilation. Peer instances remain separate;
original archives are deduplicated only by complete SRI. The collector checks
original compressed bytes, freshly interpreted native file maps, lifecycle
classification and every connected CAFS payload, then seals a closed private
package and rechecks all members. The selected archive count is an observation,
not a predicted acceptance number.

CI separately acquires the checksum-locked SDK under frozen Node 22.23.3, tests
its adverse controls, and restores the actual copied capsule into a fresh native
store. The unchanged verified index bytes accompany independently verified
reconstructed payloads. Only the compact manifest and source-joined restoration
receipt are uploaded; originals, old installation metadata and cached payloads
are not diagnostics. Read the SDK README for exact explicit commands and limits.

This does not capture the remaining graph-loading-only Go inputs or complete
network-denied native compilation. Both `dependencyInputsCaptured` and
`nativeOfflineReplayProved` remain false. CI checks actual copied originals and
native consumption receipts, but uploads only compact manifests and diagnostics;
the original binaries and archives remain excluded. Full replay remains #343.

## Complete native retained-input replay

The explicit replay command assembles this build's prepared source, selected
service and generator Go originals, authenticated graph metadata, pnpm originals,
schema originals, console originals and the fixed source-verifier bootstrap,
plus their original build receipt. It
selects those fixed components rather than archiving the build directory. Prior
compiled output, installed workspaces, side-effect caches and ambient stores are
excluded. The same closed archive engine uses a distinct `retained-build-inputs`
profile, preserving private/read-only capsule modes. That transport profile
cannot declare prepared build records or enter prepared-source restoration.
External manifest/archive seals bind the actual pack/unpack round trip.

```sh
bash scripts/replay-zitadel-inputs.sh \
  --build /physical/owned/original-build \
  --module-source-verifier /physical/owned/zitadel-module-sources \
  --bsr-source-verifier /physical/owned/zitadel-bsr-sources \
  --verifier-go /physical/original-go1.27.3/bin/go \
  --output /physical/owned/new-offline-replay
```

The existing builder's exclusive `--retained-inputs`/`--retained-sha256` mode
reuses prepared-source and recipe admission. The original receipt binds the
separately supplied verifier executables and the compiler digest observed before
their original builds; command/compiler metadata and byte digests must agree.
Original executables authenticate bootstrap data, while the service replay uses
only reconstructed executables. Maintained native verifiers authenticate every
capsule and its source/recipe joins before reconstruction. Go receives original
ZIP/mod/info and signed checksum records, never extracted trees or trusted ZIP
hash markers. pnpm receives original archives and verified native indexes, never
an installed workspace. Schema restoration receives the checked original cache.
Console installation still consumes the retained three original downloads.

The outer command checks an independently reachable network control, creates a
new network namespace and verifies different namespace identity, the original
nonroot UID/GID, no new privileges and zero effective/bounding capabilities.
It also observes the network control fail there. The complete existing native
pipeline first restores the exact approved tool-source layout and checksum-pinned
x/mod ZIP/mod/info into fresh private caches. The supplied original Go 1.27.3
compiler rebuilds the two fixed commands with identical flags. Their successful
native package compositions and executable bytes must equal the original build.
A separate empty-cache control must refuse acquisition. The reconstructed module
verifier rechecks the capsule; only these rebuilt binaries then enter service
replay. Source, bootstrap, compiler and binary identities are rechecked at the
final boundary, with a separate reconstruction receipt.
The complete existing native
install, generation, console, service compilation and notice pipeline then runs
inside that namespace with fresh owned compiler/acquisition stores and empty
ambient configuration. pnpm explicitly uses offline installation, Go remote
proxy/VCS acquisition is disabled and its public checksum verification remains
enabled. There is no fallback to the network or an old compiled cache.

Pinned Go installs still query module deprecation metadata. The restored native
file-only proxy supplies exact verified ZIP/mod/info bytes and deterministic
lists of complete generator-source versions with native checksum evidence,
separately from writable acquisition caches. Graph-only metadata is retained for
exact graph reads and is not advertised as an install candidate. Go keeps
its native deprecation/retraction ordering and checksum checks. The resolver scope
is the retained snapshot, not authenticated original/current upstream latest;
no `@latest` response or timestamp-based authority is synthesized.

Before the existing `go mod verify`, native Go consumes the exact selected
service modules against the prepared project's unchanged frozen checksums. The
complete returned identities/checksums must match the retained selection; Go
creates its own derived cache markers. No `.ziphash` is copied or synthesized.
This project-checksum read is separate from generator reads outside the project,
which require their retained signed-log evidence.

Both notice phases explicitly select the reconstructed `go-input-native/go-modules`
cache with `--retained-go-cache`; ordinary builds retain their `go-modules` layout.
Neither mode accepts an arbitrary cache root or falls back to the other layout.
Module/package paths, checksums, complete terms and origin inputs remain checked.
The retained marker enters the final build receipt before either notice inventory
binds its bytes; completion only renames that unchanged receipt.

Go 1.26.10, Node 22.23.3, pnpm 10.30.3, the host's normal shell/system utilities,
the caller-selected original Go 1.27.3 distribution and lock-pinned native
pnpm SDK remain explicit build-tool boundaries, not application payloads inside
the input archive. This does not claim a hermetic OS/toolchain distribution.
The builder still reads this checkout's maintained notice collectors/catalogues.
The [identity build-tool scope](../third_party/zitadel-build-tools/README.md) grants
AGPL-3.0-only for its exact authored file list and terms, without licensing the
remaining application. Check it with `bash scripts/check-zitadel-build-tools-license.sh`.
The approved tool source and x/mod originals support this verifier reconstruction.
Other custom-tool inputs, preferred source and bootstrap-dependency treatment
remain #387; a grant does not supply missing source inputs. Durable source-offer
assembly and distribution admission remain #308.

Only after successful full native compilation, complete same-build notice
inventories, source/recipe/toolchain joins and final unchanged-input readback does
the outer command emit `native-retained-input-replay-v1`, with both whole-build
proof flags true. Inner component receipts retain their narrower false flags.
Failures retain private diagnosis and no replay success receipt. CI retains only
compact manifests, receipts and logs, excluding the full archive, caches and
service executable. Neither a fixture nor an assembled archive proves native
success. Reproducible binary equality, image admission, complete license/source
offering, deployed resource measurements and portal activation remain separate.

## Local restricted image CLI test

The explicit image test packages the actual successful Linux build and observes
its version command inside a restricted container. Run it on native Linux amd64
with Go 1.26.10 and a Docker engine that reports its built-in seccomp profile:

```sh
go run scripts/zitadel-image-package.go scripts/zitadel-image-smoke.go scripts/zitadel-service-smoke.go scripts/zitadel-browser.go \
  --smoke --build "$work/linux-build" --repository "$PWD" \
  --output "$work/local-image-test"
```

The output must be new and outside both input trees. A parent-directory alias
does not permit output inside them. No arguments perform no work. Ordinary
website builds and publishers do not invoke this entrypoint. The selected Linux
CI job invokes the stronger `--browser-smoke` profile after its actual native
build. It retains the existing image/service diagnostics, separate browser
receipt and binding, and three fictional-fixture screenshots alongside the
existing build evidence. The CLI-only and HTTP-only profiles remain explicit
alternatives; neither reports browser acceptance.

The notice collector's read-only `--verify` mode repeats the actual completed
build capture, compares every retained diagnostic and final inventory, and
emits the unchanged inventory only when every check passes. It requires the
final build receipt; pending or ambiguous evidence is refused. The image test
uses that mode, the existing login-icon verifier, read-only text-font verification
and read-only copied-asset verification before packaging.

The closed context contains the checked executable, unchanged build receipt,
five notice inventories, seven complete retained notice outputs and three
retained login-icon materials. Its manifest binds all seventeen payload files,
sizes, modes and digests. A dedicated Dockerfile uses the existing immutable
distroless static base and UID/GID 65532. The actual image payload is copied back
from the owned stopped container and compared with that sealed context; matching
version prose alone does not prove matching bytes.

The command inspects image and container configuration, requires network none,
a read-only root, no mounts or published ports, all capabilities dropped,
no privilege escalation and built-in seccomp support. The CLI test is bounded
to 32 PIDs, 256 MiB, one CPU and a 30-second execution deadline. These are test
limits, not measured production sizing. It requires the exact frozen version,
zero exit, an exited container and no OOM/dead state. Only the generated unique
container and local image are removed by their inspected, owner-bound immutable
identities. No tag or container name is overwritten or reclaimed. Cleanup or receipt-write failure
cannot produce a success diagnostic.

The diagnostic records the local image identity, context and recipe digests,
payload file bindings, configured user, inspected restrictions, exact output,
exit and cleanup. `cli-version-smoke` does not establish a daemon's effective
process credentials or memory footprint, service setup/start, PostgreSQL/TLS,
mail, browser login, MFA, recovery or revocation. Process-double controls are
separate from the mandatory actual Docker execution in hosted Linux CI.

The context, executable, image and prepared source are not uploaded or pushed
by this test. No customer credentials or configuration are supplied.
`licenseClosure` and `imageAdmission` remain `unknown`, and `reproducible` stays
false. Remaining terms and corresponding source remain #326/#308; complete
restricted services and actual invitation/login acceptance remain #305/#302.
The portal and identity publication remain off.

## Isolated service process test

Replacing `--smoke` with `--service-smoke` retains the complete CLI/package
checks and cold-starts that same immutable local image against a digest-pinned
PostgreSQL 17 fixture. Both containers use fictional, disposable state. The
network is internal and neither container publishes a host port. The native Linux
probe connects directly to the inspected service's private IPv4 endpoint on that
owned network. It retains `fixture.localhost:8080` as the HTTP/OIDC authority without
DNS resolution; other authorities, ports and redirect destinations are refused.
Docker can retain a requested host mapping without publishing it on an internal-only
network ([upstream report](https://github.com/moby/moby/discussions/53256)); the probe
does not rely on that mapping or add an externally connected network.
HTTP and a pre-verified fictional administrator
are deliberate local fixture choices, not the maintained production identity
policy. No real credentials, mail, invitations, accounts or remote services enter
this test. It does not change the dormant configuration preparer.

The service has a read-only root, UID/GID 65532, dropped capabilities,
no-new-privileges and built-in seccomp. Its one explicitly configured application
mount is a 16 MiB non-executable `/tmp` tmpfs. Docker's ordinary managed kernel
mounts remain separate; this is not a complete writable-filesystem audit.
The fixture limits it to 128 PIDs, 1 GiB and two CPUs;
these ceilings are not measured production sizing. The test requires actual
readiness, issuer-bound OIDC discovery and embedded Console HTTP delivery, then
reads three actual native Linux process samples. Those samples must show the
single service process, all four UID/GID slots, empty capability sets,
no-new-privileges, seccomp filter mode, RSS, peak RSS and thread count. Each sample
also reads that process's actual Linux mount table: the root must be read-only,
and `/tmp` must be a 16 MiB tmpfs owned by 65532:65532, mode 0700, with noexec,
nosuid and nodev. Missing or ambiguous entries, a hidden child mount or failed
kernel reads refuse the result. Docker's legacy `--tmpfs` can appear only in
`HostConfig.Tmpfs`, rather than the regular `Mounts` list; either representation
retains this mandatory independent kernel check. Raw mount tables stay private
and are not retained in the diagnostic. These are
container-process measurements, not build RSS or whole-stack consumption.

The diagnostic binds the inspected image, upstream revision and harness source.
It records startup time, process measurements and cleanup without raw container
configuration, environment values, database contents, authentication tokens or
service logs. The test requires normal service shutdown and removes only its
verified owner-bound service, database and network; the outer image test removes
its own image. A failure is never converted to a successful receipt.

`browserProof` and `authenticationProof` stay false. Console HTTP delivery does
not prove fallback font appearance, a successful browser login, invitation/MFA/
recovery, Dex, verified TLS, backups or production resource sizing. Those remain
under #308/#305/#302, alongside independent image/source publication and admission.

## Native browser presentation

`--browser-smoke` adds a real Chrome/WebDriver journey to the same restricted
service fixture. The hosted Ubuntu runner provides Chrome and its matching
driver; the receipt records the actual browser version. No browser automation
package is installed into the identity distribution. The explicit Go worker
uses WebDriver's native form controls and screenshots, not a projected HTML
fixture or modified upstream page.

The browser and driver run as the invoking non-root user, with no-new-privileges,
inside the inspected identity service's internal network namespace. Only the
network namespace changes. Native Chrome sandbox protections remain enabled;
no certificate-warning or sandbox bypass is used. The `fixture.localhost` authority
maps to that namespace's loopback interface. The browser must independently report
a secure context and available WebCrypto. The
[standard local-origin exception](https://developer.mozilla.org/en-US/docs/Web/Security/Defenses/Secure_Contexts#potentially_trustworthy_origins)
supplies this development-only property without pretending that HTTP is verified
production TLS. No host port or external network is added.

The journey starts at Console, observes the actual Login V1 username form,
submits the fictional fixture operator through the native password form, then
requires the authenticated Console header and navigation. Each page must finish
rendering with its selected font face actually loaded, no failed font faces or
Ailerons resources, and resources confined to the fictional origin. Three
screenshots and their byte hashes make the presentation independently inspectable.
Console capture additionally waits for its native navigation animations to settle
and a full-sized navigation link to be visible, within the viewport and unobstructed
at its center. Mere authenticated component presence cannot admit a clipped first frame.
If the native form offers optional MFA enrollment, this presentation-only fixture
may use its explicit deferral button once. The unchanged native form and its exact
same-origin enrollment action must be observed first. Mandatory enrollment is
never deferred, and the optional action is never submitted twice. The harness
waits for the native transition and still requires the authenticated Console
within the worker deadline. The receipt records that choice and keeps `mfaProof` false.
Production MFA requirements are unchanged. An incomplete journey may retain only
fixed stage/error labels, counts and observation booleans, never DOM text, URLs or
form values. Requests have a 30-second budget covering the 20-second navigation
limit; readiness polls use one second. The complete worker remains bounded to
100 seconds.
Driver session closure, process exit and private profile cleanup are mandatory.
The outer Go harness rechecks the runner source, screenshot bytes and receipt,
then binds the browser result to the inspected local image and upstream source.

This is English Login V1/Console presentation with an ephemeral fictional
administrator. It does not prove portal federation, invitation delivery, MFA,
recovery, production TLS, durable storage or full-stack sizing. The separate
HTTP/process receipt keeps `browserProof` and `authenticationProof` false;
the browser receipt carries only its own observed journey. Image admission,
complete notice closure, durable corresponding source and signed image/source
publication remain required before runtime distribution and activation.

The explicit restricted browser fixture also requires ten neutral-provider presentation
observations: light/dark picker (nine entries), configured table (two fictional
providers), Google editor, Login and registration. Both providers are created only
in the disposable fixture through the authenticated operator’s same-origin native
API. The fixture preserves all seventeen writable login-policy values except
enabling external providers, verifies the two links, and never follows federation
actions or enables local registration. The browser retains no bearer, auth-request
URL, response body or real account data in its receipt. Native Tab reaches both
Login/registration links; Enter activates the Google chooser route. Registration
navigation preserves the existing native authentication request's exact
`authRequestID` parameter rather than opening an unbound options page. Every case
requires loaded glyphs, neutral geometry, labels, native targets, theme and confined
resources, with ten bound PNGs in addition to the original three pages. These
settings and editor observations require their own rendered native Console frame;
they do not reuse the home screen's navigation-bar requirement. The original
operator home observation retains that requirement. After native theme-button
interaction, the worker scrolls the native viewport to the observed controls and
requires their glyphs and links to fit inside it; it injects no DOM or styles.
These checks cover two actual table variants and one actual editor; the other variants
remain bound by their exact transformed source hashes. This remains presentation
evidence, not completed federation, invitations, MFA or portal activation.
