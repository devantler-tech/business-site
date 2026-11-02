#!/usr/bin/env bash
# Authenticate one complete original main CI before reading its closed carrier.
set -euo pipefail
export LC_ALL=C
readonly repo=devantler-tech/business-site
fail() {
  printf 'identity release: refused %s\n' "$1" >&2
  exit 1
}
[[ "${GITHUB_REPOSITORY:-}" == "$repo" && "${GITHUB_EVENT_NAME:-}" == workflow_dispatch && "${GITHUB_REF:-}" == refs/heads/main && "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'manual main source'
[[ "${IDENTITY_CI_RUN_ID:-}" =~ ^[1-9][0-9]{0,14}$ && -d "${RUNNER_TEMP:-}" && -x "$RUNNER_TEMP/identity-release" && -n "${GITHUB_ENV:-}" ]] || fail 'original run and trusted reader'
readonly caller=$GITHUB_SHA run=$IDENTITY_CI_RUN_ID
[[ "$(df -Pk "$RUNNER_TEMP" | awk 'NR==2 {print $4}')" -ge 8388608 ]] || fail 'original carrier verification needs 8 GiB disposable disk headroom'
work="$(mktemp -d "$RUNNER_TEMP/identity-forge.XXXXXX")"
complete=false
trap 'if [[ "$complete" != true ]]; then rm -rf "$work"; fi' EXIT
read_json() {
  (
    ulimit -f 8192
    gh api --hostname github.com "repos/$repo/$1" >"$2"
  ) 2>/dev/null || fail 'bounded API read'
  jq -se 'length==1 and (.[0]|type)=="object"' "$2" >/dev/null || fail 'API document'
}
read_json 'commits/main' "$work/main-before.json"
jq -e --arg source "$caller" '.sha==$source' "$work/main-before.json" >/dev/null || fail 'caller is no longer current main'
read_json "actions/runs/$run" "$work/run-before.json"
source="$(jq -er '.head_sha|select(test("^[0-9a-f]{40}$"))' "$work/run-before.json")" || fail 'original source identity'
attempt="$(jq -er '.run_attempt|tostring' "$work/run-before.json")"
[[ "$attempt" =~ ^[1-9][0-9]{0,5}$ ]] || fail 'current original attempt'
check_run() {
  jq -e --arg source "$source" --arg repo "$repo" --argjson run "$run" --argjson attempt "$attempt" '
 .id==$run and .run_attempt==$attempt and .repository.full_name==$repo and .head_repository.full_name==$repo and
 .head_sha==$source and .head_branch=="main" and .path==".github/workflows/ci.yaml" and
 .event=="push" and .status=="completed" and .conclusion=="success"' "$1" >/dev/null || fail 'successful original main CI'
}
check_run "$work/run-before.json"
read_json "compare/$source...$caller" "$work/ancestry.json"
jq -e --arg source "$source" '(.status=="ahead" or .status=="identical") and .base_commit.sha==$source and .merge_base_commit.sha==$source' "$work/ancestry.json" >/dev/null || fail 'original source is not an ancestor of current caller main'
read_json "actions/runs/$run/attempts/$attempt" "$work/attempt.json"
check_run "$work/attempt.json"
read_json "actions/runs/$run/attempts/$attempt/jobs?per_page=100" "$work/jobs.json"
# Bind required steps explicitly; a successful job cannot stand in for a skipped producer.
jq -e '
 . as $all | .total_count==(.jobs|length) and .total_count>0 and .total_count<=100 and
 ([.jobs[]|select(.name=="build-zitadel-linux")]|length)==1 and
 ([.jobs[]|select(.name=="build-zitadel-linux")][0] as $job |
 $job.status=="completed" and $job.conclusion=="success" and ($job.steps|length)>10 and
 all($job.steps[];.status=="completed" and (.conclusion=="success" or .conclusion=="skipped")) and
 all(["Scan the actual frozen identity executable","Verify frozen base signatures for original transport","Bind the original local archives and native witnesses","Read back the original saved candidate without rebuilding","Seal the original publication carrier","Retain the admitted original publication carrier"][];
 . as $name | [$job.steps[]|select(.name==$name)] as $steps | ($steps|length)==1 and $steps[0].conclusion=="success"))
' "$work/jobs.json" >/dev/null || fail 'complete original native producer'
read_json "actions/runs/$run/artifacts?per_page=100" "$work/artifacts.json"
jq -e '.total_count==(.artifacts|length) and .total_count>0 and .total_count<=100' "$work/artifacts.json" >/dev/null || fail 'complete artifact inventory'
jq -ce --arg source "$source" --arg name "identity-original-$run-$attempt" --argjson run "$run" '
 [.artifacts[]|select(.name==$name)]|select(length==1)|.[0]|select(.expired==false and
 (.id|type)=="number" and (.id|tostring|test("^[1-9][0-9]{0,14}$")) and
 (.digest|type)=="string" and (.digest|test("^sha256:[0-9a-f]{64}$")) and
 (.size_in_bytes|type)=="number" and .size_in_bytes>0 and .size_in_bytes<=1350565888 and
 .workflow_run.id==$run and .workflow_run.head_sha==$source and .workflow_run.head_branch=="main" and
 .workflow_run.repository_id==1409101115 and .workflow_run.head_repository_id==1409101115)
' "$work/artifacts.json" >"$work/artifact-before.json" || fail 'unique original carrier identity'
artifact="$(jq -er .id "$work/artifact-before.json")"
digest="$(jq -er '.digest|ltrimstr("sha256:")' "$work/artifact-before.json")"
(
  ulimit -f 1318912
  gh api --hostname github.com "repos/$repo/actions/artifacts/$artifact/zip" >"$work/artifact.zip"
) 2>/dev/null || fail 'bounded original carrier download'
chmod 444 "$work/artifact.zip"
"$RUNNER_TEMP/identity-release" restore-artifact --input "$work/artifact.zip" --sha256 "$digest" --source-sha "$source" --run-id "$run" --run-attempt "$attempt" --output "$work/restored" >"$work/transport.json" || fail 'digest-bound original carrier restoration'
read_json "actions/runs/$run" "$work/run-after.json"
check_run "$work/run-after.json"
read_json "actions/artifacts/$artifact" "$work/artifact-after.json"
jq -e --slurpfile before "$work/artifact-before.json" '.expired==false and [.id,.name,.digest,.size_in_bytes,.workflow_run]==[$before[0].id,$before[0].name,$before[0].digest,$before[0].size_in_bytes,$before[0].workflow_run]' "$work/artifact-after.json" >/dev/null || fail 'carrier changed during admission'
read_json 'commits/main' "$work/main-after.json"
jq -e --arg source "$caller" '.sha==$source' "$work/main-after.json" >/dev/null || fail 'main moved during original admission'
jq -n --arg source "$source" --arg caller "$caller" --arg run "$run" --argjson attempt "$attempt" --arg artifact "$artifact" --arg digest "$digest" '{schemaVersion:1,sourceSha:$source,callerSha:$caller,ciRunId:$run,ciRunAttempt:$attempt,artifactId:$artifact,artifactSha256:$digest,forgeAdmission:true,sourceOfferPublished:false,imagePublished:false,portalActivated:false}' >"$work/forge.json"
printf 'IDENTITY_RELEASE_WORK=%s\n' "$work" >>"$GITHUB_ENV"
complete=true
