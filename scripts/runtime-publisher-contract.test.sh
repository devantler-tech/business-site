#!/usr/bin/env bash
# Validate the privileged consumers of real admission outputs, retaining broken-wiring controls.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
# Name the broken workflow boundary rather than treating a parse failure as an accepted mutation.
fail() {
  printf 'runtime publisher contract: FAIL — %s\n' "$*" >&2
  exit 1
}
release="$(yq -o=json '.' "$root/.github/workflows/release-runtime.yaml")" || fail 'source-owned release caller is missing'
publish="$(yq -o=json '.' "$root/.github/workflows/publish-runtime.yaml")" || fail 'source-owned publisher is missing'
# Accept only mandatory, read-only admission and original-image verification on the frozen source.
admission_valid() {
  jq -e '
    .jobs.admit.steps as $s |
    .permissions=={} and .jobs.admit.permissions=={"contents":"read","actions":"read","packages":"read"} and
    $s[0].with.ref=="\u0024{{ github.sha }}" and $s[0].with["persist-credentials"]==false and
    ([range(0;$s|length)|select($s[.].run=="bash scripts/admit-runtime-release.sh")]|length)==1 and
    ([range(0;$s|length)|select($s[.].run=="bash scripts/verify-runtime-origin.sh")]|length)==1 and
    all($s[]|select(.run=="bash scripts/admit-runtime-release.sh");
      .env.RUNTIME_PUBLICATION_ENABLED=="\u0024{{ vars.RUNTIME_PUBLICATION_ENABLED }}" and .env.GH_TOKEN=="\u0024{{ github.token }}") and
    all($s[]; .if==null and (.["continue-on-error"]==null or .["continue-on-error"]==false)) and
    all($s[]|select(.uses//""|startswith("actions/setup-go@")); .with.cache==false) and
    all($s[]|select(.uses!=null); .uses|test("@[0-9a-f]{40}$")) and
    $s[-1].run=="bash scripts/verify-runtime-origin.sh" and
    .jobs.admit.outputs["image-digest"]=="\u0024{{ steps.admit.outputs.image-digest }}" and
    .jobs.admit.outputs["ci-run-id"]=="\u0024{{ steps.admit.outputs.ci-run-id }}" and
    .jobs.admit.outputs["ci-run-attempt"]=="\u0024{{ steps.admit.outputs.ci-run-attempt }}" and
    .jobs.admit.outputs["public-digest"]=="\u0024{{ steps.admit.outputs.public-digest }}"
  ' >/dev/null
}
# Admit a main-only manual release with a default dry run and narrowly forwarded App credential.
release_valid() {
  jq -e '
    (.on|keys)==["workflow_dispatch"] and
    .on.workflow_dispatch.inputs.mode.default=="dry-run" and .on.workflow_dispatch.inputs.mode.options==["dry-run","release"] and
    .jobs.admit.if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027workflow_dispatch\u0027 && github.ref == \u0027refs/heads/main\u0027 && (inputs.mode == \u0027dry-run\u0027 || (inputs.mode == \u0027release\u0027 && vars.RUNTIME_PUBLICATION_ENABLED == \u0027true\u0027)) }}" and
    all(.jobs.admit.steps[]|select(.run=="bash scripts/admit-runtime-release.sh"); .env.RELEASE_MODE=="\u0024{{ inputs.mode }}") and
    .jobs.release.needs=="admit" and .jobs.release.if==null and
    .jobs.release.uses=="devantler-tech/.github/.github/workflows/create-release.yaml@bd24a3e1bbbf6aba2d7afd58a2fd8a3d67bebe6b" and
    .jobs.release.permissions=={"contents":"read"} and
    .jobs.release.secrets=={"APP_PRIVATE_KEY":"\u0024{{ secrets.APP_PRIVATE_KEY }}"} and
    .jobs.release.with=={"disable-issue-side-effects":true,"warn-missing-breaking-bang":true,"dry-run":"\u0024{{ github.event_name == \u0027workflow_dispatch\u0027 && inputs.mode == \u0027dry-run\u0027 }}"}
  ' >/dev/null
}
# Admit a real enabled release, exact accepted publisher and independent mandatory pair readback.
publish_valid() {
  jq -e '
    .on=={release:{types:["published"]}} and
    .jobs.admit.if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027release\u0027 && vars.RUNTIME_PUBLICATION_ENABLED == \u0027true\u0027 && !github.event.release.draft && !github.event.release.prerelease }}" and
    all(.jobs.admit.steps[]|select(.run=="bash scripts/admit-runtime-release.sh"); .env.RELEASE_MODE=="release") and
    .jobs.publish.needs=="admit" and .jobs.publish.if==null and
    .jobs.publish.uses=="devantler-tech/.github/.github/workflows/publish-app.yaml@467f07582436a26b62cf4aa756423512f85ba0c0" and
    .jobs.publish.permissions=={"contents":"read","packages":"write","id-token":"write"} and
    .jobs.publish.secrets==null and
    .jobs.publish.with=={"app-name":"business-site","deploy-path":"./deploy","enable-caller-pin":true,"enable-signed-promotion":true,"enable-prebuilt-webapp":true,"enable-signed-recovery":false,
      "prebuilt-image-digest":"\u0024{{ needs.admit.outputs.image-digest }}","prebuilt-public-digest":"\u0024{{ needs.admit.outputs.public-digest }}",
      "prebuilt-ci-run-id":"\u0024{{ needs.admit.outputs.ci-run-id }}","prebuilt-ci-run-attempt":"\u0024{{ needs.admit.outputs.ci-run-attempt }}"} and
    .jobs.verify.needs==["admit","publish"] and .jobs.verify.if==null and
    .jobs.verify.permissions=={"contents":"read","actions":"read","packages":"read"} and
    .jobs.verify.steps[-2].run=="bash scripts/verify-runtime-publication.sh" and
    (.jobs.verify.steps[-1].uses|startswith("actions/upload-artifact@")) and
    .jobs.verify.steps[-1].with=={"name":"business-site-runtime-publication","path":"\u0024{{ runner.temp }}/runtime-publication.json","if-no-files-found":"error","retention-days":30} and
    all(.jobs.verify.steps[]; .if==null and (.["continue-on-error"]==null or .["continue-on-error"]==false))
  ' >/dev/null
}
for data in "$release" "$publish"; do
  admission_valid <<<"$data" || fail 'credential-free admission and original signature verification are mandatory'
  for mutation in '.jobs.admit.permissions.contents="write"' '.jobs.admit.steps[-1]["continue-on-error"]=true' \
    '.jobs.admit.steps[-1].if="false"' '.jobs.admit.steps|=.[0:-1]' '.jobs.admit.steps[0].with.ref="main"' \
    '.jobs.admit.outputs["ci-run-attempt"]="1"' \
    '.jobs.admit.steps |= map(if (.uses//""|startswith("actions/setup-go@")) then .with.cache=true else . end)'; do
    if admission_valid <<<"$(jq "$mutation" <<<"$data")"; then fail 'unsafe admission wiring accepted'; fi
  done
