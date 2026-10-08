#!/usr/bin/env bash
# Exercise the real pre-checkout admission and the reusable publisher's boundaries.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
workflow="$root/.github/workflows/publish-pages.yaml"
fail() { printf 'publishing contract: FAIL — %s\n' "$*" >&2; exit 1; }
command -v yq >/dev/null || fail 'yq is required'
command -v jq >/dev/null || fail 'jq is required'
ci_json="$(yq -o=json '.' "$root/.github/workflows/ci.yaml")" || fail 'cannot parse source CI'
ci_gate_valid() {
  jq -e '
    .jobs.status as $gate |
    $gate.if == "always()" and $gate.permissions == {} and
    ($gate.needs | sort) == ([.jobs | keys[] | select(. != "status")] | sort) and
    ($gate.steps | length) == 1 and
    ($gate.steps[0].uses | test("^devantler-tech/\\.github/actions/aggregate-job-checks@[0-9a-f]{40}$")) and
    ($gate.steps[0].with["job-results"] as $results |
      all($gate.needs[]; . as $job | $results | contains("needs." + $job + ".result")))
  ' >/dev/null
}
ci_gate_valid <<<"$ci_json" || fail 'CI aggregate must resolve the canonical action and cover every source job'
ci_preview_valid() {
  jq -e '
    .jobs["build-docs"].steps as $steps |
    [range(0; $steps | length) | select($steps[.].run == "npm run build" and $steps[.].env.FEATURE_PREVIEW_BANNER == "true")] as $preview |
    [range(0; $steps | length) | select($steps[.].run == "npm run build" and $steps[.].env.FEATURE_PREVIEW_BANNER == "false")] as $production |
    [range(0; $steps | length) | select($steps[.].with.path == "dist")] as $artifacts |
    ($preview | length) == 1 and ($production | length) == 1 and ($artifacts | length) == 1 and
    $preview[0] < $production[0] and $production[0] < $artifacts[0] and
    all(($preview + $production)[]; $steps[.] | .["working-directory"] == "." and .if == null and .["continue-on-error"] == null)
  ' >/dev/null
}
ci_preview_valid <<<"$ci_json" || fail 'CI must validate preview-on before preview-off and upload only the final production output'
for broken_preview in \
  '.jobs["build-docs"].steps |= map(select(.env.FEATURE_PREVIEW_BANNER != "true"))' \
  '.jobs["build-docs"].steps |= map(if .env.FEATURE_PREVIEW_BANNER == "true" then .env.FEATURE_PREVIEW_BANNER = "false" else . end)' \
  '.jobs["build-docs"].steps |= map(select(.env.FEATURE_PREVIEW_BANNER != "false"))' \
  '.jobs["build-docs"].steps |= reverse' \
  '.jobs["build-docs"].steps |= ([.[-1]] + .[0:-1])' \
  '.jobs["build-docs"].steps |= map(if .env.FEATURE_PREVIEW_BANNER == "true" then .["continue-on-error"] = true else . end)'; do
  if ci_preview_valid <<<"$(jq "$broken_preview" <<<"$ci_json")"; then
    fail 'a broken preview/production build sequence passed its negative control'
  fi
done
for broken_ci in \
  '.jobs.status.steps[0].uses |= sub("/actions/aggregate-job-checks"; "/aggregate-job-checks")' \
  '.jobs.status.steps[0].uses |= sub("@[0-9a-f]{40}$"; "@main")' \
  '.jobs.status.needs |= map(select(. != "build-docs"))' \
  '.jobs.status.steps[0].with["job-results"] |= sub("needs.build-docs.result"; "ignored")' \
  '.jobs.status.if = "success()"'; do
  if ci_gate_valid <<<"$(jq "$broken_ci" <<<"$ci_json")"; then
    fail 'a broken CI aggregate passed its negative control'
  fi
