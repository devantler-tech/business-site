# Prepare identity service source

The explicit offline preparer produces the frozen ZITADEL 4.20.0 source without
the unadmitted Ailerons font assets. It does not fetch, compile, publish an image,
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
  two imports removed. Every unrelated source file is preserved, including the
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

The test uses the real frozen public source and exercises default-off behavior,
unchanged input, complete transformed output, existing destinations, missing
source and a plausible partial archive followed by failure. An optional local
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
