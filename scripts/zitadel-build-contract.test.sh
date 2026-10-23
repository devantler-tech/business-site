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
    $job["runs-on"] == "ubuntu-24.04" and
    $job.permissions == {contents:"read"} and
    $job["continue-on-error"] == null and
    $job.needs == "changes" and
    $job.if == "needs.changes.outputs.zitadel-build == '\''true'\''" and
    any($job.steps[]; .name == "Build the complete native Linux service and embedded console" and
      .run == "bash scripts/build-zitadel-linux.sh --build --source \"$RUNNER_TEMP/zitadel-upstream\" --output \"$RUNNER_TEMP/zitadel-build\"" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    any($job.steps[]; (.uses // "" | startswith("actions/setup-go@")) and .with["go-version"] == "1.26.10" and .with.cache == false) and
    any($job.steps[]; .name == "Verify the bounded login icon inventory" and
      .["working-directory"] == "scripts" and
      .run == "go test -race zitadel-login-icons.go zitadel-login-icons_test.go" and
      .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
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
        sort == (["build-receipt.json", "packages.jsonl", "login-icons-inventory.json", "*.txt", "*.log", "*.sha256"] |
          map("$" + "{{ runner.temp }}/zitadel-build/" + .) | sort))) and
    (.jobs.validation.needs | index("build-zitadel-linux") != null) and
    (.jobs.status.needs | index("build-zitadel-linux") != null) and
    any(.jobs.validation.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result"))) and
    any(.jobs.status.steps[]; (.with["job-results"] // "" | contains("needs.build-zitadel-linux.result")))
  ' "$1" >/dev/null
}
check "$scratch/ci.json"
for mutation in \
  '.jobs["build-zitadel-linux"].permissions.packages="write"' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Build the complete native Linux service and embedded console" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Verify the bounded login icon inventory"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded login icon inventory" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify the bounded login icon inventory" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=("$"+"{{ runner.temp }}/zitadel-build/service") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select((.uses // "" | startswith("actions/upload-artifact@")) | not))' \
  '.jobs["build-zitadel-linux"].steps += [.jobs["build-zitadel-linux"].steps[] | select(.uses // "" | startswith("actions/upload-artifact@"))]' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .if="success()" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with["if-no-files-found"]="ignore" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path="" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | split("\n") | map(select(contains("build-receipt.json") | not)) | join("\n")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=(.with.path | sub("/zitadel-build/login-icons-inventory.json"; "/./zitadel-build/build-receipt.json")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.name="other-evidence" else . end)' \
  '.jobs.validation.needs |= map(select(. != "build-zitadel-linux"))' \
  '.jobs.status.steps[0].with["job-results"]=("$"+"{{ needs.test-zitadel-source.result }}")'; do
  jq "$mutation" "$scratch/ci.json" >"$scratch/mutation.json"
  if check "$scratch/mutation.json"; then echo "CI admitted broken Linux build control: $mutation" >&2; exit 1; fi
done
echo 'ZITADEL build CI contract passed, including eighteen broken-wiring controls.'
