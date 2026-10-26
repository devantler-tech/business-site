# Selected identity Go source inputs

This command in the existing Go 1.27.3 server module is inactive without arguments. It uses the frozen
231-module service catalogue and `golang.org/x/mod` v0.42.0, pinned through
the server's `go.mod` and `go.sum`. This build-tool dependency is not an identity service
dependency. Ordinary website builds never run it.

```sh
# From server/, with the exact repository compiler:
GOTOOLCHAIN=local go test -race ./cmd/zitadel-module-sources
GOTOOLCHAIN=local go vet ./cmd/zitadel-module-sources
GOTOOLCHAIN=local go build -trimpath -buildvcs=false \
  -o /absolute/zitadel-module-sources ./cmd/zitadel-module-sources
/absolute/zitadel-module-sources --capture --build /absolute/completed-build \
  --catalogue /absolute/repository/third_party/zitadel-go-module-notices/catalogue.json \
  --output /absolute/fresh-retained-directory
/absolute/zitadel-module-sources --verify /absolute/fresh-retained-directory --sha256 EXTERNAL_MANIFEST_SHA256
/absolute/zitadel-module-sources capture-generator-sources \
  --build /absolute/completed-build --checkpoint-sha256 CHECKPOINT_SEAL \
  --source-sha256 SOURCE_SEAL --output /absolute/fresh-generator-sources
/absolute/zitadel-module-sources verify-generator-sources \
  --package /absolute/fresh-generator-sources --sha256 GENERATOR_SOURCES_SEAL \
  --source-sha256 SOURCE_SEAL
/absolute/zitadel-module-sources capture-graph-metadata \
  --build /absolute/completed-build --source-sha256 SOURCE_SEAL \
  --output /absolute/fresh-graph-metadata
/absolute/zitadel-module-sources verify-graph-metadata \
  --package /absolute/fresh-graph-metadata --sha256 GRAPH_METADATA_SEAL \
  --source-sha256 SOURCE_SEAL
/absolute/zitadel-module-sources restore-go-inputs \
  --service /absolute/selected-go-sources --service-sha256 SERVICE_SEAL \
  --generators /absolute/selected-generator-go-sources --generators-sha256 GENERATOR_SEAL \
  --metadata /absolute/go-graph-metadata --metadata-sha256 GRAPH_METADATA_SEAL \
  --source-sha256 SOURCE_SEAL --output /absolute/fresh-native-inputs
```

The explicit Linux builder takes `--module-source-verifier /absolute/zitadel-module-sources`
alongside `--capture-source`. It requires the reviewed command identity, exact
tool compiler and an unchanged executable digest. CI builds that command before
switching to the identity service's unchanged Go 1.26.10 compiler; the native
build never switches or auto-downloads a toolchain. The builder invokes capture
and verification after compilation
and its final module verification. The complete compiler stream, root command
dependency closure, reviewed catalogue and frozen locks must agree. Original and
effective replacement identities are retained. Only the build-owned selected ZIPs
and separate go.mod files are read; `.ziphash` is never integrity evidence here.

