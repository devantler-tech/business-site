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
    .permissions=={} and .jobs.admit["runs-on"]=="ubuntu-latest" and
    .jobs.admit.permissions=={"contents":"read","actions":"read","packages":"read"} and
    $s[0].with.ref=="\u0024{{ github.sha }}" and $s[0].with["persist-credentials"]==false and
    ([range(0;$s|length)|select($s[.].run=="bash scripts/admit-runtime-release.sh")]|length)==1 and
    ([range(0;$s|length)|select($s[.].run=="bash scripts/verify-runtime-origin.sh")]|length)==1 and
    all($s[]|select(.run=="bash scripts/admit-runtime-release.sh");
      .env.RUNTIME_PUBLICATION_ENABLED=="\u0024{{ vars.RUNTIME_PUBLICATION_ENABLED }}" and .env.GH_TOKEN=="\u0024{{ github.token }}") and
    all($s[]; .if==null and (.["continue-on-error"]==null or .["continue-on-error"]==false)) and
    all($s[]|select(.uses//""|startswith("actions/setup-go@")); .with.cache==false) and
    all($s[]|select(.uses!=null); .uses|test("@[0-9a-f]{40}$")) and
    $s[-1].run=="bash scripts/verify-runtime-origin.sh" and
    $s[-1].env.PREBUILT_CI_AUTH_MODE=="job-token" and
    $s[-1].env.PREBUILT_CI_READ_TOKEN=="\u0024{{ github.token }}" and
    all([.env, .jobs.admit.env, ($s[0:-1][]|.env)][];
      .PREBUILT_CI_AUTH_MODE==null and .PREBUILT_CI_READ_TOKEN==null) and
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
    (.jobs.release.uses|test("^devantler-tech/\\.github/\\.github/workflows/create-release\\.yaml@[0-9a-f]{40}$")) and
    .jobs.release.permissions=={"contents":"read","id-token":"write"} and
    .jobs.release.secrets=={"APP_PRIVATE_KEY":"\u0024{{ secrets.APP_PRIVATE_KEY }}"} and
    .jobs.release.with=={"disable-issue-side-effects":true,"warn-missing-breaking-bang":true,"dry-run":"\u0024{{ github.event_name == \u0027workflow_dispatch\u0027 && inputs.mode == \u0027dry-run\u0027 }}"}
  ' >/dev/null
}
# Admit a real enabled release, signed as this repository's own release workflow on its tag by the
# commit-pinned publish action called directly in this job, and independent mandatory pair readback.
publish_valid() {
  jq -e '
    .on=={release:{types:["published"]}} and
    .jobs.admit.if=="\u0024{{ github.repository == \u0027devantler-tech/business-site\u0027 && github.event_name == \u0027release\u0027 && vars.RUNTIME_PUBLICATION_ENABLED == \u0027true\u0027 && !github.event.release.draft && !github.event.release.prerelease && !startsWith(github.event.release.tag_name, \u0027identity-\u0027) }}" and
    all(.jobs.admit.steps[]|select(.run=="bash scripts/admit-runtime-release.sh"); .env.RELEASE_MODE=="release") and
    .jobs.publish.needs=="admit" and .jobs.publish.if==null and .jobs.publish.uses==null and .jobs.publish.secrets==null and
    .jobs.publish["runs-on"]=="ubuntu-latest" and
    .jobs.publish.permissions=={"contents":"read","actions":"read","packages":"write","id-token":"write"} and
    .jobs.publish.concurrency["cancel-in-progress"]==false and
    (.jobs.publish.steps|type=="array" and length==2) and
    (.jobs.publish.steps[0].uses//""|startswith("actions/checkout@")) and
    .jobs.publish.steps[0].with=={"ref":"\u0024{{ github.sha }}","persist-credentials":false} and
    (.jobs.publish.steps[1].uses//""|test("^devantler-tech/\\.github/actions/publish-app@[0-9a-f]{40}$")) and
    .jobs.publish.steps[1].with=={"mode":"prebuilt-webapp","app-name":"business-site","deploy-path":"./deploy","app-manifest":"base/webapp.yaml",
      "prebuilt-ci-auth-mode":"job-token",
      "prebuilt-image-digest":"\u0024{{ needs.admit.outputs.image-digest }}","prebuilt-public-digest":"\u0024{{ needs.admit.outputs.public-digest }}",
      "prebuilt-ci-run-id":"\u0024{{ needs.admit.outputs.ci-run-id }}","prebuilt-ci-run-attempt":"\u0024{{ needs.admit.outputs.ci-run-attempt }}"} and
    all(.jobs.publish.steps[]; .if==null and .env==null and .run==null and (.["continue-on-error"]==null or .["continue-on-error"]==false) and
      (.uses//""|test("@[0-9a-f]{40}$"))) and
    .jobs.verify.needs==["admit","publish"] and .jobs.verify.if==null and
    .jobs.verify.permissions=={"contents":"read","actions":"read","packages":"read"} and
    .jobs.verify.steps[-2].run=="bash scripts/verify-runtime-publication.sh" and
    (.jobs.verify.steps[-1].uses|startswith("actions/upload-artifact@")) and
    .jobs.verify.steps[-1].with=={"name":"business-site-runtime-publication","path":"\u0024{{ runner.temp }}/runtime-publication.json","if-no-files-found":"error","retention-days":30} and
    all(.jobs.verify.steps[]; .if==null and (.["continue-on-error"]==null or .["continue-on-error"]==false))
  ' >/dev/null
}
for data in "$release" "$publish"; do
  admission_valid <<<"$data" || fail 'read-only CI authentication and original signature verification are mandatory'
  for mutation in '.jobs.admit.permissions.contents="write"' '.jobs.admit.steps[-1]["continue-on-error"]=true' \
    '.jobs.admit.steps[-1].if="false"' '.jobs.admit.steps|=.[0:-1]' '.jobs.admit.steps[0].with.ref="main"' \
    '.jobs.admit.outputs["ci-run-attempt"]="1"' '.jobs.admit["runs-on"]="self-hosted"' \
    'del(.jobs.admit.steps[-1].env.PREBUILT_CI_READ_TOKEN)' '.jobs.admit.steps[-1].env.PREBUILT_CI_AUTH_MODE="anonymous"' \
    '.jobs.admit.env.PREBUILT_CI_READ_TOKEN=.jobs.admit.steps[-1].env.PREBUILT_CI_READ_TOKEN' \
    '.jobs.admit.steps |= map(if (.uses//""|startswith("actions/setup-go@")) then .with.cache=true else . end)'; do
    if admission_valid <<<"$(jq "$mutation" <<<"$data")"; then fail 'unsafe admission wiring accepted'; fi
  done
done
release_valid <<<"$release" || fail 'unsafe release trigger, default or credential boundary'
for mutation in '.jobs.admit.if="always()"' '.on.workflow_dispatch.inputs.mode.default="release"' '.on.push={branches:["main"]}' \
  '.jobs.release.with["dry-run"]=false' '.jobs.release.secrets="inherit"' '.jobs.release.needs=null' \
  '.jobs.release.permissions.contents="write"' '.jobs.release.permissions.issues="write"' \
  '.jobs.release.uses|=sub("@[0-9a-f]{40}$";"@main")' '.jobs.release.uses|=sub("@[0-9a-f]{40}$";"@v20.1.19")' \
  '.jobs.release.uses|=sub("[0-9a-f]$";"")' '.jobs.release.uses|=sub("^devantler-tech/";"foreign/")' \
  '.jobs.release.uses|=sub("create-release";"publish-app")'; do
  if release_valid <<<"$(jq "$mutation" <<<"$release")"; then fail 'unsafe release boundary accepted'; fi
done
publish_valid <<<"$publish" || fail 'unsafe publisher or missing independent readback'
for mutation in '.on.push={tags:["v*"]}' '.jobs.admit.if="always()"' '.jobs.publish.steps[1].with.mode="deployment"' \
  '.jobs.publish.steps[1].with["prebuilt-ci-run-id"]="1"' '.jobs.publish.steps[1].with["deploy-path"]="./"' \
  '.jobs.publish.steps[1].with["app-manifest"]="webapp.yaml"' \
  '.jobs.publish.steps[1].uses|=sub("@[0-9a-f]{40}$";"@main")' '.jobs.verify.needs=["admit"]' \
  '.jobs.verify.steps[-2]["continue-on-error"]=true' \
  '.jobs.publish.steps[1].uses|=sub("@[0-9a-f]{40}$";"@v21.7.2")' '.jobs.publish.steps[1].uses|=sub("[0-9a-f]$";"")' \
  '.jobs.publish.steps[1].uses|=sub("^devantler-tech/";"foreign/")' \
  '.jobs.publish.steps[1].uses|=sub("actions/publish-app";"actions/aggregate-job-checks")' \
  '.jobs.publish.steps[1]["continue-on-error"]=true' '.jobs.publish.steps[1].if="false"' \
  '.jobs.publish.steps[0].with["persist-credentials"]=true' '.jobs.publish.steps[0].with.ref="main"' \
  '.jobs.publish.permissions["id-token"]="none"' '.jobs.publish.permissions.contents="write"' \
  'del(.jobs.publish.permissions.actions)' '.jobs.publish.permissions.actions="write"' \
  '.jobs.publish["runs-on"]="self-hosted"' \
  'del(.jobs.publish.steps[1].with["prebuilt-ci-auth-mode"])' '.jobs.publish.steps[1].with["prebuilt-ci-auth-mode"]="anonymous"' \
  '.jobs.publish.concurrency["cancel-in-progress"]=true' \
  '.jobs.publish.uses="devantler-tech/.github/.github/workflows/publish-app.yaml@a76d5df9371cf712a8f60307fde060825a34cf35"' \
  '.jobs.publish.steps=[.jobs.publish.steps[1]]' '.jobs.publish.steps+=[{"run":"true"}]'; do
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
printf 'runtime publisher contract: PASS — off-state, immutable callers, own-repository signing, least privilege and mandatory independent verification\n'