done
release_valid <<<"$release" || fail 'unsafe release trigger, default or credential boundary'
for mutation in '.jobs.admit.if="always()"' '.on.workflow_dispatch.inputs.mode.default="release"' '.on.push={branches:["main"]}' \
  '.jobs.release.with["dry-run"]=false' '.jobs.release.secrets="inherit"' '.jobs.release.needs=null'; do
  if release_valid <<<"$(jq "$mutation" <<<"$release")"; then fail 'unsafe release boundary accepted'; fi
done
publish_valid <<<"$publish" || fail 'unsafe publisher or missing independent readback'
for mutation in '.on.push={tags:["v*"]}' '.jobs.admit.if="always()"' '.jobs.publish.with["enable-signed-recovery"]=true' \
  '.jobs.publish.with["enable-prebuilt-webapp"]=false' '.jobs.publish.with["prebuilt-ci-run-id"]="1"' \
  '.jobs.publish.uses|=sub("@[0-9a-f]{40}$";"@main")' '.jobs.verify.needs=["admit"]' \
  '.jobs.verify.steps[-2]["continue-on-error"]=true'; do
  if publish_valid <<<"$(jq "$mutation" <<<"$publish")"; then fail 'unsafe publisher boundary accepted'; fi
done
jq -e '.branches==["main"] and (.plugins|length)==3 and .plugins[0][0]=="@semantic-release/commit-analyzer" and
  .plugins[0][1].parserOpts.breakingHeaderPattern=="^(\\w*)(?:\\((.*)\\))?!: (.*)$" and
  .plugins[1][0]=="@semantic-release/release-notes-generator" and .plugins[2]=="@semantic-release/github"' "$root/.releaserc" >/dev/null || fail 'release must be main-only and GitHub-only'
ci="$(yq -o=json '.' "$root/.github/workflows/ci.yaml")"
# Require source CI and both always-run aggregates to include all three actual contract suites.
ci_valid() {
  jq -e '
    . as $workflow | .jobs["test-repository-contracts"] as $job |
    $job.steps as $steps |
    $job.if==null and
    all(["bash scripts/runtime-release.test.sh","bash scripts/runtime-verification.test.sh","bash scripts/runtime-publisher-contract.test.sh"][];
      . as $command | [$steps[]|select(.run==$command)] as $matches |
      ($matches|length)==1 and $matches[0].if==null and
      ($matches[0]["continue-on-error"]==null or $matches[0]["continue-on-error"]==false)) and
    any($steps[]; (.uses//""|startswith("actions/setup-go@")) and .with["go-version-file"]=="server/go.mod") and
    all(["status","validation"][]; . as $name | $workflow.jobs[$name].if=="always()" and
      ($workflow.jobs[$name].needs|index("test-repository-contracts"))!=null)
  ' >/dev/null
}
ci_valid <<<"$ci" || fail 'real receipt, verifier and workflow controls must run unconditionally in source CI'
for mutation in '.jobs["test-repository-contracts"].steps |= map(select(.run!="bash scripts/runtime-verification.test.sh"))' \
  '.jobs["test-repository-contracts"].steps |= map(if .run=="bash scripts/runtime-release.test.sh" then .if="false" else . end)' \
  '.jobs["test-repository-contracts"].steps |= map(if .run=="bash scripts/runtime-release.test.sh" then .["continue-on-error"]=true else . end)'; do
  if ci_valid <<<"$(jq "$mutation" <<<"$ci")"; then fail 'optional or absent runtime release controls accepted'; fi
done
printf 'runtime publisher contract: PASS — off-state, immutable callers, least privilege and mandatory independent verification\n'
