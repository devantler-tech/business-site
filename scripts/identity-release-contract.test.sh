#!/usr/bin/env bash
# Refuse widened privilege, optional admission or unbounded raw transport.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
fail() {
  printf 'identity publication contract: FAIL %s\n' "$1" >&2
  exit 1
}
yq -o=json '.' "$root/.github/workflows/ci.yaml" >"$scratch/ci.json"
yq -o=json '.' "$root/.github/workflows/publish-identity.yaml" >"$scratch/publish.json"
expected_base="$(
  cat <<'STEP'
seal="$(jq -er .manifestSha256 "$RUNNER_TEMP/zitadel-base-source/evidence/capture.json")"
"$RUNNER_TEMP/identity-release" verify-base-auth --input "$RUNNER_TEMP/zitadel-base-source/capsule" --sha256 "$seal" --repository "$GITHUB_WORKSPACE" --output "$RUNNER_TEMP/zitadel-base-auth" > "$RUNNER_TEMP/zitadel-base-auth.json"
STEP
)"$'\n'
expected_seal="$(
  cat <<'STEP'
mkdir -m 700 -- "$RUNNER_TEMP/identity-carrier"
"$RUNNER_TEMP/identity-release" pack-carrier --input "$RUNNER_TEMP" --output "$RUNNER_TEMP/identity-carrier/carrier.tar" > "$RUNNER_TEMP/identity-carrier/carrier.json"
STEP
)"$'\n'
expected_readback="$(
  cat <<'STEP'
