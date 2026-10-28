#!/usr/bin/env bash
# The full build must actually gate CI and must not distribute an unadmitted image.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
yq -o=json '.' "$root/.github/workflows/ci.yaml" >"$scratch/ci.json"
# Exercise the actual workflow command with a scanner that produces plausible
# stdout before its stderr diagnostic and failure. Both streams must survive.
scanner_run="$(jq -er '[.jobs["build-zitadel-linux"].steps[] | select(.name == "Scan the actual frozen identity executable") | .run] | select(length == 1) | .[0]' "$scratch/ci.json")"
mkdir -p "$scratch/scanner/identity-analysis" "$scratch/scanner/zitadel-build/service"
cat >"$scratch/scanner/identity-analysis/govulncheck" <<'SCANNER'
#!/usr/bin/env bash
set -eu
test "$#" -eq 3
test "$1" = -mode=binary
test "$2" = -db=https://vuln.go.dev
test "$3" = "$RUNNER_TEMP/zitadel-build/service/zitadel"
printf 'plausible scan report\n'
printf 'fixture scanner diagnostic\n' >&2
exit "$SCANNER_FIXTURE_STATUS"
SCANNER
chmod +x "$scratch/scanner/identity-analysis/govulncheck"
printf 'plausible scan report\nfixture scanner diagnostic\n' >"$scratch/scanner-expected.txt"
for scanner_status in 42 3 0; do
  observed_status=0
  RUNNER_TEMP="$scratch/scanner" SCANNER_FIXTURE_STATUS="$scanner_status" \
    bash -e -o pipefail -c "$scanner_run" >"$scratch/scanner-step.stdout" 2>"$scratch/scanner-step.stderr" || observed_status=$?
  test "$observed_status" -eq "$scanner_status"
  if ! cmp -s "$scratch/scanner-expected.txt" "$scratch/scanner/zitadel-build/vulnerability-analysis.txt"; then
    echo 'scanner diagnostic missing from retained analysis' >&2
    exit 1
  fi
  test ! -s "$scratch/scanner-step.stdout"
  test ! -s "$scratch/scanner-step.stderr"