The module library checks the [Go ZIP format](https://pkg.go.dev/golang.org/x/mod/zip)
and recomputes complete [Go content hashes](https://pkg.go.dev/golang.org/x/mod/sumdb/dirhash).
The verifier additionally binds literal-file hashes, counts, sizes, modes and
closed membership to an externally supplied manifest digest. It refuses linked
internal parents, extra or missing members, unknown/duplicate manifest fields,
partial I/O and inconsistent selection. Canonical file permissions are applied
to fresh owned descriptors, including under a restrictive umask.

Decoded fields cannot appear under conflicting case variants in compiler
observations, nested module/replacement records or retained manifests. Arbitrary
map keys keep their native case-sensitive meaning; additional unconsumed compiler
fields remain supported. An incomplete/error-bearing observation cannot become
complete by overwriting one decoded field through a second spelling.

Build and output directories are explicit, private and exclusively owned by the
invocation. No concurrent writer may rename or replace them during capture or
readback. Neither input metadata nor an archive can select commands to execute.
Failed capture removes only its newly reserved output; preexisting output is
never replaced. CI verifies a fresh copy of the real package and uploads only
compact manifest/round-trip diagnostics, not the retained sources.

The separate `observe-generators --build /absolute/build --source-sha256 SOURCE_SEAL`
operation reads all fourteen actual API generator executables after generation and
before the console installs its different OpenAPI binary. It validates the frozen
Go locks and API install recipe, exact tool commands/main versions, Go 1.26.10,
Linux amd64 and disabled CGO. It hashes each confined executable before and after
reading native Go build information. The checkpoint retains every original
main/dependency/replacement/settings record, including gci and the nested gRPC
generator module. Its effective module union keys on both path and version;
independent tool graphs may legitimately select different versions.

`verify-generator-checkpoint --checkpoint /absolute/checkpoint.json --sha256 CHECKPOINT_SEAL
--source-sha256 SOURCE_SEAL` checks a fresh copy against the caller's external seals
and recomputes that complete union. CI joins the compact receipt to the original
build receipt. This validates build metadata; it does **not** retain generator
module archives, authenticate a binary signature, or enumerate graph-loading-only
inputs. `dependencyInputsCaptured` and `nativeOfflineReplayProved` stay false.

The explicit generator-source operations retain the checkpoint's selected Go
module ZIP, go.mod and literal info bytes, plus the required checksum proofs.
The maintained checksum client uses Go 1.26.10's fixed official `sum.golang.org`
verifier key; the CLI and retained data cannot select another key. It reads only
this build's owned lookup, tile and signed latest-tree records, with remote reads
denied and latest-tree updates confined to memory. Each ZIP checksum must also
match the original native build metadata. Complete ZIP/go.mod content validation
and externally sealed copied-package readback repeat these checks. Literal info
metadata supplies version equality, not authenticated timestamps or origins.

The graph-metadata operations retain observed `.mod` and optional `.info` files
from the same build's fresh download cache, including resolver-only versions
without adding their source ZIPs. They authenticate complete go.mod content
against the frozen service checksum file or the fixed official checksum verifier,
with only the needed same-build signed-log evidence. Literal info bytes must name
the matching version; their timestamps and origins are not authenticated authority.
The portable capsule has closed membership, bounded reads and an external seal.
It contains no compiled cache, `.ziphash` marker or unrelated global cache.

`restore-go-inputs` authenticates all three capsules before reserving a fresh
private output. It joins compiled identities to observed metadata and refuses
conflicting literal files. Only selected ZIP/mod/info and signed checksum records
are mapped into explicit native Go cache paths; no compiled cache, extracted
module tree or `.ziphash` is trusted. The CLI cannot override the frozen catalogue,
service checksums or official checksum key. Failed restoration clears its receipt
and removes only its new output, preserving existing paths and input capsules.

It also creates a separate sealed `go-proxy` from the exact verified ZIP/mod/info
bytes. Its deterministic version lists enumerate only retained identities with
validated info and authenticated module definitions. Native Go performs its own
version ordering, deprecation/retraction and checksum checks against this finite
file-only snapshot. This is not proof of the original or current upstream latest
resolver result. No `@latest` response or timestamp-based selection is invented.
The portable receipt identifies `moduleProxyScope: retained-snapshot`; it contains
no host-specific URL. The native consumer encodes its own fixed `go-proxy` path
with no HTTP/direct fallback; writable native caches stay separate. Identical
inputs must retain byte-identical receipts across different scratch locations.

CI runs `scripts/readback-zitadel-go-inputs.sh` with Go 1.26.10 inside a network
namespace, dropping root privileges before native module reads. It compares every
service/generator checksum, preserves the source locks and requires a fresh
missing-proof read to fail. Only compact manifests and reader results are uploaded.
This validates native cache reads; it does not run generation or compilation from
those caches. Complete dependency capture and cold native replay remain issue #343,
and both overall proof flags stay false. Companion commands handle pnpm, BSR and console
inputs; their capsules do not establish complete dependency closure. Full
source/image admission and public retrieval remain #308. Daemon resources, deployed
login and invited-customer acceptance remain #305/#302. No activation is implied.