test "$(df -Pk "$RUNNER_TEMP" | awk 'NR==2 {print $4}')" -ge 4194304
work="$(mktemp -d "$RUNNER_TEMP/identity-forge.XXXXXX")"
mkdir -m 700 "$work/restored"
archive="$(jq -er .archiveSha256 "$RUNNER_TEMP/identity-carrier/carrier.json")"
manifest="$(jq -er .handoffSha256 "$RUNNER_TEMP/identity-carrier/carrier.json")"
"$RUNNER_TEMP/identity-release" unpack-carrier --input "$RUNNER_TEMP/identity-carrier/carrier.tar" --sha256 "$archive" --manifest-sha256 "$manifest" --source-sha "$GITHUB_SHA" --run-id "$GITHUB_RUN_ID" --run-attempt "$GITHUB_RUN_ATTEMPT" --output "$work/restored/native"
IDENTITY_RELEASE_WORK="$work" IDENTITY_RELEASE_MODE=dry-run bash scripts/verify-identity-original.sh
STEP
)"$'\n'
carrier_valid() {
  jq -e --arg base "$expected_base" --arg seal "$expected_seal" --arg readback "$expected_readback" '
 . as $workflow | .jobs["build-zitadel-linux"] as $job | $job.steps as $s | ($s|map(.name)) as $names |
 $job.permissions=={contents:"read"} and
 any(.jobs.changes.steps[];.id=="filter" and (.with.filters|contains("scripts/verify-identity-original.sh")) and (.with.filters|contains("third_party/debian-source-signers/**"))) and
 ([$s[]|select(.uses//""|startswith("actions/upload-artifact@"))]|length)==2 and
 ([$s[]|select(.name=="Retain the admitted original publication carrier")]|length)==1 and
 all($s[]|select(.name=="Retain the admitted original publication carrier");
 .uses=="actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9" and
 .if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027push\u0027 && github.ref == \u0027refs/heads/main\u0027 }}" and
 .with=={name:"identity-original-\u0024{{ github.run_id }}-\u0024{{ github.run_attempt }}",path:"\u0024{{ runner.temp }}/identity-carrier/carrier.tar\n\u0024{{ runner.temp }}/identity-carrier/carrier.json\n","compression-level":0,"if-no-files-found":"error","retention-days":1} and .["continue-on-error"]==null) and
 all(["Verify frozen base signatures for original transport","Bind the original local archives and native witnesses","Read back the original saved candidate without rebuilding","Seal the original publication carrier"][];
 . as $name | [$s[]|select(.name==$name)] as $matches | ($matches|length)==1 and $matches[0].if==null and ($matches[0]["continue-on-error"]==null or $matches[0]["continue-on-error"]==false)) and
 ($names|index("Scan the actual frozen identity executable")) < ($names|index("Verify frozen base signatures for original transport")) and
 ($names|index("Verify frozen base signatures for original transport")) < ($names|index("Bind the original local archives and native witnesses")) and
 ($names|index("Bind the original local archives and native witnesses")) < ($names|index("Seal the original publication carrier")) and
 ($names|index("Seal the original publication carrier")) < ($names|index("Read back the original saved candidate without rebuilding")) and
 ($names|index("Read back the original saved candidate without rebuilding")) < ($names|index("Retain the admitted original publication carrier")) and
 any($s[];.name=="Verify frozen base signatures for original transport" and .run==$base) and
 any($s[];.name=="Seal the original publication carrier" and .run==$seal) and
 any($s[];.name=="Read back the original saved candidate without rebuilding" and .run==$readback) and
 any($workflow.jobs["test-repository-contracts"].steps[];.run=="bash scripts/identity-release-contract.test.sh" and .if==null and .["continue-on-error"]==null)
 ' >/dev/null
}
publisher_valid() {
  jq -e '
 (.on|keys)==["workflow_dispatch"] and
 .on.workflow_dispatch.inputs.mode.default=="dry-run" and .on.workflow_dispatch.inputs.mode.options==["dry-run","publish"] and
 .permissions=={} and .concurrency=={group:"identity-original-publication","cancel-in-progress":false} and
 .jobs.verify.if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027workflow_dispatch\u0027 && github.ref == \u0027refs/heads/main\u0027 }}" and
 .jobs.publish.if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027workflow_dispatch\u0027 && github.ref == \u0027refs/heads/main\u0027 && inputs.mode == \u0027publish\u0027 }}" and
 .jobs.publish.needs=="verify" and
 .jobs.verify.permissions=={contents:"read",actions:"read"} and
 .jobs.publish.permissions=={contents:"write",actions:"read",packages:"write","id-token":"write"} and
 .jobs.verify.env.IDENTITY_RELEASE_MODE=="dry-run" and .jobs.publish.env.IDENTITY_RELEASE_MODE=="publish" and
 all(.jobs[];.env.IDENTITY_CI_RUN_ID=="\u0024{{ inputs.ci-run-id }}" and
  all(.steps[];.if==null and (.["continue-on-error"]==null or .["continue-on-error"]==false)) and
  all(.steps[]|select(.uses!=null);.uses|test("@[a-f0-9]{40}$")) and
  all(.steps[]|select(.uses//""|startswith("actions/checkout@"));.with=={ref:"\u0024{{ github.sha }}","persist-credentials":false}) and
  all(.steps[]|select(.uses//""|startswith("actions/setup-go@"));.with.cache==false) and
  ([.steps[]|select(.run=="bash scripts/prepare-identity-release.sh")]|length)==1 and
  all(.steps[]|select(.run=="bash scripts/prepare-identity-release.sh");.env.GH_TOKEN=="\u0024{{ github.token }}")) and
 ([.jobs.publish.steps[].run]|index("bash scripts/prepare-identity-release.sh")) < ([.jobs.publish.steps[].run]|index("bash scripts/publish-identity-release.sh")) and
 ([.jobs.publish.steps[]|select(.run=="bash scripts/publish-identity-release.sh")]|length)==1 and
 .jobs.publish.steps[-1].with.path=="\u0024{{ runner.temp }}/identity-publication.json"
 ' >/dev/null
}
carrier_valid <"$scratch/ci.json" || fail 'original carrier boundary'
for mutation in '.jobs["build-zitadel-linux"].permissions.contents="write"' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Retain the admitted original publication carrier" then .if="always()" else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Retain the admitted original publication carrier" then .with["retention-days"]=30 else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Retain the admitted original publication carrier" then .with.path="**" else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Verify frozen base signatures for original transport" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Read back the original saved candidate without rebuilding" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(if .name=="Read back the original saved candidate without rebuilding" then .run="echo passed" else . end)' \
  '.jobs["build-zitadel-linux"].steps|=map(select(.name!="Seal the original publication carrier"))'; do
  if jq "$mutation" "$scratch/ci.json" | carrier_valid; then fail 'unsafe carrier mutation accepted'; fi
done
for step in 'Verify frozen base signatures for original transport' 'Seal the original publication carrier' 'Read back the original saved candidate without rebuilding'; do
  for wrapper in early-success conditional; do
    if jq --arg step "$step" --arg wrapper "$wrapper" '.jobs["build-zitadel-linux"].steps|=map(if .name==$step then .run=(if $wrapper=="early-success" then "exit 0\n"+.run else "if false; then\n"+.run+"fi\n" end) else . end)' "$scratch/ci.json" | carrier_valid; then fail 'skipped real producer accepted'; fi
  done
done
publisher_valid <"$scratch/publish.json" || fail 'manual privilege boundary'
for mutation in '.on.push={branches:["main"]}' '.on.workflow_dispatch.inputs.mode.default="publish"' '.jobs.verify.permissions.contents="write"' \
  '.jobs.publish.if="always()"' '.jobs.publish.needs=null' '.concurrency["cancel-in-progress"]=true' \
  '.jobs.publish.steps|=map(select(.run!="bash scripts/prepare-identity-release.sh"))' \
  '.jobs.verify.steps|=map(if .run=="bash scripts/prepare-identity-release.sh" then .if="false" else . end)' \
  '.jobs.publish.steps|=map(if .run=="bash scripts/publish-identity-release.sh" then .["continue-on-error"]=true else . end)' \
  '.jobs.publish.steps[0].with.ref="main"'; do
  if jq "$mutation" "$scratch/publish.json" | publisher_valid; then fail 'unsafe publisher mutation accepted'; fi
done
bash "$root/scripts/identity-release-forge.test.sh"
bash "$root/scripts/identity-release-publisher.test.sh"
printf 'identity publication contract: PASS — original transport, main-only default-off release and refusal controls\n'
