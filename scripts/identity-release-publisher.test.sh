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
 [[ "$2" == --hostname && "$3" == github.com ]] || exit 97
 paginated=false
 if [[ "$#" == 6 && "$4" == --paginate && "$5" == --slurp ]];then paginated=true
 elif [[ "$#" != 4 ]];then exit 97;fi
 endpoint=${!#}
 case "$endpoint" in
  repos/devantler-tech/business-site/commits/main) printf '{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n' ;;
  repos/devantler-tech/business-site/actions/runs/123) printf '{"head_sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","run_attempt":2,"path":".github/workflows/ci.yaml","event":"push","head_branch":"main","status":"completed","conclusion":"success"}\n' ;;
  repos/devantler-tech/business-site/git/matching-refs/tags/identity-v4.20.0-devantler.1)
   if [[ "$KIND" == tag-conflict ]];then printf '[{"ref":"refs/tags/identity-v4.20.0-devantler.1","object":{"type":"commit","sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}]\n';
   elif [[ -f "$FIXTURE/release.json" ]] && jq -e '.draft==false' "$FIXTURE/release.json" >/dev/null;then printf '[{"ref":"refs/tags/identity-v4.20.0-devantler.1","object":{"type":"commit","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]\n';else printf '[]\n';fi ;;
  repos/devantler-tech/business-site/releases\?per_page=100*)
   releases='[]'
   if [[ -f "$FIXTURE/release.json" ]];then releases="$(jq '[.]' "$FIXTURE/release.json")";fi
   case "$KIND" in
   paged-*)
    prefix="$(jq -n '[range(0;100)|{id:(1000+.),tag_name:("other-"+tostring)}]')"
    if [[ "$KIND" == paged-duplicate ]];then prefix="$(jq '.[0].tag_name="identity-v4.20.0-devantler.1"' <<<"$prefix")";fi
    pages="$(jq -n --argjson prefix "$prefix" --argjson releases "$releases" '[$prefix,$releases]')" ;;
   malformed-inventory) pages="$(jq -n '[[range(0;100)|{tag_name:"other"}],{}]')" ;;
   oversized-inventory|boundary-inventory)
    prefix="$(jq -n '[range(0;100)|{tag_name:"other"}]')"
    pages="$(jq -n --argjson prefix "$prefix" --argjson releases "$releases" --arg kind "$KIND" '[range(0;101)|if .==99 and $kind=="boundary-inventory" then $releases else $prefix end]')" ;;
   failed-inventory) printf '[[]]\n';exit 42 ;;
   *) pages="$(jq -n --argjson releases "$releases" '[$releases]')" ;;
   esac
   if [[ "$paginated" == true ]];then
    if [[ "$KIND" == oversized-inventory ]];then
      printf 'inventory-page %s\n' {1..101} >>"$FIXTURE/calls"
    fi
    printf '%s\n' "$pages"
   else
    page=${endpoint##*&page=}
    [[ "$page" =~ ^[1-9][0-9]*$ ]] || exit 97
    printf 'inventory-page %s\n' "$page" >>"$FIXTURE/calls"
    jq --argjson page "$page" '.[$page-1]' <<<"$pages"
   fi ;;
  repos/devantler-tech/business-site/releases/tags/identity-v4.20.0-devantler.1)
   jq -e '.draft==false' "$FIXTURE/release.json" >/dev/null || exit 44;cat "$FIXTURE/release.json" ;;
  repos/devantler-tech/business-site/releases/789) cat "$FIXTURE/release.json" ;;
  *) exit 98 ;;
 esac
elif [[ "$1" == release && "$2" == create ]];then
 draft=false;for arg in "$@";do [[ "$arg" != --draft ]] || draft=true;done
 jq -n --argjson draft "$draft" '{id:789,tag_name:"identity-v4.20.0-devantler.1",target_commitish:("a"*40),draft:$draft,prerelease:false,immutable:($draft|not),assets:[]}' >"$FIXTURE/release.json"
