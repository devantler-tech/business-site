# Selected schema source packages

This explicit Go 1.27.3 build tool retains and checks the frozen identity
profile's three BSR modules. No arguments perform no work. It never acquires
dependencies, executes retained content, starts identity services or activates
the portal. Same-build Linux acquisition and native replay remain [#351](https://github.com/devantler-tech/business-site/issues/351).

The collector checks all 49 independently exported source/documentation files
against both native cache representations. It independently recomputes each
module's complete SHAKE256 digest, including its original v1 configuration and
lock records. The immutable commit, owner, module and creation time must match
the frozen profile and the separate registry response. The older b5 content is
checked byte for byte; no dependency-sensitive b5 digest or signed upstream
attestation is invented. The algorithm follows the pinned Buf
[module digest](https://github.com/bufbuild/buf/blob/v1.72.0/private/bufpkg/bufmodule/digest.go)
and [CAS manifest](https://github.com/bufbuild/buf/blob/v1.72.0/private/pkg/cas/manifest.go)
formats.

Build with the existing server module's compiler:

```sh
GOTOOLCHAIN=local go test -race ./cmd/zitadel-bsr-sources
GOTOOLCHAIN=local go build -trimpath -buildvcs=false -o /absolute/zitadel-bsr-sources ./cmd/zitadel-bsr-sources
```

An explicit acquisition caller must supply one owned input root containing:

| Path | Input |
|---|---|
| `buf-cache/v3/modules/{b5,shake256}/buf.build/<owner>/<module>/<commit>/` | Complete native selected module trees |
| `buf-cache/v3/commits/shake256/buf.build/<commit>.json` | Native commit records |
| `bsr-exports/<owner>/<module>/` | Separate complete exports |
| `bsr-records/<commit>.json` | Literal older registry commit/time responses |
| `bsr-tools/{api,console}` | Actual supplied tool binaries, hashed but never executed or retained |
| `bsr-tools/{api,console}.version` | Caller-observed `1.67.0` / `1.72.0`, each with one newline |
| `source-snapshot/source-manifest.json` | Prepared-source manifest with an external seal |
| `prepared/source/proto/buf.lock` | Exact frozen source lock |

The caller also supplies the literal reviewed recipe. Supplied tool byte hashes
and version observations are retained bindings, not independent executable/version
attestations. The acquisition caller must establish their provenance and actual
execution order. This tool does not prove that an arbitrary input directory came
from a native build.

```sh
zitadel-bsr-sources capture --build /absolute/owned-build --recipe /absolute/build-recipe.sh --source-sha256 SOURCE_MANIFEST_SHA256 --output /absolute/new-package
zitadel-bsr-sources verify --package /absolute/package --sha256 EXTERNAL_PACKAGE_MANIFEST_SHA256
zitadel-bsr-sources restore --package /absolute/package --sha256 EXTERNAL_PACKAGE_MANIFEST_SHA256 --output /absolute/new-native-cache
zitadel-bsr-sources check-cache --package /absolute/package --sha256 EXTERNAL_PACKAGE_MANIFEST_SHA256 --cache /absolute/native-cache
```

The sealed package contains 113 native cache files, 49 exported files, three
acquisition responses and five build/tool records (170 members plus its manifest).
Every retained file is private with no execute permissions; directories are private.
Membership, complete bytes, metadata and full modes are checked. Linked parents,
linked/special/multiply linked files, ambiguous JSON, unsafe names, partial I/O
and oversized inputs fail. Payload reads are bounded to 8 MiB per file and 64 MiB
in total before allocation; inventories are capped at 512 members. Supplied tool
binary reads have their separate 128 MiB bound. Output directories must be new.
Failures emit no success receipt; an owned incomplete output may remain for
diagnosis. Callers must not treat that directory's existence as success.

Restore copies the verified in-memory snapshot and checks the complete native
destination. It never rereads unverified source bytes for copying. The explicit
cache check must precede a native consumer; native Buf can subsequently create
transient locks, so do not execute it against the sealed package or treat a
mutated cache as the original verified input.

`selectedBSRSourcesCaptured` describes this selected source set only.
`dependencyInputsCaptured` and `nativeOfflineReplayProved` remain false. The
package does not close pnpm/tool dependencies, prove an unchanged native offline
build, clear licensing/distribution, authorize an image, or grant customer access.
