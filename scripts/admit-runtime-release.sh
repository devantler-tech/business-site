#!/usr/bin/env bash
# Read-only source/API/archive admission. Signature verification is a separate required step.
set -euo pipefail
export LC_ALL=C
readonly repo=devantler-tech/business-site
fail() {
  printf 'runtime release: refused %s\n' "$1" >&2
  exit 1
}
[[ "${GITHUB_REPOSITORY:-}" == "$repo" && "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || fail 'source context'
readonly source=$GITHUB_SHA
[[ -f "${GITHUB_EVENT_PATH:-}" && -n "${GITHUB_OUTPUT:-}" && -d "${RUNNER_TEMP:-}" ]] || fail 'workflow files'
[[ -f "$RUNNER_TEMP/runtime-receipt" && -x "$RUNNER_TEMP/runtime-receipt" ]] || fail 'compiled receipt reader'
event=${GITHUB_EVENT_NAME:-}
mode=${RELEASE_MODE:-}
[[ "$mode" == release || "$mode" == dry-run ]] || fail 'release mode'
if [[ "$event" != workflow_dispatch || "$mode" != dry-run ]]; then
  [[ "${RUNTIME_PUBLICATION_ENABLED:-}" == true ]] || fail 'publication is off'
fi
work="$(mktemp -d "$RUNNER_TEMP/runtime-admission.XXXXXX")"
trap 'rm -rf "$work"' EXIT
jq -se 'length==1 and (.[0]|type)=="object"' "$GITHUB_EVENT_PATH" >/dev/null || fail 'event document'
# The public-only API destinations are fixed here, never selected by an event URL.
read_json() {
  (
    ulimit -f 8192
    gh api --hostname github.com "repos/$repo/$1" >"$2"
  ) 2>/dev/null || fail 'API read'
  jq -se 'length==1 and (.[0]|type)=="object"' "$2" >/dev/null || fail 'API document'
}
positive_id() { [[ "$1" =~ ^[1-9][0-9]{0,14}$ ]]; }
version=''
tag=''
release_id=''
case "$event" in
workflow_dispatch)
  [[ "${GITHUB_REF:-}" == refs/heads/main ]] || fail 'manual main context'
  ;;
release)
  [[ "$mode" == release ]] || fail 'release mode'
  tag=${GITHUB_REF#refs/tags/}
  [[ "${GITHUB_REF:-}" == refs/tags/* && "$tag" =~ ^v(0|[1-9][0-9]{0,14})\.(0|[1-9][0-9]{0,14})\.(0|[1-9][0-9]{0,14})$ ]] || fail 'stable semantic-version tag'
  version=${tag#v}
  jq -e --arg tag "$tag" '.action=="published" and .release.tag_name==$tag and .release.draft==false and .release.prerelease==false and (.release.published_at|type)=="string"' "$GITHUB_EVENT_PATH" >/dev/null || fail 'published stable release'
  release_id="$(jq -er '.release.id|tostring' "$GITHUB_EVENT_PATH")"
  positive_id "$release_id" || fail 'release identity'
  read_json "releases/$release_id" "$work/release-before.json"
  jq -e --arg tag "$tag" --argjson id "$release_id" '.id==$id and .tag_name==$tag and .draft==false and .prerelease==false and (.published_at|type)=="string"' "$work/release-before.json" >/dev/null || fail 'current release identity'
  ;;
*) fail 'event is not admitted' ;;
esac

check_tag() {
  local destination=$1 oid kind depth=0
  read_json "git/ref/tags/$tag" "$destination"
  jq -e --arg ref "refs/tags/$tag" '.ref==$ref' "$destination" >/dev/null || fail 'tag ref'
  oid="$(jq -er '.object.sha' "$destination")"
  kind="$(jq -er '.object.type' "$destination")"
  while [[ "$kind" == tag && "$depth" -lt 5 ]]; do
    [[ "$oid" =~ ^[0-9a-f]{40}$ ]] || fail 'tag object identity'
    read_json "git/tags/$oid" "$work/tag-object.json"
    jq -e --arg sha "$oid" '.sha==$sha' "$work/tag-object.json" >/dev/null || fail 'tag object mismatch'
    oid="$(jq -er '.object.sha' "$work/tag-object.json")"
    kind="$(jq -er '.object.type' "$work/tag-object.json")"
    depth=$((depth + 1))
  done
  [[ "$kind" == commit && "$oid" == "$source" ]] || fail 'tag source differs from caller'
}
[[ -z "$tag" ]] || check_tag "$work/tag-before.json"
# A bounded unfiltered window avoids GitHub's inconsistent head_sha filter.
# No match or an ambiguous/malformed inventory fails, never vacuously succeeds.
read_json 'actions/runs?per_page=100' "$work/runs.json"
jq -e '(.workflow_runs|type)=="array" and (.workflow_runs|length)>0 and (.workflow_runs|length)<=100 and
    ([.workflow_runs[].id]|unique|length)==(.workflow_runs|length) and
    ([.workflow_runs[].id]==([.workflow_runs[].id]|sort|reverse))' "$work/runs.json" >/dev/null || fail 'bounded CI inventory'
run="$(jq -er --arg source "$source" '[.workflow_runs[]|select(.head_sha==$source and .head_branch=="main" and .event=="push" and .path==".github/workflows/ci.yaml")]|max_by(.id)|.id|select(.!=null)|tostring' "$work/runs.json")" || fail 'no original main CI in window'
positive_id "$run" || fail 'original CI identity'
read_json "actions/runs/$run" "$work/run-before.json"
attempt="$(jq -er '.run_attempt|tostring' "$work/run-before.json")"
[[ "$attempt" =~ ^[1-9][0-9]{0,5}$ ]] || fail 'current CI attempt'
check_run() {
  jq -e --arg source "$source" --arg repo "$repo" --argjson run "$run" --argjson attempt "$attempt" '
    .id==$run and .run_attempt==$attempt and .repository.full_name==$repo and .head_repository.full_name==$repo and
    .head_sha==$source and .head_branch=="main" and .path==".github/workflows/ci.yaml" and
    .event=="push" and .status=="completed" and .conclusion=="success"
  ' "$1" >/dev/null || fail 'original CI source, attempt or success'
}
check_run "$work/run-before.json"
read_json "actions/runs/$run/attempts/$attempt" "$work/attempt.json"
check_run "$work/attempt.json"
read_json "actions/runs/$run/artifacts?per_page=100" "$work/artifacts.json"
jq -e '(.artifacts|type)=="array" and (.artifacts|length)==.total_count and .total_count<=100' "$work/artifacts.json" >/dev/null || fail 'complete artifact inventory'
jq -ce --arg source "$source" --argjson run "$run" '
  [.artifacts[]|select(.name=="business-site-container-publication")]|select(length==1)|.[0]|
  select(.expired==false and (.size_in_bytes|type)=="number" and .size_in_bytes>0 and .size_in_bytes<=4194304 and
    (.id|type)=="number" and (.id|tostring|test("^[1-9][0-9]{0,14}$")) and
    (.digest|type)=="string" and (.digest|test("^sha256:[0-9a-f]{64}$")) and
    .workflow_run.id==$run and .workflow_run.head_sha==$source and .workflow_run.head_branch=="main" and
    .workflow_run.repository_id==1409101115 and .workflow_run.head_repository_id==1409101115)
' "$work/artifacts.json" >"$work/artifact-before.json" || fail 'unique source-bound archive metadata'
artifact="$(jq -er .id "$work/artifact-before.json")"
digest="$(jq -er '.digest|ltrimstr("sha256:")' "$work/artifact-before.json")"
(
  ulimit -f 8192
  gh api --hostname github.com "repos/$repo/actions/artifacts/$artifact/zip" >"$work/publication.zip"
) 2>/dev/null || fail 'bounded archive download'
"$RUNNER_TEMP/runtime-receipt" --archive "$work/publication.zip" --sha256 "$digest" \
  --source "$source" --run "$run" --attempt "$attempt" --artifact "$artifact" >"$work/receipt.json" || fail 'archive admission'
read_json "actions/runs/$run" "$work/run-after.json"
check_run "$work/run-after.json"
read_json "actions/artifacts/$artifact" "$work/artifact-after.json"
jq -e --slurpfile before "$work/artifact-before.json" '.expired==false and
  ([.id,.name,.digest,.size_in_bytes,.workflow_run]==[$before[0].id,$before[0].name,$before[0].digest,$before[0].size_in_bytes,$before[0].workflow_run])
' "$work/artifact-after.json" >/dev/null || fail 'artifact changed during admission'
if [[ -n "$tag" ]]; then
  check_tag "$work/tag-after.json"
  cmp -s "$work/tag-before.json" "$work/tag-after.json" || fail 'tag changed during admission'
  read_json "releases/$release_id" "$work/release-after.json"
  jq -e --slurpfile before "$work/release-before.json" '
    [.id,.tag_name,.draft,.prerelease,.published_at]==[$before[0].id,$before[0].tag_name,$before[0].draft,$before[0].prerelease,$before[0].published_at]
  ' "$work/release-after.json" >/dev/null || fail 'release changed during admission'
fi
{
  printf 'source=%s\nci-run-id=%s\nci-run-attempt=%s\n' "$source" "$run" "$attempt"
  printf 'image-digest=%s\npublic-digest=%s\nversion=%s\n' \
    "$(jq -er '.image|split("@")[1]' "$work/receipt.json")" "$(jq -er .publicDigest "$work/receipt.json")" "$version"
} >>"$GITHUB_OUTPUT"
