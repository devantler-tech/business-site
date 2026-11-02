#!/usr/bin/env bash
# Exercise the actual source-before-image publisher with local process doubles.
# This is ordering/refusal proof, never a real registry or publication receipt.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
mkdir "$scratch/bin"
cat >"$scratch/bin/gh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >>"$FIXTURE/calls"
if [[ "$1" == api ]];then
 [[ "$#" == 4 && "$2" == --hostname && "$3" == github.com ]] || exit 97
 case "$4" in
  repos/devantler-tech/business-site/commits/main) printf '{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n' ;;
  repos/devantler-tech/business-site/actions/runs/123) printf '{"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","run_attempt":2,"path":".github/workflows/ci.yaml","event":"push","head_branch":"main","status":"completed","conclusion":"success"}\n' ;;
  repos/devantler-tech/business-site/git/matching-refs/tags/identity-v4.20.0-devantler.1)
   if [[ "$KIND" == tag-conflict ]];then printf '[{"ref":"refs/tags/identity-v4.20.0-devantler.1","object":{"type":"commit","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}]\n';else printf '[]\n';fi ;;
  repos/devantler-tech/business-site/releases/tags/identity-v4.20.0-devantler.1) cat "$FIXTURE/release.json" ;;
  *) exit 98 ;;
 esac
elif [[ "$1" == release && "$2" == create ]];then
 printf '{"id":789,"tag_name":"identity-v4.20.0-devantler.1","draft":false,"prerelease":false,"assets":[]}\n' >"$FIXTURE/release.json"
elif [[ "$1" == release && "$2" == upload ]];then
 file=${!#};name=$(basename "$file");cp "$file" "$FIXTURE/$name"
 sha=$(shasum -a 256 "$file"|awk '{print $1}')
 jq --arg name "$name" --arg digest "sha256:$sha" '.assets += [{id:(100+(.assets|length)),name:$name,digest:$digest}]' "$FIXTURE/release.json" >"$FIXTURE/new"
 mv "$FIXTURE/new" "$FIXTURE/release.json"
else exit 99;fi
FIXTURE
cat >"$scratch/bin/curl" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
output='';headers='';config='';url='';previous=''
for arg in "$@";do
 case "$previous" in --output) output=$arg;;--dump-header) headers=$arg;;--config) config=$arg;;esac
 [[ "$arg" != https://* ]] || url=$arg
 previous=$arg
done
printf 'curl %s\n' "$url" >>"$FIXTURE/calls"
if [[ "$url" == https://github.com/* ]];then
 name=${url##*/};cp "$FIXTURE/$name" "$output"
 if [[ "$KIND" == source-corrupt && "$name" == inputs.tar ]];then printf changed >>"$output";fi
elif [[ "$url" == https://ghcr.io/v2/* ]];then
 if [[ "$KIND" == registry-error ]];then printf 503;exit 0;fi
 if [[ ! -f "$FIXTURE/pushed" && "$KIND" != registry-conflict ]];then printf 404;printf '{}\n' >"$output";exit 0;fi
 cp "$FIXTURE/manifest.json" "$output"
 if [[ "$KIND" == registry-conflict ]];then jq '.config.digest="sha256:different"' "$output" >"$FIXTURE/new";mv "$FIXTURE/new" "$output";fi
 sha=$(shasum -a 256 "$output"|awk '{print $1}')
 printf 'HTTP/2 200\r\nDocker-Content-Digest: sha256:%s\r\n' "$sha" >"$headers"
 printf 200
elif [[ -n "$config" ]];then
 printf '{"token":"fixture-token"}\n' >"$output"
else exit 98;fi
FIXTURE
cat >"$scratch/bin/docker" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >>"$FIXTURE/calls"
case "$1" in
 login) cat >/dev/null;;tag) :;;push)
  # All three anonymous source reads must finish before any image upload.
  [[ "$(grep -c 'curl https://github.com/' "$FIXTURE/calls")" == 3 ]] || exit 96
  touch "$FIXTURE/pushed";;image) [[ "$2" == rm ]] || exit 97;;*) exit 98;;esac
FIXTURE
cat >"$scratch/bin/cosign" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
printf 'cosign %s\n' "$1" >>"$FIXTURE/calls"
case "$1" in
 sign) [[ -f "$FIXTURE/pushed" ]] || exit 96;;verify) printf '[{}]\n';;
 attest)
  previous='';for arg in "$@";do if [[ "$previous" == --predicate ]];then cp "$arg" "$FIXTURE/predicate.json";fi;previous=$arg;done;;
 verify-attestation)
  sha=$(shasum -a 256 "$FIXTURE/manifest.json"|awk '{print $1}')
  jq -n --arg digest "$sha" --slurpfile predicate "$FIXTURE/predicate.json" '{subject:[{digest:{sha256:$digest}}],predicate:$predicate[0]}' >"$FIXTURE/statement.json"
  if [[ "$KIND" == attestation-conflict ]];then jq '.predicate.ciRunAttempt=3' "$FIXTURE/statement.json" >"$FIXTURE/new";mv "$FIXTURE/new" "$FIXTURE/statement.json";fi
  jq -n --arg payload "$(base64 <"$FIXTURE/statement.json"|tr -d '\n')" '{payload:$payload}' ;;
 *) exit 98;;esac
