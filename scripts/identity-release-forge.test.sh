#!/usr/bin/env bash
# Execute the actual API admission helper. Archive semantics are separately
# exercised by the real Go carrier/ZIP tests; this reader double grants none.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/bin" "$scratch/runner"
source_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
jq -n --arg source "$source_sha" '{id:123,run_attempt:2,repository:{full_name:"devantler-tech/business-site"},head_repository:{full_name:"devantler-tech/business-site"},head_sha:$source,head_branch:"main",path:".github/workflows/ci.yaml",event:"push",status:"completed",conclusion:"success"}' >"$scratch/run-good.json"
jq -n '{total_count:1,jobs:[{name:"build-zitadel-linux",status:"completed",conclusion:"success",steps:(["Scan the actual frozen identity executable","Verify frozen base signatures for original transport","Bind the original local archives and native witnesses","Read back the original saved candidate without rebuilding","Seal the original publication carrier","Retain the admitted original publication carrier","one","two","three","four","five","six"]|map({name:.,status:"completed",conclusion:"success"}))}]}' >"$scratch/jobs-good.json"
jq -n --arg source "$source_sha" '{id:456,name:"identity-original-123-2",expired:false,digest:("sha256:"+("b"*64)),size_in_bytes:100,workflow_run:{id:123,head_sha:$source,head_branch:"main",repository_id:1409101115,head_repository_id:1409101115}}' >"$scratch/artifact-good.json"
cat >"$scratch/bin/gh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 4 && "$1" == api && "$2" == --hostname && "$3" == github.com ]] || exit 97
printf '%s\n' "$4" >>"$FIXTURE/calls"
case "$4" in
 repos/devantler-tech/business-site/compare/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)
  printf '{"status":"identical","base_commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"merge_base_commit":{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}\n' ;;
 repos/devantler-tech/business-site/commits/main)
  if [[ -f "$FIXTURE/downloaded" && -f "$FIXTURE/main-after.json" ]]; then cat "$FIXTURE/main-after.json";else printf '{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n';fi ;;
 repos/devantler-tech/business-site/actions/runs/123)
  if [[ -f "$FIXTURE/downloaded" && -f "$FIXTURE/run-after.json" ]];then cat "$FIXTURE/run-after.json";else cat "$FIXTURE/run.json";fi ;;
 repos/devantler-tech/business-site/actions/runs/123/attempts/2) cat "$FIXTURE/run.json" ;;
 'repos/devantler-tech/business-site/actions/runs/123/attempts/2/jobs?per_page=100') cat "$FIXTURE/jobs.json" ;;
 'repos/devantler-tech/business-site/actions/runs/123/artifacts?per_page=100')
  jq --argjson count "${ARTIFACT_COUNT:-1}" '. as $a|{total_count:$count,artifacts:[$a]}' "$FIXTURE/artifact.json" ;;
 repos/devantler-tech/business-site/actions/artifacts/456/zip) touch "$FIXTURE/downloaded";printf fixture ;;
 repos/devantler-tech/business-site/actions/artifacts/456)
  if [[ -f "$FIXTURE/artifact-after.json" ]];then cat "$FIXTURE/artifact-after.json";else cat "$FIXTURE/artifact.json";fi ;;
 *) exit 98 ;;
esac
FIXTURE
cat >"$scratch/runner/identity-release" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 13 && "$1" == restore-artifact && "$2" == --input && "$4" == --sha256 && "$6" == --source-sha && "$7" == aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa && "$8" == --run-id && "$9" == 123 && "${10}" == --run-attempt && "${11}" == 2 && "${12}" == --output ]] || exit 96
mkdir -p "${13}/native"
printf '{}\n'
FIXTURE
chmod +x "$scratch/bin/gh" "$scratch/runner/identity-release"
reset_fixture() {
  cp "$scratch/run-good.json" "$scratch/run.json"
  cp "$scratch/jobs-good.json" "$scratch/jobs.json"
  cp "$scratch/artifact-good.json" "$scratch/artifact.json"
  rm -f "$scratch/downloaded" "$scratch/run-after.json" "$scratch/artifact-after.json" "$scratch/main-after.json"
  : >"$scratch/env"
  : >"$scratch/calls"
}
admit() {
  env PATH="$scratch/bin:$PATH" FIXTURE="$scratch" RUNNER_TEMP="$scratch/runner" GITHUB_ENV="$scratch/env" GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REF=refs/heads/main GITHUB_SHA="$source_sha" IDENTITY_CI_RUN_ID=123 "$@" bash "$root/scripts/admit-identity-release.sh" >"$scratch/stdout" 2>"$scratch/stderr"
}
refuse() {
  if admit "$@"; then
    printf 'identity forge: invalid context accepted\n' >&2
    exit 1
  fi
  test ! -s "$scratch/env"
}
reset_fixture
admit || {
  cat "$scratch/stderr"
  exit 1
}
test "$(wc -l <"$scratch/env" | tr -d ' ')" == 1
for context in GITHUB_EVENT_NAME=pull_request GITHUB_REF=refs/heads/other GITHUB_REPOSITORY=other/site GITHUB_SHA=main IDENTITY_CI_RUN_ID=0; do
  reset_fixture
  refuse "$context"
  test ! -s "$scratch/calls"
done
for mutation in '.head_sha="bad"' '.event="workflow_dispatch"' '.head_branch="other"' '.conclusion="failure"' '.status="queued"' '.repository.full_name="other/site"' '.path="other.yaml"'; do
  reset_fixture
  jq "$mutation" "$scratch/run.json" >"$scratch/new"
  mv "$scratch/new" "$scratch/run.json"
  refuse
  test ! -f "$scratch/downloaded"
done
for mutation in '.total_count=2' '.jobs[0].conclusion="skipped"' '.jobs[0].steps[0].conclusion="skipped"' '.jobs[0].steps[1].conclusion="failure"' '.jobs[0].steps|=.[1:]' '.jobs[0].steps[4].conclusion="skipped"'; do
  reset_fixture
  jq "$mutation" "$scratch/jobs.json" >"$scratch/new"
  mv "$scratch/new" "$scratch/jobs.json"
  refuse
  test ! -f "$scratch/downloaded"
done
for mutation in '.expired=true' '.digest=null' '.name="identity-original-123-1"' '.workflow_run.head_sha="bad"' '.workflow_run.id=124' '.size_in_bytes=1350565889'; do
  reset_fixture
  jq "$mutation" "$scratch/artifact.json" >"$scratch/new"
  mv "$scratch/new" "$scratch/artifact.json"
  refuse
  test ! -f "$scratch/downloaded"
done
reset_fixture
refuse ARTIFACT_COUNT=2
reset_fixture
jq '.run_attempt=3' "$scratch/run.json" >"$scratch/run-after.json"
refuse
reset_fixture
jq '.digest="sha256:changed"' "$scratch/artifact.json" >"$scratch/artifact-after.json"
refuse
reset_fixture
printf '{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}\n' >"$scratch/main-after.json"
refuse
printf 'identity forge: PASS — actual bounded API admission and 28 refusal controls\n'
