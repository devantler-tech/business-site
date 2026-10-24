# Selected identity Go source inputs

This Go 1.26 build tool is inactive without arguments. It uses the frozen
231-module service catalogue and `golang.org/x/mod` v0.42.0, pinned through
`go.mod` and `go.sum`. This separate build-tool dependency is not a service
dependency. Ordinary website builds never run it.

```sh
go test -race ./...
go vet ./...
go run . --capture --build /absolute/completed-build \
  --catalogue /absolute/repository/third_party/zitadel-go-module-notices/catalogue.json \
  --output /absolute/fresh-retained-directory
go run . --verify /absolute/fresh-retained-directory --sha256 EXTERNAL_MANIFEST_SHA256
```

The existing explicit Linux builder invokes these operations after compilation
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

Build and output directories are explicit, private and exclusively owned by the
invocation. No concurrent writer may rename or replace them during capture or
readback. Neither input metadata nor an archive can select commands to execute.
Failed capture removes only its newly reserved output; preexisting output is
never replaced. CI verifies a fresh copy of the real package and uploads only
compact manifest/round-trip diagnostics, not the retained sources.

This is service-Go source retention. Generator-tool graphs, filtered pnpm inputs,
BSR schema sources and clean network-denied full compilation remain #343. Full
source/image admission and public retrieval remain #308. Daemon resources, deployed
login and invited-customer acceptance remain #305/#302. No activation is implied.
