#!/usr/bin/env bash
# Guard the actual otherwise-inert base-source collector's native CI invocation.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
yq -o=json '.' "$root/.github/workflows/ci.yaml" > "$scratch/ci.json"
check() {
  jq -e '
    .jobs["build-zitadel-linux"] as $j |
    [$j.steps[]|select(.name=="Retain and verify the frozen identity base sources")] as $s |
    ($s|length)==1 and $s[0].if==null and ($s[0]["continue-on-error"]==null or $s[0]["continue-on-error"]==false) and
    $s[0]["working-directory"]=="server" and
    $s[0].run=="go test -race ./cmd/identity-release\nCGO_ENABLED=0 \"$IDENTITY_VERIFIER_GO\" build -trimpath -buildvcs=false -o \"$RUNNER_TEMP/identity-release\" ./cmd/identity-release\nbash ../scripts/retain-zitadel-base-source.test.sh\nbash ../scripts/retain-zitadel-base-source.sh --capture --command \"$RUNNER_TEMP/identity-release\" --output \"$RUNNER_TEMP/zitadel-base-source\"\n" and
    ([$j.steps[].name]|index("Prepare original verifier compiler and private caches") < index("Retain and verify the frozen identity base sources") and index("Retain and verify the frozen identity base sources") < index("Setup the frozen identity Go compiler")) and
    any(.jobs.changes.steps[]; .id=="filter" and (.with.filters|contains("server/cmd/identity-release/**"))) and
    all($j.steps[]|select(.uses//""|startswith("actions/upload-artifact@"));
      (.with.path|contains("/zitadel-base-source/evidence/capture.json")) and
      (.with.path|contains("/zitadel-base-source/evidence/verify.json")) and
      (.with.path|contains("/zitadel-base-source/evidence/pack.json")) and
      (.with.path|contains("/zitadel-base-source/evidence/base-source.json")) and
      (.with.path|contains("/zitadel-base-source/capsule")|not) and
      (.with.path|contains("/zitadel-base-source/base-source.tar")|not))
  ' "$1" >/dev/null
}
check "$scratch/ci.json"
controls=0
for mutation in \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name!="Retain and verify the frozen identity base sources"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .run|=sub("retain-zitadel-base-source.sh";"unrelated.sh") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .run=("if false; then\n"+.run+"fi\n") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .run|=sub("go test -race";"go test") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name=="Retain and verify the frozen identity base sources" then .run=("exit 0\n"+.run) else . end)' \
  '.jobs.changes.steps |= map(if .id=="filter" then .with.filters|=sub("server/cmd/identity-release/\\*\\*";"unrelated/**") else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if (.uses//""|startswith("actions/upload-artifact@")) then .with.path|=sub("zitadel-base-source/evidence/capture.json";"zitadel-base-source/capsule/**") else . end)'; do
  jq "$mutation" "$scratch/ci.json" > "$scratch/bad.json"
  if check "$scratch/bad.json"; then echo 'broken base-source invocation admitted' >&2; exit 1; fi
  controls=$((controls+1))
done
printf 'Identity base-source CI contract passed, including %s broken-invocation controls.\n' "$controls"
