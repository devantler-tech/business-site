# Selected identity-build pnpm inputs

This native pnpm SDK boundary belongs to business-site #343 and its existing
source-replay PR. It is inactive without an explicit operation. It does not
install the application, publish dependency archives or activate the portal.

The maintained entrypoint is `index.mjs`; pnpm exposes these metadata and CAFS
APIs through its native JavaScript packages. The exact SDK dependencies and their
original integrity values are in `package-lock.json`, separately from the frozen
identity application's lock and toolchain.

```sh
npm ci --prefix tools/zitadel-pnpm-source --ignore-scripts
node --test tools/zitadel-pnpm-source/index.test.mjs
```

CLI operations require native Linux amd64, Node 22.23.3 and umask 022. The existing
Linux build invokes capture only for its explicit `--capture-source` mode. It
retains the actual native selected-project output after the unchanged filtered
install. The exact frozen wanted lock, native current lock, modules manifest and
four workspace package manifests enter a private, externally sealed capsule.

The native filter must independently join both locks, peer-qualified package
instances and Linux/x64/glibc optional skips. Only active selected original npm
archives are fetched, from the public npm registry without credentials or
redirects. Complete original SHA-512 is checked before bounded decompression and
native CAFS interpretation. Every native file-map identity, size, mode and hash
must match the connected index and actual cached bytes. Native lifecycle
classification is independently derived with pnpm's `pkgRequiresBuild`; neither
timestamps nor an index's unchecked `requiresBuild` field supplies proof.
The exact native registry metadata also includes pnpm 10.30.3's built-in
`@jsr` scope, independently checked in its frozen configuration source. Its
presence does not admit JSR downloads: selected original archives must still
resolve to the public npm registry. Missing, substituted or extra registry
entries are rejected.

Native hard links into this build's own installation are counted explicitly.
Foreign links, symbolic/special files, nonempty side-effects caches, unselected
indexes/payloads and unexpected directories are rejected. Only pnpm's exact
precreated hexadecimal empty buckets are admitted. Capture finishes with complete
connected and retained readback. A failed operation retains bounded private,
unsealed diagnosis and emits no success receipt; it never recursively deletes a
pathname that might have been replaced.
Failure output contains only a bounded stage and fixed reason/error category;
native exception messages, stacks and source/configuration records are excluded.
An optional-skip mismatch additionally reports bounded identities from the checked
public frozen lock; unknown entries are redacted. pnpm's installation manifest
records workspace-wide optional exclusions, including incompatible dependencies
from unselected roots. That native full-workspace classification is checked
separately from the four-root source graph; it never adds unselected archives to
capture. Missing, invented or compatible-package skips still fail.
File refusals additionally report numeric type/mode/size/link metadata, never
pathnames. Original archive permissions are checked against the complete native
file map. Physical CAFS permissions follow pnpm's distinct storage normalization:
0755 for executable content, otherwise 0644 under the required umask 022.
pnpm's bin linking can subsequently set an installed command to 0755, changing
the same CAFS inode when the import uses hard links. The native `package-bins`
reader derives those command targets from SRI-verified original manifests;
directory declarations use a private filename-only original view. Only those
targets with counted links into this build may have that exact transition.
All original selected manifests are joined before shared CAFS inodes are checked.
Unrelated executable files, foreign links, changed bytes and other modes still
fail. Original archive modes and connected index maps remain byte-for-byte
evidence. Reconstruction starts with normal native storage modes, before a
future offline install performs its own linking.

The explicit read-only `inspect --build /physical/owned/build` operation runs
immediately after installation, before expensive generation. It checks the same
native metadata and frozen lock and rechecks its literal observations, acquiring
no archives and creating no capsule. Its source-capture/offline flags remain
false. Final capture repeats all selection checks after compilation.

```sh
node tools/zitadel-pnpm-source/index.mjs capture \
  --build /physical/owned/build --source-sha256 '<source-seal>' \
  --output /physical/owned/selected-pnpm-sources \
  --scratch /physical/owned/new-capture-work

node tools/zitadel-pnpm-source/index.mjs verify \
  --capsule /physical/owned/selected-pnpm-sources --sha256 '<pnpm-seal>' \
  --scratch /physical/owned/new-verify-work

node tools/zitadel-pnpm-source/index.mjs restore \
  --capsule /physical/owned/selected-pnpm-sources --sha256 '<pnpm-seal>' \
  --scratch /physical/owned/new-restore-work \
  --store /physical/owned/new-native-store
```

Verification reinterprets originals with the same SDK. Restoration writes a new
native CAFS store and byte-identical verified indexes, then independently checks
its complete contents. Old `node_modules`, current-lock or modules files are
evidence only; they are never transplanted into the replayed application.

Source/build records retain this adapter and both SDK manifests. CI tests the
native SDK under the frozen Linux Node version, captures from the actual build,
then verifies and restores a copied capsule. Only its compact manifest and joined
receipt are diagnostic artifacts. Original archives, cached payloads and full
literal observations remain private build inputs.

This is selected pnpm input retention and native store reconstruction. Successful
fixtures are not observed native Linux capture. Store reconstruction is not an
offline application installation or a network-denied full build. Generator-Go
closure and complete cold nonroot replay remain #343. Receipts keep
`dependencyInputsCaptured` and `nativeOfflineReplayProved` false.
