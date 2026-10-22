#!/usr/bin/env bash
# Keep one source-owned publisher and preserve source CI's publication boundary.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
fail() { printf 'publishing contract: FAIL — %s\n' "$*" >&2; exit 1; }
command -v yq >/dev/null || fail 'yq is required'
command -v jq >/dev/null || fail 'jq is required'
# Historical immutable callers resolve their own retained Git revision, not a
# default-branch export. Refuse both GitHub workflow extensions here.
retired_publisher_absent() {
  [[ ! -e "$1/publish-pages.yaml" && ! -e "$1/publish-pages.yml" ]]
}
retired_publisher_absent "$root/.github/workflows" || fail 'legacy callable publisher must remain retired'
ownership_fixture="$(mktemp -d)"
trap 'rm -rf "$ownership_fixture"' EXIT
retired_publisher_absent "$ownership_fixture" || fail 'empty workflow directory rejected'
for extension in yaml yml; do
  printf 'on: workflow_call\n' > "$ownership_fixture/publish-pages.$extension"
  if retired_publisher_absent "$ownership_fixture"; then
    fail "restored legacy .$extension export was accepted"
  fi
  rm "$ownership_fixture/publish-pages.$extension"
done
ci_json="$(yq -o=json '.' "$root/.github/workflows/ci.yaml")" || fail 'cannot parse source CI'
ci_gate_valid() {
  jq -e '
    .jobs.status as $gate |
    $gate.if == "always()" and $gate.permissions == {} and
    ($gate.needs | sort) == ([.jobs | keys[] | select(. != "status")] | sort) and
    ($gate.steps | length) == 2 and
    $gate.steps[0].if == null and
    all($gate.steps[]; .["continue-on-error"] == null or .["continue-on-error"] == false) and
    ($gate.steps[1].if | gsub("[[:space:]]+"; " ") | rtrimstr(" ")) ==
      "github.event_name == \u0027push\u0027 && github.ref == \u0027refs/heads/main\u0027 && github.repository == \u0027devantler-tech/business-site\u0027" and
    $gate.steps[1].env == {"PUBLICATION_RESULT":"\u0024{{ needs.publish-container.result }}"} and
    $gate.steps[1].run == "test \"\u0024PUBLICATION_RESULT\" = success" and
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
  '.jobs.status.if = "success()"' \
  '.jobs.status.steps |= .[0:1]' \
  '.jobs.status.steps[1].if = "true"' \
  '.jobs.status.steps[1].env.PUBLICATION_RESULT = "success"' \
  '.jobs.status.steps[1].run = "true"' \
  '.jobs.status.steps[1]["continue-on-error"] = true'; do
  if ci_gate_valid <<<"$(jq "$broken_ci" <<<"$ci_json")"; then
    fail 'a broken CI aggregate passed its negative control'
  fi
done
printf 'publishing contract: PASS — one current publisher, source CI aggregate and negative controls\n'
