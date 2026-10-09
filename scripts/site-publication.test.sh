#!/usr/bin/env bash
# Exercise source-owned admission/receipts and detect weakened deployment wiring.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
# Stop the contract at the first violation and report its publication boundary.
fail() { printf 'site publication: FAIL — %s\n' "$*" >&2; exit 1; }
workflow="$root/.github/workflows/publish-site.yaml"
[[ -f "$workflow" ]] || fail 'source-owned workflow is missing'
json="$(yq -o=json '.' "$workflow")"
# Assert a named invariant against the actual parsed publisher workflow.
require() { jq -e "$1" <<<"$json" >/dev/null || fail "$2"; }
require '(.on | keys | sort) == ["push","schedule","workflow_dispatch"] and .on.push.branches == ["main"]' 'only own main push/manual/schedule can publish'
require '.on.workflow_dispatch.inputs.mode.type == "choice" and .on.workflow_dispatch.inputs.mode.default == "preview" and .on.workflow_dispatch.inputs.mode.options == ["preview","publish"]' 'manual dispatch defaults to artifact-only preview'
require '.permissions == {} and .jobs.build.permissions == {"contents":"read"} and .jobs.deploy.permissions == {"pages":"write","id-token":"write"}' 'authority must remain least privilege'
require '.jobs.build.steps[0].name == "Validate source-owned admission" and .jobs.build.steps[1].with.ref == "\u0024{{ github.sha }}" and .jobs.build.steps[1].with["persist-credentials"] == false and .jobs.build.steps[1].with.repository == null' 'admission precedes exact own-source checkout'
require '.jobs.build.steps[0].env == {"PUBLICATION_REPOSITORY":"\u0024{{ github.repository }}","PUBLICATION_REF":"\u0024{{ github.ref }}","PUBLICATION_EVENT":"\u0024{{ github.event_name }}","PUBLICATION_ENABLED":"\u0024{{ vars.SITE_PUBLICATION_ENABLED }}","PUBLICATION_MODE":"\u0024{{ inputs.mode || \u0027publish\u0027 }}","SOURCE_REVISION":"\u0024{{ github.sha }}"}' 'admission binds runtime context safely'
# Check ordered, fail-closed build steps and protected deployment dependencies.
layout_valid() {
  jq -e '
    .jobs.build.steps as $steps |
    [range(0; $steps | length) | select($steps[.].run == "npm ci")] as $install |
    [range(0; $steps | length) | select($steps[.].run == "bash scripts/refresh-public-stars.sh")] as $refresh |
    [range(0; $steps | length) | select($steps[.].run == "npm run build")] as $build |
    [range(0; $steps | length) | select($steps[.].name == "Record source-owned identity")] as $receipt |
    [range(0; $steps | length) | select(($steps[.].uses // "") | startswith("actions/upload-pages-artifact@"))] as $upload |
    all([$install,$refresh,$build,$receipt,$upload][]; length == 1) and
    $install[0] < $refresh[0] and $refresh[0] < $build[0] and $build[0] < $receipt[0] and $receipt[0] < $upload[0] and
    all($steps[]; .if == null and ( .["continue-on-error"] == null or .["continue-on-error"] == false)) and
    $steps[$refresh[0]].env.GH_TOKEN == "\u0024{{ github.token }}" and
    $steps[$upload[0]].with.path == "dist" and
    .jobs.deploy.needs == "build" and .jobs.deploy.environment.name == "github-pages" and
    .jobs.build.if == "\u0024{{ vars.SITE_PUBLICATION_ENABLED == \u0027true\u0027 || (github.event_name == \u0027workflow_dispatch\u0027 && inputs.mode == \u0027preview\u0027) }}" and
    .jobs.deploy.if == "\u0024{{ vars.SITE_PUBLICATION_ENABLED == \u0027true\u0027 && (github.event_name != \u0027workflow_dispatch\u0027 || inputs.mode == \u0027publish\u0027) }}" and
    .concurrency["cancel-in-progress"] == true and
    .concurrency.group == "pages-\u0024{{ github.event_name == \u0027workflow_dispatch\u0027 && inputs.mode == \u0027preview\u0027 && \u0027preview\u0027 || \u0027production\u0027 }}" and
    all(.jobs[].steps[]? | select(.uses != null); .uses | test("@[0-9a-f]{40}$"))
  ' >/dev/null
}
layout_valid <<<"$json" || fail 'unsafe build/deploy wiring'
for mutation in \
  '.jobs.build.steps |= map(select(.run != "bash scripts/refresh-public-stars.sh"))' \
  '.jobs.build.steps |= map(if .run == "bash scripts/refresh-public-stars.sh" then .["continue-on-error"] = true else . end)' \
  '.jobs.build.steps |= map(if .run == "npm ci" then .if = "false" else . end)' \
  '.jobs.build.steps |= reverse' \
  '.jobs.deploy.needs = null' \
  '.jobs.deploy.if = "always()"' \
  '.jobs.build.if = "always()"' \
  '.jobs.deploy.environment.name = "unprotected"' \
  '.concurrency["cancel-in-progress"] = false' \
  '.concurrency.group = "pages"' \
  '.jobs.build.steps[1].uses |= sub("@[0-9a-f]{40}$"; "@main")'; do
  if layout_valid <<<"$(jq "$mutation" <<<"$json")"; then fail "weakened wiring admitted: $mutation"; fi
done
admission="$(jq -r '.jobs.build.steps[0].run' <<<"$json")"
sha=0123456789abcdef0123456789abcdef01234567
# Execute the workflow's admission script with an independently varied context.
admit() {
  env PUBLICATION_REPOSITORY="$1" PUBLICATION_REF="$2" PUBLICATION_EVENT="$3" \
    PUBLICATION_ENABLED="$4" PUBLICATION_MODE="$5" SOURCE_REVISION="$6" \
    bash -c "$admission" >/dev/null 2>&1
}
for event in push schedule workflow_dispatch; do
  admit devantler-tech/business-site refs/heads/main "$event" true publish "$sha" || fail "own $event rejected"
  for enabled in '' false TRUE yes; do
    if admit devantler-tech/business-site refs/heads/main "$event" "$enabled" publish "$sha"; then fail 'default-off deployment admitted'; fi
  done
done
admit devantler-tech/business-site refs/heads/main workflow_dispatch '' preview "$sha" || fail 'safe preview rejected'
admit devantler-tech/business-site refs/heads/main workflow_dispatch true preview "$sha" || fail 'enabled preview rejected'
for repo in devantler-tech/monorepo foreign/business-site; do
  if admit "$repo" refs/heads/main push true publish "$sha"; then fail 'foreign publisher admitted'; fi
done
for ref in refs/heads/feature refs/tags/main refs/pull/1/merge; do
  if admit devantler-tech/business-site "$ref" workflow_dispatch true publish "$sha"; then fail 'non-main publication admitted'; fi
done
for event in pull_request pull_request_target merge_group workflow_call repository_dispatch; do
  if admit devantler-tech/business-site refs/heads/main "$event" true publish "$sha"; then fail 'unapproved event admitted'; fi
done
for revision in main deadbeef '' '0123456789abcdef0123456789abcdef0123456G'; do
  if admit devantler-tech/business-site refs/heads/main push true publish "$revision"; then fail 'mutable or malformed source admitted'; fi
done
if admit devantler-tech/business-site refs/heads/main push true preview "$sha"; then fail 'automatic preview admitted'; fi
if admit devantler-tech/business-site refs/heads/main workflow_dispatch true unknown "$sha"; then fail 'unknown mode admitted'; fi
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
git -C "$scratch" init -q
git -C "$scratch" -c user.name=Fixture -c user.email=fixture@example.invalid -c commit.gpgsign=false commit --allow-empty -qm fixture
source_sha="$(git -C "$scratch" rev-parse HEAD)"
mkdir "$scratch/dist"
receipt="$(jq -r '.jobs.build.steps[] | select(.name == "Record source-owned identity") | .run' <<<"$json")"
# Execute the workflow's receipt script inside a real fixture checkout.
write_receipt() {
  (cd "$scratch" && env SOURCE_REVISION="$1" GITHUB_SHA="$2" GITHUB_RUN_ID="$3" GITHUB_RUN_ATTEMPT="$4" \
    PUBLICATION_MODE="$5" GITHUB_STEP_SUMMARY="$scratch/summary" bash -c "$receipt") >/dev/null 2>&1
}
for mode in preview publish; do
  write_receipt "$source_sha" "$source_sha" 123456789 2 "$mode" || fail 'real source receipt failed'
  jq -e --arg source "$source_sha" --arg mode "$mode" \
    '. == {repository:"devantler-tech/business-site",sourceRevision:$source,workflowRunId:"123456789",workflowRunAttempt:"2",publicationMode:$mode}' \
    "$scratch/dist/publication-source.json" >/dev/null || fail 'receipt misidentifies own run or preview'
done
if write_receipt "$sha" "$source_sha" 123456789 2 publish; then fail 'checkout mismatch accepted'; fi
if write_receipt "$source_sha" "$sha" 123456789 2 publish; then fail 'context mismatch accepted'; fi
if write_receipt "$source_sha" "$source_sha" broken 2 publish; then fail 'invalid run identity accepted'; fi
if write_receipt "$source_sha" "$source_sha" 123456789 0 publish; then fail 'invalid attempt accepted'; fi
if write_receipt "$source_sha" "$source_sha" 123456789 2 unknown; then fail 'invalid receipt mode accepted'; fi
printf 'site publication: PASS — own main, default-off deployment, isolated preview, exact checkout/run receipt and negative controls\n'
