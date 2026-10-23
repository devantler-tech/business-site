#!/usr/bin/env bash
# The full build must actually gate CI and must not distribute an unadmitted image.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
yq -o=json '.' "$root/.github/workflows/ci.yaml" >"$scratch/ci.json"
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
    any($job.steps[]; .name == "Prepare the frozen identity JavaScript toolchain" and
      (.run | contains("df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de")) and
      (.run | contains("corepack prepare pnpm@10.30.3 --activate"))) and
    any($job.steps[]; .name == "Fetch the frozen public upstream input" and
      (.run | contains("8afa5dd37a8e723efb9eace8d09fd5c1cccef694"))) and
    all($job.steps[] | select(.uses // "" | startswith("actions/upload-artifact@"));
      (.with.path | split("\n") | map(select(length > 0)) |
        all(test("/zitadel-build/(build-receipt\\.json|packages\\.jsonl|\\*\\.(txt|log|sha256))$")))) and
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
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses // "" | startswith("actions/upload-artifact@")) then .with.path=("$"+"{{ runner.temp }}/zitadel-build/service") else . end)' \
  '.jobs.validation.needs |= map(select(. != "build-zitadel-linux"))' \
  '.jobs.status.steps[0].with["job-results"]=("$"+"{{ needs.test-zitadel-source.result }}")'; do
  jq "$mutation" "$scratch/ci.json" >"$scratch/mutation.json"
  if check "$scratch/mutation.json"; then echo "CI admitted broken Linux build control: $mutation" >&2; exit 1; fi
done
echo 'ZITADEL build CI contract passed, including six broken-wiring controls.'