done
json="$(yq -o=json '.' "$workflow")" || fail 'cannot parse publisher'
require() { jq -e "$1" <<<"$json" >/dev/null || fail "$2"; }
require '(.on | keys) == ["workflow_call"]' 'production is callable only, never an independent publisher'
require '.on.workflow_call.inputs["source-revision"] == {"description":"Reviewed business-site gitlink revision selected by the monorepo", "required":true, "type":"string"}' 'source input is required and has no branch default'
require '.permissions == {"contents":"read"} and .jobs.build.permissions == {"contents":"read"}' 'build has read-only authority'
require '.jobs.build.steps[0].name == "Validate publication admission"' 'admission runs before any checkout'
# jq decodes U+0024 as a literal dollar, not a shell variable expansion.
require '.jobs.build.steps[0].env == {"CALLER_REPOSITORY":"\u0024{{ github.repository }}","CALLER_REF":"\u0024{{ github.ref }}","CALLER_EVENT":"\u0024{{ github.event_name }}","SOURCE_REVISION":"\u0024{{ inputs.source-revision }}"}' 'admission reads context and input through environment, not interpolated shell'
require '.jobs.build.steps[1].with.repository == "devantler-tech/business-site" and .jobs.build.steps[1].with.ref == "\u0024{{ inputs.source-revision }}" and .jobs.build.steps[1].with["persist-credentials"] == false' 'checkout selects only the fixed site repository and exact source input'
require '.jobs.deploy.needs == "build" and .jobs.deploy.permissions == {"pages":"write","id-token":"write"} and .jobs.deploy.environment.name == "github-pages"' 'deployment depends on the admitted build and uses only Pages authority'
require '[.jobs[].steps[]? | select(has("continue-on-error"))] | length == 0' 'publisher cannot ignore failures'
require '[.jobs[].steps[]? | select(.uses != null) | .uses | test("@[0-9a-f]{40}$")] | all' 'every publisher action is immutably pinned'
publisher_layout_valid() {
  jq -e '
    .jobs.build.steps as $steps |
    [range(0; $steps | length) | select($steps[.].run == "npm ci")] as $install |
    [range(0; $steps | length) | select($steps[.].run == "npm run build")] as $build |
    [range(0; $steps | length) | select($steps[.].name == "Record public source identity")] as $receipt |
    [range(0; $steps | length) | select(($steps[.].uses // "") | startswith("actions/upload-pages-artifact@"))] as $upload |
    all([$install, $build, $receipt, $upload][]; length == 1) and
    $install[0] < $build[0] and $build[0] < $receipt[0] and $receipt[0] < $upload[0] and
    all(($install + $build + $receipt + $upload)[]; $steps[.] | .if == null and
      (.["continue-on-error"] == null or .["continue-on-error"] == false)) and
    all(($install + $build)[]; $steps[.]["working-directory"] == ".") and
    $steps[$upload[0]].with.path == "dist" and
    ([ $steps[] | select((.uses // "") | startswith("actions/setup-node@")) |
      .with["cache-dependency-path"] ] == ["package-lock.json"])
  ' >/dev/null
}
publisher_layout_valid <<<"$json" || fail 'publisher must install/build at the application root and upload its completed output'
for broken_layout in \
  '.jobs.build.steps |= map(if .run == "npm ci" then .["working-directory"] = "docs" else . end)' \
  '.jobs.build.steps |= map(if .run == "npm run build" then .if = "false" else . end)' \
  '.jobs.build.steps |= map(select(.name != "Record public source identity"))' \
  '.jobs.build.steps |= map(if (.uses // "" | startswith("actions/upload-pages-artifact@")) then .with.path = "docs/dist" else . end)' \
  '.jobs.build.steps |= map(if (.uses // "" | startswith("actions/setup-node@")) then .with["cache-dependency-path"] = "docs/package-lock.json" else . end)' \
  '.jobs.build.steps |= reverse'; do
  if publisher_layout_valid <<<"$(jq "$broken_layout" <<<"$json")"; then
    fail 'a broken root publication layout passed its negative control'
  fi
done
# Execute the actual workflow receipt step against the root artifact directory.
# This proves the moved output contains the exact source/caller identity, not
# merely that the YAML mentions a matching-looking path.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/dist"
receipt="$(jq -r '.jobs.build.steps[] | select(.name == "Record public source identity") | .run' <<<"$json")"
source_sha=0123456789abcdef0123456789abcdef01234567
caller_sha=89abcdef0123456789abcdef0123456789abcdef
(cd "$scratch" && env SOURCE_REVISION="$source_sha" GITHUB_SHA="$caller_sha" \
  GITHUB_STEP_SUMMARY="$scratch/summary" bash -c "$receipt") || fail 'root receipt step failed'
jq -e --arg source "$source_sha" --arg caller "$caller_sha" \
  '. == {repository:"devantler-tech/business-site",sourceRevision:$source,callerRevision:$caller}' \
  "$scratch/dist/publication-source.json" >/dev/null || fail 'root artifact receipt has the wrong identity'
admission="$(jq -r '.jobs.build.steps[0].run' <<<"$json")"
[[ -n "$admission" && "$admission" != null ]] || fail 'admission is missing'
admit() {
  env CALLER_REPOSITORY="$1" CALLER_REF="$2" CALLER_EVENT="$3" SOURCE_REVISION="$4" \
    bash -c "$admission" >/dev/null 2>&1
}
sha=0123456789abcdef0123456789abcdef01234567
admit devantler-tech/monorepo refs/heads/main push "$sha" || fail 'reviewed main push rejected'
admit devantler-tech/monorepo refs/heads/main workflow_dispatch "$sha" || fail 'reviewed main dispatch rejected'
for input in main refs/heads/main deadbeef '0123456789abcdef0123456789abcdef0123456G' '0123456789abcdef0123456789abcdef012345678' 'main; echo unsafe'; do
  if admit devantler-tech/monorepo refs/heads/main push "$input"; then fail "non-immutable source admitted: $input"; fi
done
if admit unrelated/repository refs/heads/main push "$sha"; then fail 'foreign caller admitted'; fi
if admit devantler-tech/business-site refs/heads/main push "$sha"; then fail 'competing site-repository publisher admitted'; fi
if admit devantler-tech/monorepo refs/heads/feature push "$sha"; then fail 'branch deployment admitted'; fi
for event in pull_request pull_request_target merge_group schedule; do
  if admit devantler-tech/monorepo refs/heads/main "$event" "$sha"; then fail "$event deployment admitted"; fi
done
printf 'publishing contract: PASS — fixed caller, reviewed main, immutable source, least privilege and negative controls\n'