done
# Validate one complete workflow document, including mandatory diagnostic retention.
check() {
  jq -e '
    .jobs["build-zitadel-linux"] as $job |
    .jobs["test-zitadel-source"] as $source |
    $source.if == null and $source["continue-on-error"] == null and
    any($source.steps[]; (.uses // "" | startswith("actions/setup-go@")) and
      .with["go-version"] == "1.26.10" and .with.cache == false and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($source.steps[]; .run == "bash scripts/prepare-zitadel-source.test.sh" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("third_party/zitadel-console-notices/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("third_party/zitadel-text-fonts/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("third_party/zitadel-copied-assets/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("third_party/zitadel-build-tools/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("server/cmd/zitadel-module-sources/**")) and (.with.filters | contains("server/go.mod")) and (.with.filters | contains("server/go.sum"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("server/cmd/zitadel-bsr-sources/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("tools/zitadel-pnpm-source/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("third_party/zitadel-go-module-notices/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("scripts/testdata/go1.26.10/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("scripts/testdata/zitadel-source/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("scripts/testdata/zitadel-metadata-profile/**"))) and
    any(.jobs.changes.steps[]; .id == "filter" and (.with.filters | contains("Dockerfile.zitadel"))) and
    $job["runs-on"] == "ubuntu-24.04" and
    $job.permissions == {contents:"read"} and
    $job["continue-on-error"] == null and
    $job.needs == "changes" and
    $job.if == "needs.changes.outputs.zitadel-build == '\''true'\''" and
    any($job.steps[]; .name == "Build the pinned identity vulnerability checker" and
      .run == "GOBIN=\"$RUNNER_TEMP/identity-analysis\" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Scan the actual frozen identity executable" and
      .run == "\"$RUNNER_TEMP/identity-analysis/govulncheck\" -mode=binary -db=https://vuln.go.dev \"$RUNNER_TEMP/zitadel-build/service/zitadel\" > \"$RUNNER_TEMP/zitadel-build/vulnerability-analysis.txt\" 2>&1" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Setup the source verifier compiler") < index("Build the pinned identity vulnerability checker") and
      index("Build the pinned identity vulnerability checker") < index("Setup the frozen identity Go compiler") and
      index("Build the complete native Linux service and embedded console") < index("Scan the actual frozen identity executable")) and
    ([$job.steps[] | select(.name == "Verify the exact identity build-tool license scope")] | length) == 1 and
    any($job.steps[]; .name == "Verify the exact identity build-tool license scope" and
      .run == "bash scripts/zitadel-build-tools-license.test.sh" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify the exact identity build-tool license scope") < index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Build the complete native Linux service and embedded console" and
      .run == "bash scripts/build-zitadel-linux.sh --build --capture-source --module-source-verifier \"$RUNNER_TEMP/zitadel-module-sources\" --bsr-source-verifier \"$RUNNER_TEMP/zitadel-bsr-sources\" --source \"$RUNNER_TEMP/zitadel-upstream\" --output \"$RUNNER_TEMP/zitadel-build\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Read back restored Go inputs without networking" and
      .run == "bash scripts/readback-zitadel-go-inputs.sh --build \"$RUNNER_TEMP/zitadel-build\" --module-source-verifier \"$RUNNER_TEMP/zitadel-module-sources\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Replay the complete retained identity build without networking" and
      .run == "bash scripts/replay-zitadel-inputs.sh --build \"$RUNNER_TEMP/zitadel-build\" --module-source-verifier \"$RUNNER_TEMP/zitadel-module-sources\" --bsr-source-verifier \"$RUNNER_TEMP/zitadel-bsr-sources\" --output \"$RUNNER_TEMP/zitadel-offline-replay\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; (.uses // "" | startswith("actions/setup-go@")) and .with["go-version"] == "1.26.10" and .with.cache == false) and
    any($job.steps[]; .name == "Verify the bounded login icon inventory" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-login-icons.go zitadel-text-fonts.go zitadel-copied-assets.go zitadel-login-icons_test.go zitadel-text-fonts_test.go zitadel-copied-assets_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify the bounded console notice capture" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-console-notices.go zitadel-console-notices_test.go zitadel-console-supplement_test.go zitadel-toolchain-notices_test.go zitadel-source-notices_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify local identity image packaging and execution controls" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-image-package.go zitadel-image-smoke.go zitadel-service-smoke.go zitadel-browser.go zitadel-image-package_test.go zitadel-image-smoke_test.go zitadel-service-smoke_test.go zitadel-browser_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify sealed source snapshots and closed archive controls" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-source-bundle.go zitadel-source-bundle_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify original console generator archives" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-console-assets.go zitadel-console-assets_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify native pnpm source retention" and
      .run == "npm ci --prefix tools/zitadel-pnpm-source --ignore-scripts\nnode --test tools/zitadel-pnpm-source/index.test.mjs\n" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify the actual native API generator checkpoint" and
      (.run | contains(".sourceSnapshot.generatorBuildMetadata.checkpointSha256")) and
      (.run | contains("cp \"$RUNNER_TEMP/zitadel-build/generator-checkpoint.json\" \"$RUNNER_TEMP/zitadel-generator-checkpoint.json\"")) and
      (.run | contains("verify-generator-checkpoint --checkpoint \"$RUNNER_TEMP/zitadel-generator-checkpoint.json\" --sha256 \"$seal\" --source-sha256 \"$source\"")) and
      (.run | contains(". == $build[0].sourceSnapshot.generatorBuildMetadata")) and
      (.run | contains(".tools == 14")) and (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify the actual native API generator checkpoint") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Copy and authenticate the native Go graph metadata" and
      (.run | contains("cp -R \"$RUNNER_TEMP/zitadel-build/go-graph-metadata\"")) and
      (.run | contains("verify-graph-metadata --package")) and
      (.run | contains(". == $build[0].sourceSnapshot.goGraphMetadata")) and
      (.run | contains(".goGraphMetadataCaptured == true")) and
      (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Copy and authenticate the actual generator Go source inputs" and
      (.run | contains(".sourceSnapshot.generatorGoSources.manifestSha256")) and
      (.run | contains("cp -R \"$RUNNER_TEMP/zitadel-build/selected-generator-go-sources\"")) and
      (.run | contains("verify-generator-sources --package \"$RUNNER_TEMP/zitadel-generator-sources-roundtrip\" --sha256 \"$seal\" --source-sha256 \"$source\"")) and
      (.run | contains(". == $build[0].sourceSnapshot.generatorGoSources")) and
      (.run | contains(".selectedGeneratorGoSourcesCaptured == true")) and
      (.run | contains(".checkpointSha256 == $build[0].sourceSnapshot.generatorBuildMetadata.checkpointSha256")) and
      (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Copy and authenticate the actual generator Go source inputs") > index("Build the complete native Linux service and embedded console")) and
    ([$job.steps[].name] | index("Prepare the frozen identity JavaScript toolchain") < index("Verify native pnpm source retention") and
      index("Verify native pnpm source retention") < index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Verify and restore the actual selected pnpm sources" and
      (.run | contains(".sourceSnapshot.pnpmSources.manifestSha256")) and
      (.run | contains("cp -R")) and
      (.run | contains("node tools/zitadel-pnpm-source/index.mjs restore --capsule")) and
      (.run | contains("del(.nativeStoreRestored) == $build[0].sourceSnapshot.pnpmSources")) and
      (.run | contains(".sourceManifestSha256 == $build[0].sourceSnapshot.manifestSha256")) and
      (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify and restore the actual selected pnpm sources") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Verify selected Go source retention" and
      .["working-directory"] == "server" and
      .run == "go test -race ./cmd/zitadel-module-sources\nCGO_ENABLED=0 go build -trimpath -buildvcs=false -o \"$RUNNER_TEMP/zitadel-module-sources\" ./cmd/zitadel-module-sources\n" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify selected schema source retention" and
      .["working-directory"] == "server" and
      .run == "go test -race ./cmd/zitadel-bsr-sources\nCGO_ENABLED=0 go build -trimpath -buildvcs=false -o \"$RUNNER_TEMP/zitadel-bsr-sources\" ./cmd/zitadel-bsr-sources\n" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Setup the source verifier compiler" and
      (.uses // "" | startswith("actions/setup-go@")) and
      .with["go-version-file"] == "server/go.mod" and .with.cache == false and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Setup the source verifier compiler") < index("Verify selected Go source retention") and
      index("Verify selected Go source retention") < index("Verify selected schema source retention") and
      index("Verify selected schema source retention") < index("Setup the frozen identity Go compiler")) and
    any($job.steps[]; .name == "Verify actual retained schema inputs and replay" and
      (.run | contains(".sourceSnapshot.bsrSources.manifestSha256")) and
      (.run | contains("cp -R")) and
      (.run | contains("\"$RUNNER_TEMP/zitadel-bsr-sources\" verify --package")) and
      (.run | contains(".schemaOfflineReplayProved == true")) and
      (.run | contains(". == $build[0].sourceSnapshot.bsrSchemaReplay")) and
      (.run | contains(".noNewPrivilegesReplay == true")) and
      (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify actual retained schema inputs and replay") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Copy and verify the actual retained Go module inputs" and
      (.run | contains(".sourceSnapshot.goModules.manifestSha256")) and
      (.run | contains("cp -R")) and
      (.run | contains("\"$RUNNER_TEMP/zitadel-module-sources\" --verify")) and
      (.run | contains("cmp")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify selected Go source retention") < index("Build the complete native Linux service and embedded console")) and
    ([$job.steps[].name] | index("Copy and verify the actual retained Go module inputs") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Unpack and verify the actual pre-install source archive" and
      (.run | contains("go run scripts/zitadel-source-bundle.go --unpack")) and
      (.run | contains(".sourceSnapshot.manifestSha256")) and
      (.run | contains(".sourceSnapshot.archiveSha256")) and
      (.run | contains("cmp \"$RUNNER_TEMP/zitadel-build/source-snapshot/source-manifest.json\" \"$RUNNER_TEMP/zitadel-source-roundtrip/source-manifest.json\"")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify sealed source snapshots and closed archive controls") < index("Build the complete native Linux service and embedded console")) and
    ([$job.steps[].name] | index("Unpack and verify the actual pre-install source archive") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Rebuild the native service from the retained prepared source" and
      (.run | contains("bash scripts/build-zitadel-linux.sh --build --prepared-source \"$RUNNER_TEMP/zitadel-source-roundtrip\" --prepared-sha256 \"$manifest\" --console-inputs \"$RUNNER_TEMP/zitadel-build/selected-console-originals\" --console-sha256 \"$console\" --output \"$RUNNER_TEMP/zitadel-prepared-build\"")) and
      (.run | contains(".sourceInput.recipeSha256 == .recipeSha256")) and
      (.run | contains(".sourceInput.manifestSha256 == $original[0].sourceSnapshot.manifestSha256")) and
      (.run | contains(".sourceInput.nativeOfflineReplayProved == false")) and
      (.run | contains("cp \"$RUNNER_TEMP/zitadel-prepared-build/build-receipt.json\" \"$RUNNER_TEMP/zitadel-build/prepared-build-receipt.json\"")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Rebuild the native service from the retained prepared source") > index("Unpack and verify the actual pre-install source archive")) and
    any($job.steps[]; .name == "Verify the original console downloads and native consumption" and
      (.run | contains("cp -R")) and
      (.run | contains("go run scripts/zitadel-console-assets.go verify --capsule")) and
      (.run | contains("go run scripts/zitadel-console-assets.go consumption --capsule")) and
      (.run | contains(".requests == 3")) and
      (.run | contains(".originalConsoleDownloadsConsumed == true")) and
      (.run | contains(".nativeOfflineReplayProved == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Exercise the local restricted identity image" and
      .run == "command -v google-chrome\ncommand -v chromedriver\ngo run scripts/zitadel-image-package.go scripts/zitadel-image-smoke.go scripts/zitadel-service-smoke.go scripts/zitadel-browser.go --browser-smoke --build \"$RUNNER_TEMP/zitadel-build\" --repository \"$GITHUB_WORKSPACE\" --output \"$RUNNER_TEMP/zitadel-image\"\n" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Exercise the local restricted identity image") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Verify actual identity browser evidence" and
      (.run | contains("sha256sum scripts/zitadel-browser.go")) and
      (.run | contains(".browserReceiptSha256 == $receipt")) and
      (.run | contains(".browserSandboxDisabled == false")) and
      (.run | contains(".mfaProof == false")) and
      (.run | contains(".optionalMFADeferred | type == \"boolean\"")) and
      (.run | contains(".portalFederation == false")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify actual identity service evidence" and
      (.run | contains("sha256sum scripts/zitadel-service-smoke.go")) and
      (.run | contains("--slurpfile image")) and
      (.run | contains("/zitadel-image/service-smoke.json")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify actual identity service evidence") > index("Exercise the local restricted identity image")) and
    any($job.steps[]; .name == "Prepare the frozen identity JavaScript toolchain" and
      (.run | contains("df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de")) and
      (.run | contains("corepack prepare pnpm@10.30.3 --activate"))) and
    any($job.steps[]; .name == "Fetch the frozen public upstream input" and
      (.run | contains("8afa5dd37a8e723efb9eace8d09fd5c1cccef694"))) and
    ([$job.steps[] | select(.uses // "" | startswith("actions/upload-artifact@"))] | length == 1) and
    all($job.steps[] | select(.uses // "" | startswith("actions/upload-artifact@"));
      .if == "always()" and .with["if-no-files-found"] == "error" and
      (.["continue-on-error"] == null or .["continue-on-error"] == false) and
      .with.name == "zitadel-linux-build-evidence" and
      (.with.path | split("\n") | map(select(length > 0)) |
        sort == (["build-receipt.json", "packages.jsonl", "login-icons-inventory.json", "text-font-notices-inventory.json", "copied-asset-notices-inventory.json", "console-notices-inventory.json", "console-notices-supplement-inventory.json", "*.txt", "*.log", "*.sha256", "source-snapshot/source-manifest.json", "source-roundtrip.json", "prepared-build-receipt.json", "selected-go-sources/manifest.json", "go-module-source-roundtrip.json", "generator-checkpoint.json", "generator-checkpoint-roundtrip.json", "selected-generator-go-sources/manifest.json", "generator-source-readback.json", "generator-source-roundtrip.json", "go-graph-metadata/manifest.json", "go-graph-metadata-readback.json", "go-graph-metadata-roundtrip.json", "go-input-restoration.json", "go-input-readback.json", "go-input-native/manifest.json", "selected-pnpm-sources/manifest.json", "pnpm-source-roundtrip.json", "pnpm-installation-check.json", "selected-bsr-sources/manifest.json", "bsr-source-roundtrip.json", "bsr-schema-replay.json", "selected-console-originals/manifest.json", "console-download-consumption.json", "console-originals-roundtrip.json", "console-consumption-roundtrip.json"] |
          map("$" + "{{ runner.temp }}/zitadel-build/" + .) + (["image-smoke.json","service-smoke.json","browser-smoke.json","browser-binding.json","browser-incomplete.json","login.png","password.png","console.png"] | map("$" + "{{ runner.temp }}/zitadel-image/" + .)) + (["replay-receipt.json","input-retention.json","input-archive.json","input-roundtrip.json","roundtrip/source-manifest.json","native-replay.log","native/build-receipt.json","native/retained-go-restoration.json","native/retained-go-service-readback.json","native/retained-bsr-restoration.json","native/retained-pnpm-restoration.json","native/console-notices-inventory.json","native/console-notices-supplement-inventory.json","native/login-icons-inventory.json","native/text-font-notices-inventory.json", "native/copied-asset-notices-inventory.json","native/build-info.txt"] | map("$" + "{{ runner.temp }}/zitadel-offline-replay/" + .)) | sort))) and
    (.jobs.validation.needs | index("build-zitadel-linux") != null) and
    (.jobs.status.needs | index("build-zitadel-linux") != null) and
    any(.jobs.validation.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result"))) and
    any(.jobs.status.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result")))
  ' "$1" >/dev/null
}
check "$scratch/ci.json"
# Compile the actual admitted CI source-file list. An incomplete build must reach
# our command's refusal, not fail earlier because a source path is wrong. The
# empty fixture is rejected before Docker on the native platform too.
image_sources=()
jq -r '.jobs["build-zitadel-linux"].steps[] | select(.name == "Exercise the local restricted identity image") | .run | split("\n")[] | select(startswith("go run ")) | split(" ") as $a | $a[2:($a | index("--browser-smoke"))][]' "$scratch/ci.json" > "$scratch/image-sources.txt"
while IFS= read -r file; do image_sources+=("$file"); done < "$scratch/image-sources.txt"
mkdir "$scratch/build" "$scratch/repository"
if (cd "$root" && GOTOOLCHAIN=local go run "${image_sources[@]}" --browser-smoke --build "$scratch/build" --repository "$scratch/repository" --output "$scratch/image") > "$scratch/entrypoint.log" 2>&1; then
  echo "empty native service input was accepted" >&2; exit 1
fi
if ! grep -q '^identity-image:' "$scratch/entrypoint.log"; then
  echo "actual CI service command failed before its input admission" >&2; exit 1
fi

# Execute the mandatory consumer's predicate against independent receipt data.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Verify actual identity service evidence") | .run | split("--slurpfile image")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" > "$scratch/service.jq"
jq -n '{schemaVersion:1,profile:"cli-version-smoke",upstreamCommit:"8afa5dd37a8e723efb9eace8d09fd5c1cccef694",localImageId:("sha256:"+("a"*64))}' > "$scratch/service-image.json"
jq -n '{schemaVersion:1,profile:"isolated-service-smoke",upstreamCommit:"8afa5dd37a8e723efb9eace8d09fd5c1cccef694",localImageId:("sha256:"+("a"*64)),serviceSourceSha256:("c"*64),databaseImage:"postgres:17.11-bookworm@sha256:3645570cccdfa447589da9f57dd740faa29b30938e861289a5574b6ca6b03826",readyMilliseconds:12000,processSamples:([range(0;3)|{pid:1234,name:"zitadel",uid:65532,gid:65532,rssKiB:47104,peakRssKiB:65536,threads:12,effectiveCapabilities:"0000000000000000",boundingCapabilities:"0000000000000000",noNewPrivileges:true,seccompMode:2,mounts:{rootReadOnly:true,tmpfsBytes:16777216,uid:65532,gid:65532,mode:"0700",noExec:true,noSuid:true,noDev:true}}]),readOnlyRoot:true,configuredTmpfsPaths:["/tmp"],network:"internal-two-container-direct",transport:"fictional-http-only",oidcDiscovery:true,embeddedConsoleHTTP:true,browserProof:false,authenticationProof:false,cleanup:"complete",imageAdmission:"unknown",licenseClosure:"unknown"}' > "$scratch/service-positive.json"
check_service() {
  jq -e --arg source "$(printf 'c%.0s' {1..64})" --slurpfile image "$scratch/service-image.json" -f "$scratch/service.jq" "$1" > /dev/null
}
check_service "$scratch/service-positive.json"
for mutation in '.localImageId=("sha256:"+("b"*64))' '.upstreamCommit=("b"*40)' '.serviceSourceSha256=("b"*64)' \
  '.processSamples=[]' '.processSamples[0].pid=4321' '.processSamples[0].uid=0' '.processSamples[0].gid=0' \
  '.processSamples[0].rssKiB="47104"' '.processSamples[0].peakRssKiB=0' '.processSamples[0].noNewPrivileges=false' \
  '.processSamples[0].effectiveCapabilities="0000000000000001"' '.processSamples[0].seccompMode=0' \
  '.readOnlyRoot=false' '.configuredTmpfsPaths+=["/app"]' '.network="host"' '.oidcDiscovery=false' \
  '.processSamples[0].mounts.rootReadOnly=false' '.processSamples[0].mounts.tmpfsBytes=33554432' '.processSamples[0].mounts.uid=0' '.processSamples[0].mounts.noExec=false' '.processSamples[0].mounts=null' \
  '.cleanup="pending"' '.browserProof=true' '.authenticationProof=true' '.imageAdmission="complete"'; do
  jq "$mutation" "$scratch/service-positive.json" > "$scratch/service-negative.json"
  if check_service "$scratch/service-negative.json"; then echo "invalid service evidence admitted: $mutation" >&2; exit 1; fi
done
# Exercise the outer replay's actual service-consumption predicate. A component
# readback must remain source-bound and must not promote itself to whole replay.
sed -n '/^jq -e --slurpfile original.*--slurpfile restored /,/retained-go-service-readback.json/p' \
  "$root/scripts/replay-zitadel-inputs.sh" | sed '1,2d;$d' >"$scratch/native-service.jq"
test -s "$scratch/native-service.jq"
jq -n '{sourceSnapshot:{manifestSha256:("a"*64)}}' >"$scratch/native-service-original.json"
jq -n '{manifestSha256:("b"*64)}' >"$scratch/native-service-restored.json"
jq -n '{modules:[{effective:{Path:"one",Version:"v1"}},{effective:{Path:"two",Version:"v1"}},{effective:{Path:"one",Version:"v1"}}]}' >"$scratch/native-service-selection.json"
jq -n '{schemaVersion:1,sourceManifestSha256:("a"*64),restorationManifestSha256:("b"*64),serviceModules:2,nativeCacheReadbackPassed:true,sourceChecksumsUnchanged:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}' >"$scratch/native-service-positive.json"
check_native_service() {
  jq -e --slurpfile original "$scratch/native-service-original.json" \
    --slurpfile restored "$scratch/native-service-restored.json" \
    --slurpfile service "$scratch/native-service-selection.json" \
    -f "$scratch/native-service.jq" "$1" >/dev/null
}
check_native_service "$scratch/native-service-positive.json"
for mutation in '.schemaVersion=2' '.sourceManifestSha256=("c"*64)' '.restorationManifestSha256=("c"*64)' \
  '.serviceModules=1' '.serviceModules=0' '.nativeCacheReadbackPassed=false' '.sourceChecksumsUnchanged=false' \
  '.dependencyInputsCaptured=true' '.nativeOfflineReplayProved=true'; do
  jq "$mutation" "$scratch/native-service-positive.json" >"$scratch/native-service-negative.json"
  if check_native_service "$scratch/native-service-negative.json"; then
    echo "invalid native service component admitted: $mutation" >&2; exit 1
  fi
done
# Execute the native prepared-rebuild receipt predicate, rather than accepting
# textual mentions of a joined seal or the deliberately false offline claim.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Rebuild the native service from the retained prepared source") | .run |
  split("jq -e --slurpfile original")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" >"$scratch/prepared.jq"
jq -n '{upstreamCommit:("d"*40),tools:{go:"1.26.10",node:"22.23.3",pnpm:"10.30.3"},platform:"linux/amd64",profile:"api-login-v1-console",preparation:{schemaVersion:1},recipeSha256:("c"*64),sourceSnapshot:{manifestSha256:("a"*64),preparedProfileSha256:("b"*64)},consoleDownloads:{sourceManifestSha256:("a"*64),manifestSha256:("d"*64),requestsSha256:("e"*64),helperSourceSha256:("f"*64),installerSha256:("1"*64),adapterSha256:("2"*64),launcherSha256:("3"*64),requests:3,originalConsoleDownloadsConsumed:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}}' >"$scratch/original-build.json"
jq '.sourceInput = (.sourceSnapshot + {recipeSha256:.recipeSha256,nativeOfflineReplayProved:false}) | del(.sourceSnapshot) | .imageAdmission="unknown" | .licenseClosure="unknown"' "$scratch/original-build.json" >"$scratch/prepared-build.json"
jq -e --slurpfile original "$scratch/original-build.json" -f "$scratch/prepared.jq" "$scratch/prepared-build.json" >/dev/null
for mutation in \
  '.sourceInput.manifestSha256=("e"*64)' \
  '.sourceInput.preparedProfileSha256=("e"*64)' \
  '.sourceInput.recipeSha256=("e"*64)' \
  '.recipeSha256=("e"*64) | .sourceInput.recipeSha256=.recipeSha256' \
  '.tools.node="other"' \
  '.upstreamCommit=("e"*40)' \
  '.preparation.schemaVersion=2' \
  '.sourceInput.nativeOfflineReplayProved=true' \
  '.consoleDownloads.manifestSha256=("e"*64)' \
  '.consoleDownloads.requestsSha256=("d"*64)' \
  '.consoleDownloads.helperSourceSha256=("e"*64)' \
  '.consoleDownloads.launcherSha256=("e"*64)' \
  '.consoleDownloads.installerSha256=("e"*64)' \
  '.consoleDownloads.sourceManifestSha256=("e"*64)' \
  '.consoleDownloads.requests=2' \
  '.consoleDownloads.originalConsoleDownloadsConsumed=false' \
  '.consoleDownloads.nativeOfflineReplayProved=true' \
  '.sourceSnapshot={}' \
  '.imageAdmission="admitted"' \
  '.licenseClosure="complete"'; do
  jq "$mutation" "$scratch/prepared-build.json" >"$scratch/changed-prepared-build.json"
  if jq -e --slurpfile original "$scratch/original-build.json" -f "$scratch/prepared.jq" "$scratch/changed-prepared-build.json" >/dev/null; then
    echo "CI accepted an inconsistent prepared-build receipt: $mutation" >&2; exit 1
  fi
done
# Exercise the actual copied generator-source receipt predicate, including a
# coherently altered embedded row that disagrees with the native checkpoint.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Copy and authenticate the actual generator Go source inputs") | .run | split("jq -e --slurpfile build")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" >"$scratch/generator-sources.jq"
jq -n '{schemaVersion:1,manifestSha256:("c"*64),sourceManifestSha256:("a"*64),checkpointSha256:("b"*64),modules:123,proofFiles:2,files:373,bytes:123,selectedGeneratorGoSourcesCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}' >"$scratch/generator-sources.json"
jq -n --slurpfile receipt "$scratch/generator-sources.json" '{sourceSnapshot:{manifestSha256:("a"*64),generatorBuildMetadata:{checkpointSha256:("b"*64),modules:123},generatorGoSources:$receipt[0]}}' >"$scratch/generator-sources-build.json"
jq -e --slurpfile build "$scratch/generator-sources-build.json" -f "$scratch/generator-sources.jq" "$scratch/generator-sources.json" >/dev/null
for mutation in '.modules=0' '.proofFiles=0' '.files=0' '.sourceManifestSha256=("c"*64)' '.checkpointSha256=("c"*64)' '.selectedGeneratorGoSourcesCaptured=false' '.dependencyInputsCaptured=true' '.nativeOfflineReplayProved=true' 'del(.manifestSha256)'; do
  jq "$mutation" "$scratch/generator-sources.json" >"$scratch/bad-generator-sources.json"
  if jq -e --slurpfile build "$scratch/generator-sources-build.json" -f "$scratch/generator-sources.jq" "$scratch/bad-generator-sources.json" >/dev/null; then
    echo 'CI accepted a contradictory generator-source receipt' >&2; exit 1
  fi
done
jq '.sourceSnapshot.generatorGoSources.modules=122' "$scratch/generator-sources-build.json" >"$scratch/wrong-generator-sources-build.json"
jq '.modules=122' "$scratch/generator-sources.json" >"$scratch/wrong-generator-sources.json"
if jq -e --slurpfile build "$scratch/wrong-generator-sources-build.json" -f "$scratch/generator-sources.jq" "$scratch/wrong-generator-sources.json" >/dev/null; then
  echo 'CI accepted a generator-source set different from its checkpoint' >&2; exit 1
fi
# Exercise the literal CI replay predicate, including a forged embedded claim.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Verify actual retained schema inputs and replay") | .run |
  split("jq -e --arg manifest")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" >"$scratch/replay.jq"
jq -n '{schemaVersion:1,platform:"linux/amd64",sourceManifestSha256:("a"*64),bsrManifestSha256:("b"*64),recipeSha256:("c"*64),schemaOfflineReplayProved:true,networkDenialProved:true,nonRootReplay:true,noNewPrivilegesReplay:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false,compilers:(["api","console"]|map({phase:.,version:(if .=="api" then "1.67.0" else "1.72.0" end),descriptorSha256:("d"*64),connectedMatchesOffline:true,emptyCacheRefused:true,missingMemberRefused:true,changedMemberRefused:true}))}' >"$scratch/replay.json"
jq -n --slurpfile replay "$scratch/replay.json" '{recipeSha256:("c"*64),sourceSnapshot:{manifestSha256:("a"*64),bsrSchemaReplay:$replay[0]}}' >"$scratch/replay-build.json"
jq -e --arg manifest "$(printf '%064d' 0 | tr 0 b)" --slurpfile build "$scratch/replay-build.json" -f "$scratch/replay.jq" "$scratch/replay.json" >/dev/null
jq '.sourceSnapshot.bsrSchemaReplay.schemaOfflineReplayProved=false' "$scratch/replay-build.json" >"$scratch/forged-replay-build.json"
if jq -e --arg manifest "$(printf '%064d' 0 | tr 0 b)" --slurpfile build "$scratch/forged-replay-build.json" -f "$scratch/replay.jq" "$scratch/replay.json" >/dev/null; then
  echo 'CI accepted a replay record different from the embedded build receipt' >&2; exit 1
fi
controls=0
# Execute the actual generator readback predicate on structured contradictory
# records; an embedded claim cannot replace the independently verified copy.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Verify the actual native API generator checkpoint") | .run | split("jq -e --slurpfile build")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" >"$scratch/generators.jq"
jq -n '{schemaVersion:1,sourceManifestSha256:("a"*64),checkpointSha256:("b"*64),tools:14,modules:123,generatorBuildMetadataValidated:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}' >"$scratch/generators.json"
jq -n --slurpfile receipt "$scratch/generators.json" '{sourceSnapshot:{manifestSha256:("a"*64),generatorBuildMetadata:$receipt[0]}}' >"$scratch/generator-build.json"
jq -e --slurpfile build "$scratch/generator-build.json" -f "$scratch/generators.jq" "$scratch/generators.json" >/dev/null
for mutation in '.tools=13' '.modules=0' '.generatorBuildMetadataValidated=false' '.sourceManifestSha256=("c"*64)' '.dependencyInputsCaptured=true' '.nativeOfflineReplayProved=true' 'del(.checkpointSha256)'; do
  jq "$mutation" "$scratch/generators.json" >"$scratch/bad-generators.json"
  if jq -e --slurpfile build "$scratch/generator-build.json" -f "$scratch/generators.jq" "$scratch/bad-generators.json" >/dev/null; then
    echo 'CI accepted a contradictory generator checkpoint receipt' >&2; exit 1
  fi
done
# Execute the pnpm restoration predicate on real structured control records;
# textual mentions alone cannot stand in for the source/receipt correspondence.
jq -er '.jobs["build-zitadel-linux"].steps[] | select(.name == "Verify and restore the actual selected pnpm sources") | .run | split("jq -e --slurpfile build")[1] | split("\u0027")[1] | select(length > 0)' "$scratch/ci.json" >"$scratch/pnpm.jq"
jq -n '{schemaVersion:1,manifestSha256:("a"*64),sourceManifestSha256:("b"*64),packages:1,archives:1,files:14,bytes:123,selectedPNPMSourcesCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}' >"$scratch/pnpm.json"
jq -n --slurpfile pnpm "$scratch/pnpm.json" '{sourceSnapshot:{manifestSha256:("b"*64),pnpmSources:$pnpm[0]}}' >"$scratch/pnpm-build.json"
jq '.nativeStoreRestored=true' "$scratch/pnpm.json" >"$scratch/pnpm-restored.json"
jq -e --slurpfile build "$scratch/pnpm-build.json" -f "$scratch/pnpm.jq" "$scratch/pnpm-restored.json" >/dev/null
for mutation in '.nativeStoreRestored=false' '.sourceManifestSha256=("c"*64)' '.packages=0' '.selectedPNPMSourcesCaptured=false' '.dependencyInputsCaptured=true' '.nativeOfflineReplayProved=true' 'del(.manifestSha256)'; do
  jq "$mutation" "$scratch/pnpm-restored.json" >"$scratch/pnpm-bad.json"
  if jq -e --slurpfile build "$scratch/pnpm-build.json" -f "$scratch/pnpm.jq" "$scratch/pnpm-bad.json" >/dev/null; then
    echo 'CI accepted a contradictory selected pnpm restoration receipt' >&2; exit 1
  fi
done
for mutation in \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Build the pinned identity vulnerability checker"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the pinned identity vulnerability checker" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Scan the actual frozen identity executable"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Scan the actual frozen identity executable" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Scan the actual frozen identity executable" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Scan the actual frozen identity executable" then .run |= sub(" 2>&1$"; "") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Scan the actual frozen identity executable" then .run += " -json" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Scan the actual frozen identity executable" then .run=(.run | sub("service/zitadel"; "service/other")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the exact identity build-tool license scope"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the exact identity build-tool license scope" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the exact identity build-tool license scope" then .["continue-on-error"]=true else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("third_party/zitadel-build-tools/\\\\*\\\\*"; "other-input/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Read back restored Go inputs without networking"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Read back restored Go inputs without networking" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Read back restored Go inputs without networking" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Read back restored Go inputs without networking" then .run=(.run | sub("readback-zitadel-go-inputs"; "missing-reader")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Copy and authenticate the actual generator Go source inputs"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Copy and authenticate the actual generator Go source inputs" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Copy and authenticate the actual generator Go source inputs" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Copy and authenticate the actual generator Go source inputs" then .run=(.run | sub("verify-generator-sources"; "missing-verifier")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters |= sub("tools/zitadel-pnpm-source"; "missing/tool") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify native pnpm source retention"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify and restore the actual selected pnpm sources"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify and restore the actual selected pnpm sources" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify and restore the actual selected pnpm sources" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify and restore the actual selected pnpm sources" then .run=(.run | sub(" restore --capsule"; " verify --capsule")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("selected-pnpm-sources/manifest.json"; "selected-pnpm-sources/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Rebuild the native service from the retained prepared source"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Rebuild the native service from the retained prepared source" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Rebuild the native service from the retained prepared source" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Rebuild the native service from the retained prepared source" then .run=(.run | sub("--prepared-source"; "--source")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Rebuild the native service from the retained prepared source" then .run=(.run | sub("sourceInput.recipeSha256 == .recipeSha256"; "sourceInput.recipeSha256 != .recipeSha256")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("prepared-build-receipt.json") | not)) | join("\n")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters |= sub("server/cmd/zitadel-bsr-sources"; "missing/tool") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify selected schema source retention"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify selected schema source retention" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify selected schema source retention" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .run=(.run | sub("--bsr-source-verifier"; "--missing-verifier")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify actual retained schema inputs and replay"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual retained schema inputs and replay" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual retained schema inputs and replay" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("selected-bsr-sources/manifest.json"; "selected-bsr-sources/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("bsr-schema-replay.json"; "bsr-tools/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters |= sub("server/cmd/zitadel-module-sources"; "missing/tool") else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters |= sub("server/go.mod"; "missing/mod") else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters |= sub("server/go.sum"; "missing/sum") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Setup the source verifier compiler"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Setup the source verifier compiler" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Setup the source verifier compiler" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Setup the source verifier compiler" then .with["go-version-file"]="missing/go.mod" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= sort_by(if .name == "Setup the frozen identity Go compiler" then 0 else 1 end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify selected Go source retention" then .run=(.run | sub("CGO_ENABLED=0 go build"; "echo")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .run=(.run | sub("--module-source-verifier"; "--missing-verifier")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify selected Go source retention"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify selected Go source retention" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify selected Go source retention" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Copy and verify the actual retained Go module inputs"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Copy and verify the actual retained Go module inputs" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Copy and verify the actual retained Go module inputs" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify sealed source snapshots and closed archive controls"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify sealed source snapshots and closed archive controls" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify sealed source snapshots and closed archive controls" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Unpack and verify the actual pre-install source archive"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Unpack and verify the actual pre-install source archive" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Unpack and verify the actual pre-install source archive" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Unpack and verify the actual pre-install source archive" then .run=(.run | sub("--unpack"; "--verify")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .run=(.run | sub(" --capture-source"; "")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("source-snapshot/source-manifest.json"; "source-snapshot/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("Dockerfile.zitadel"; "other-input")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify local identity image packaging and execution controls"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Exercise the local restricted identity image" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Exercise the local restricted identity image" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Exercise the local restricted identity image"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("/zitadel-image/image-smoke.json"; "/zitadel-image/context/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("third_party/zitadel-go-module-notices/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("third_party/zitadel-text-fonts/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("third_party/zitadel-copied-assets/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("scripts/testdata/zitadel-metadata-profile/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs["test-zitadel-source"].steps |= map(select((.uses // "" | startswith("actions/setup-go@")) | not))' \
  '.jobs["test-zitadel-source"].steps |= map(if (.uses // "" | startswith("actions/setup-go@")) then .with["go-version"]="1.27.2" else . end)' \
  '.jobs["test-zitadel-source"].steps |= map(if (.uses // "" | startswith("actions/setup-go@")) then .if="false" else . end)' \
  '.jobs["test-zitadel-source"].steps |= map(if .run == "bash scripts/prepare-zitadel-source.test.sh" then .if="false" else . end)' \
  '.jobs["test-zitadel-source"].steps |= map(if .run == "bash scripts/prepare-zitadel-source.test.sh" then .["continue-on-error"]=true else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("scripts/testdata/zitadel-source/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded console notice capture" then .run=(.run | sub(" zitadel-source-notices_test.go"; "")) else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("scripts/testdata/go1.26.10/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded console notice capture" then .run=(.run | sub(" zitadel-toolchain-notices_test.go"; "")) else . end)' \
  '.jobs["build-zitadel-linux"].permissions.packages="write"' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the actual native API generator checkpoint"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the actual native API generator checkpoint" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the actual native API generator checkpoint" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the actual native API generator checkpoint" then .run=(.run | sub("verify-generator-checkpoint"; "observe-generators")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the original console downloads and native consumption"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the original console downloads and native consumption" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the original console downloads and native consumption" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify original console generator archives"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify original console generator archives" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify original console generator archives" then .["continue-on-error"]=true else . end)' \
  '.jobs.changes.steps |= map(if .id == "filter" then .with.filters=(.with.filters | gsub("third_party/zitadel-console-notices/\\*\\*"; "other-input/**")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("console-notices-supplement-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the bounded login icon inventory"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded login icon inventory" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded login icon inventory" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the bounded console notice capture"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded console notice capture" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded console notice capture" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=("$"+"{{ runner.temp }}/zitadel-build/service") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select((.uses // "" | startswith("actions/upload-artifact@")) | not))' \
  '.jobs["build-zitadel-linux"].steps += [.jobs["build-zitadel-linux"].steps[] | select(.uses // "" | startswith("actions/upload-artifact@"))]' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .if="success()" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with["if-no-files-found"]="ignore" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path="" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("build-receipt.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("/zitadel-build/login-icons-inventory.json"; "/./zitadel-build/build-receipt.json")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("console-notices-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("/zitadel-build/text-font-notices-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("/native/text-font-notices-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("/zitadel-build/copied-asset-notices-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("/native/copied-asset-notices-inventory.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("/zitadel-build/console-notices-inventory.json"; "/zitadel-build/unrelated.json")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.name="other-evidence" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Replay the complete retained identity build without networking"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Replay the complete retained identity build without networking" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Replay the complete retained identity build without networking" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Exercise the local restricted identity image" then .run=(.run | sub("--browser-smoke"; "--service-smoke")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Exercise the local restricted identity image" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Exercise the local restricted identity image" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("service-smoke.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify actual identity service evidence"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual identity service evidence" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual identity service evidence" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify actual identity browser evidence"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual identity browser evidence" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify actual identity browser evidence" then .["continue-on-error"]=true else . end)' \
  '.jobs.validation.needs |= map(select(. != "build-zitadel-linux"))' \
  '.jobs.status.steps[0].with["job-results"]=("$"+"{{ needs.test-zitadel-source.result }}")'; do
  jq "$mutation" "$scratch/ci.json" >"$scratch/mutation.json"
  if check "$scratch/mutation.json"; then echo "CI admitted broken Linux build control: $mutation" >&2; exit 1; fi
  controls=$((controls+1))
done
printf 'ZITADEL build CI contract passed, including %s broken-wiring controls.\n' "$controls"
