#!/usr/bin/env bash
# The full build must actually gate CI and must not distribute an unadmitted image.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
yq -o=json '.' "$root/.github/workflows/ci.yaml" >"$scratch/ci.json"
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
    any($job.steps[]; .name == "Build the complete native Linux service and embedded console" and
      .run == "bash scripts/build-zitadel-linux.sh --build --capture-source --source \"$RUNNER_TEMP/zitadel-upstream\" --output \"$RUNNER_TEMP/zitadel-build\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; (.uses // "" | startswith("actions/setup-go@")) and .with["go-version"] == "1.26.10" and .with.cache == false) and
    any($job.steps[]; .name == "Verify the bounded login icon inventory" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-login-icons.go zitadel-login-icons_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify the bounded console notice capture" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-console-notices.go zitadel-console-notices_test.go zitadel-console-supplement_test.go zitadel-toolchain-notices_test.go zitadel-source-notices_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify local identity image packaging and execution controls" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-image-package.go zitadel-image-smoke.go zitadel-image-package_test.go zitadel-image-smoke_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Verify sealed source snapshots and closed archive controls" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-source-bundle.go zitadel-source-bundle_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; .name == "Unpack and verify the actual pre-install source archive" and
      (.run | contains("go run scripts/zitadel-source-bundle.go --unpack")) and
      (.run | contains(".sourceSnapshot.manifestSha256")) and
      (.run | contains(".sourceSnapshot.archiveSha256")) and
      (.run | contains("cmp \"$RUNNER_TEMP/zitadel-build/source-snapshot/source-manifest.json\" \"$RUNNER_TEMP/zitadel-source-roundtrip/source-manifest.json\"")) and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Verify sealed source snapshots and closed archive controls") < index("Build the complete native Linux service and embedded console")) and
    ([$job.steps[].name] | index("Unpack and verify the actual pre-install source archive") > index("Build the complete native Linux service and embedded console")) and
    any($job.steps[]; .name == "Exercise the local restricted identity image" and
      .run == "go run scripts/zitadel-image-package.go scripts/zitadel-image-smoke.go --smoke --build \"$RUNNER_TEMP/zitadel-build\" --repository \"$GITHUB_WORKSPACE\" --output \"$RUNNER_TEMP/zitadel-image\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    ([$job.steps[].name] | index("Exercise the local restricted identity image") > index("Build the complete native Linux service and embedded console")) and
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
        sort == (["build-receipt.json", "packages.jsonl", "login-icons-inventory.json", "console-notices-inventory.json", "console-notices-supplement-inventory.json", "*.txt", "*.log", "*.sha256", "source-snapshot/source-manifest.json", "source-roundtrip.json"] |
          map("$" + "{{ runner.temp }}/zitadel-build/" + .) + ["$" + "{{ runner.temp }}/zitadel-image/image-smoke.json"] | sort))) and
    (.jobs.validation.needs | index("build-zitadel-linux") != null) and
    (.jobs.status.needs | index("build-zitadel-linux") != null) and
    any(.jobs.validation.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result"))) and
    any(.jobs.status.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result")))
  ' "$1" >/dev/null
}
check "$scratch/ci.json"
controls=0
for mutation in \
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
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("/zitadel-build/console-notices-inventory.json"; "/zitadel-build/unrelated.json")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.name="other-evidence" else . end)' \
  '.jobs.validation.needs |= map(select(. != "build-zitadel-linux"))' \
  '.jobs.status.steps[0].with["job-results"]=("$"+"{{ needs.test-zitadel-source.result }}")'; do
  jq "$mutation" "$scratch/ci.json" >"$scratch/mutation.json"
  if check "$scratch/mutation.json"; then echo "CI admitted broken Linux build control: $mutation" >&2; exit 1; fi
  controls=$((controls+1))
done
printf 'ZITADEL build CI contract passed, including %s broken-wiring controls.\n' "$controls"