elif [[ "$1" == release && "$2" == upload ]];then
 if ! jq -e '.draft==true' "$FIXTURE/release.json" >/dev/null;then printf 'immutable release refuses asset uploads\n' >&2;exit 95;fi
 file=${!#};name=$(basename "$file");cp "$file" "$FIXTURE/$name"
 sha=$(shasum -a 256 "$file"|awk '{print $1}')
 jq --arg name "$name" --arg digest "sha256:$sha" '.assets += [{id:(100+(.assets|length)),name:$name,digest:$digest}]' "$FIXTURE/release.json" >"$FIXTURE/new"
 mv "$FIXTURE/new" "$FIXTURE/release.json"
elif [[ "$1" == release && "$2" == edit ]];then
 jq -e '.draft==true and (.assets|length)==3' "$FIXTURE/release.json" >/dev/null || exit 94
 jq --argjson immutable "$([[ "$KIND" == mutable-release ]] && printf false || printf true)" '.draft=false|.immutable=$immutable' "$FIXTURE/release.json" >"$FIXTURE/new";mv "$FIXTURE/new" "$FIXTURE/release.json"
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
 jq -e '.draft==false' "$FIXTURE/release.json" >/dev/null || exit 94
 name=${url##*/};cp "$FIXTURE/$name" "$output"
 if [[ "$KIND" == source-corrupt && "$name" == inputs.tar ]];then printf changed >>"$output";fi
elif [[ "$url" == https://ghcr.io/v2/* ]];then
 reads=0;[[ ! -f "$FIXTURE/registry-reads" ]] || read -r reads <"$FIXTURE/registry-reads"
 reads=$((reads+1));printf '%s\n' "$reads" >"$FIXTURE/registry-reads"
 if [[ "$KIND" == registry-transport-error ]];then exit 28;fi
 if [[ "$KIND" == registry-error ]];then printf 503;exit 0;fi
 if [[ "$KIND" == registry-oci-index || "$KIND" == registry-docker-index ]];then
  media_type=application/vnd.oci.image.index.v1+json
  [[ "$KIND" != registry-docker-index ]] || media_type=application/vnd.docker.distribution.manifest.list.v2+json
  if ! grep -Fq "$media_type" "$config";then
    printf 404;printf '{"errors":[{"code":"MANIFEST_UNKNOWN"}]}\n' >"$output";exit 0
  fi
  jq -n --arg media "$media_type" '{schemaVersion:2,mediaType:$media,manifests:[{digest:("sha256:"+("8"*64))}]}' >"$output"
  sha=$(shasum -a 256 "$output"|awk '{print $1}')
  printf 'HTTP/2 200\r\nDocker-Content-Digest: sha256:%s\r\n' "$sha" >"$headers"
  printf 200;exit 0
 fi
 if [[ ! -f "$FIXTURE/pushed" && "$KIND" != registry-conflict && !( "$KIND" == registry-late-conflict && "$reads" -ge 2 ) ]];then printf 404;printf '{}\n' >"$output";exit 0;fi
 cp "$FIXTURE/manifest.json" "$output"
 if [[ "$KIND" == registry-conflict || "$KIND" == registry-late-conflict ]];then jq '.config.digest="sha256:different"' "$output" >"$FIXTURE/new";mv "$FIXTURE/new" "$output";fi
 sha=$(shasum -a 256 "$output"|awk '{print $1}')
 printf 'HTTP/2 200\r\nDocker-Content-Digest: sha256:%s\r\n' "$sha" >"$headers"
 printf 200
elif [[ -n "$config" ]];then
 if [[ "$KIND" == registry-token-error ]];then exit 22;fi
 if [[ "$KIND" == bad-bearer ]];then
  printf '{"token":"safe\\nInjected: value"}\n' >"$output"
 else
  printf '{"token":"AbC+DeF/GhI~jK.lM_nO-pQ=="}\n' >"$output"
 fi
else exit 98;fi
FIXTURE
cat >"$scratch/bin/docker" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >>"$FIXTURE/calls"
case "$1" in
 login) cat >/dev/null;[[ "$KIND" != registry-login-error ]] || exit 1;;tag) :;;push)
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
successful() {
  [[ "$1" == valid || "$1" == draft-resume || "$1" == published-retry || "$1" == paged-fresh || "$1" == paged-resume || "$1" == paged-retry || "$1" == boundary-inventory ]]
}
for kind in valid draft-resume published-retry draft-conflict mutable-release tag-conflict source-corrupt registry-conflict registry-error attestation-conflict bad-retention bad-profile bad-authority bad-bearer paged-fresh paged-resume paged-retry paged-duplicate malformed-inventory failed-inventory oversized-inventory registry-token-error registry-login-error registry-transport-error registry-late-conflict registry-oci-index registry-docker-index boundary-inventory; do
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
  if [[ "$kind" == draft-resume || "$kind" == draft-conflict || "$kind" == paged-resume || "$kind" == paged-duplicate ]]; then
    cp "$native/zitadel-offline-replay/inputs.tar" "$fixture/inputs.tar"
    digest="$input_sha"
    if [[ "$kind" == draft-conflict ]]; then digest="${base_sha}"; fi
    jq -n --arg digest "sha256:$digest" '{id:789,tag_name:"identity-v4.20.0-devantler.1",target_commitish:("a"*40),draft:true,prerelease:false,immutable:false,assets:[{id:100,name:"inputs.tar",digest:$digest}]}' >"$fixture/release.json"
  elif [[ "$kind" == published-retry || "$kind" == paged-retry ]]; then
    cp "$scratch/valid/"{release.json,inputs.tar,base-source.tar,identity-source.json} "$fixture/"
    touch "$fixture/pushed"
  fi
  : >"$fixture/calls"
  if env PATH="$scratch/bin:$PATH" FIXTURE="$fixture" KIND="$kind" RUNNER_TEMP="$fixture/runner" IDENTITY_RELEASE_WORK="$work" IDENTITY_RELEASE_MODE=publish IDENTITY_CI_RUN_ID=123 GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_EVENT_NAME=workflow_dispatch GITHUB_REF=refs/heads/main GITHUB_ACTOR=devantler GH_TOKEN=fixture bash "$root/scripts/publish-identity-release.sh" >"$fixture/stdout" 2>"$fixture/stderr"; then
    successful "$kind" || {
      printf 'publisher accepted %s\n' "$kind" >&2
      exit 1
    }
    jq -e '.sourceOfferPublished==true and .imagePublished==true and .signatureVerified==true and .portalActivated==false' "$fixture/runner/identity-publication.json" >/dev/null
    first_write="$(awk '/^gh release (create|upload|edit) / {print NR;exit}' "$fixture/calls")"
    if [[ -n "$first_write" ]];then
      first_registry="$(awk '/^curl https:\/\/ghcr.io\/v2\// {print NR;exit}' "$fixture/calls")"
      first_login="$(awk '/^docker login / {print NR;exit}' "$fixture/calls")"
      if [[ -z "$first_registry" || -z "$first_login" || "$first_registry" -ge "$first_write" || "$first_login" -ge "$first_write" ]];then
        echo 'source was mutated before registry preflight' >&2;exit 1
      fi
    fi
  else
    if successful "$kind"; then
      cat "$fixture/stderr"
      exit 1
    fi
    test ! -f "$fixture/runner/identity-publication.json"
    if [[ "$kind" == bad-retention || "$kind" == bad-profile || "$kind" == bad-authority ]]; then test ! -s "$fixture/calls"; fi
    if [[ "$kind" == registry-conflict || "$kind" == registry-error || "$kind" == bad-bearer || "$kind" == registry-token-error || "$kind" == registry-login-error || "$kind" == registry-transport-error || "$kind" == registry-oci-index || "$kind" == registry-docker-index ]];then
      if grep -Eq '^gh release (create|upload|edit) ' "$fixture/calls";then echo "registry preflight failure mutated source: $kind" >&2;exit 1;fi
    fi
    if [[ "$kind" == paged-duplicate || "$kind" == malformed-inventory || "$kind" == failed-inventory || "$kind" == oversized-inventory ]]; then
      if grep -Eq '^gh release (create|upload|edit) ' "$fixture/calls";then echo "incomplete or conflicting inventory was mutated: $kind" >&2;exit 1;fi
    fi
    if [[ "$kind" == registry-late-conflict ]];then
      [[ "$(grep -c 'curl https://github.com/' "$fixture/calls")" == 3 ]] || { echo 'late conflict did not follow source readback' >&2;exit 1; }
      jq -e '.draft==false and .immutable==true' "$fixture/release.json" >/dev/null
    fi
    if [[ "$kind" == draft-conflict ]]; then
      if grep -q 'gh release upload\|gh release edit' "$fixture/calls"; then
        printf 'conflicting draft was mutated\n' >&2
        exit 1
      fi
    fi
    if [[ "$kind" != attestation-conflict ]]; then test ! -f "$fixture/pushed"; fi
  fi
  if [[ "$kind" == oversized-inventory ]];then
    if grep -q '^inventory-page 101$' "$fixture/calls";then echo 'release inventory fetched beyond its request bound' >&2;exit 1;fi
    [[ "$(grep -c '^inventory-page ' "$fixture/calls")" == 100 ]] || { echo 'release inventory did not reach its refusal boundary' >&2;exit 1; }
  fi
done
printf 'identity publisher: PASS — actual source-first script and conflicting tag/source/registry/provenance refusals\n'