FIXTURE
chmod +x "$scratch/bin/"{gh,curl,docker,cosign}
for kind in valid tag-conflict source-corrupt registry-conflict registry-error attestation-conflict bad-retention bad-profile bad-authority; do
  fixture="$scratch/$kind"
  mkdir -p "$fixture/runner/identity-forge.fixture/restored/native/"{zitadel-handoff,zitadel-offline-replay,zitadel-base-source} "$fixture/runner/identity-forge.fixture/saved"
  work="$fixture/runner/identity-forge.fixture"
  native="$work/restored/native"
  printf source >"$native/zitadel-offline-replay/inputs.tar"
  printf base >"$native/zitadel-base-source/base-source.tar"
  input_sha=$(shasum -a 256 "$native/zitadel-offline-replay/inputs.tar" | awk '{print $1}')
  base_sha=$(shasum -a 256 "$native/zitadel-base-source/base-source.tar" | awk '{print $1}')
  jq -n --arg input "$input_sha" --arg base "$base_sha" '{originalBinarySha256:("c"*64),localImageId:("sha256:"+("b"*64)),contextSha256:("d"*64),sourceManifestSha256:("e"*64),inputManifestSha256:("f"*64),files:[{path:"zitadel-offline-replay/inputs.tar",bytes:6,sha256:$input},{path:"zitadel-base-source/base-source.tar",bytes:4,sha256:$base}]}' >"$native/zitadel-handoff/handoff.json"
  jq -n '{schemaVersion:1,sourceSha:("a"*40),callerSha:("a"*40),ciRunId:"123",ciRunAttempt:2,artifactSha256:("9"*64),forgeAdmission:true,imagePublished:false,portalActivated:false}' >"$work/forge.json"
  jq -n '{schemaVersion:1,profile:"identity-saved-image-v1",localImageId:("sha256:"+("b"*64)),imageRetainedForPublication:true,imagePublished:false,portalActivated:false}' >"$work/saved/saved-image.json"
  case "$kind" in
  bad-retention)
    jq '.imageRetainedForPublication=false' "$work/saved/saved-image.json" >"$fixture/new"
    mv "$fixture/new" "$work/saved/saved-image.json"
    ;;
  bad-profile)
    jq '.profile="different"' "$work/saved/saved-image.json" >"$fixture/new"
    mv "$fixture/new" "$work/saved/saved-image.json"
    ;;
  bad-authority)
    jq '.imagePublished=true' "$work/saved/saved-image.json" >"$fixture/new"
    mv "$fixture/new" "$work/saved/saved-image.json"
    ;;
  esac
  jq -n '{baseManifestSha256:("8"*64)}' >"$work/base-auth.json"
  jq -n '{schemaVersion:2,config:{digest:("sha256:"+("b"*64))},layers:[{digest:("sha256:"+("7"*64))}]}' >"$fixture/manifest.json"
  : >"$fixture/calls"
  if env PATH="$scratch/bin:$PATH" FIXTURE="$fixture" KIND="$kind" RUNNER_TEMP="$fixture/runner" IDENTITY_RELEASE_WORK="$work" IDENTITY_RELEASE_MODE=publish IDENTITY_CI_RUN_ID=123 GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REF=refs/heads/main GITHUB_ACTOR=devantler GH_TOKEN=fixture bash "$root/scripts/publish-identity-release.sh" >"$fixture/stdout" 2>"$fixture/stderr"; then
    [[ "$kind" == valid ]] || {
      printf 'publisher accepted %s\n' "$kind" >&2
      exit 1
    }
    jq -e '.sourceOfferPublished==true and .imagePublished==true and .signatureVerified==true and .portalActivated==false' "$fixture/runner/identity-publication.json" >/dev/null
  else
    if [[ "$kind" == valid ]]; then
      cat "$fixture/stderr"
      exit 1
    fi
    test ! -f "$fixture/runner/identity-publication.json"
    if [[ "$kind" == bad-* ]]; then test ! -s "$fixture/calls"; fi
    if [[ "$kind" != attestation-conflict ]]; then test ! -f "$fixture/pushed"; fi
  fi
done
printf 'identity publisher: PASS — actual source-first script and conflicting tag/source/registry/provenance refusals\n'
