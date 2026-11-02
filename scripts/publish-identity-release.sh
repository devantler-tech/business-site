#!/usr/bin/env bash
# Explicit original-byte publication only; no deployment or portal activation.
set -euo pipefail
export LC_ALL=C
umask 077
readonly repo=devantler-tech/business-site tag=identity-v4.20.0-devantler.1
readonly image_name=ghcr.io/devantler-tech/business-site-identity
# fail preserves partial-state failure instead of issuing a publication receipt.
fail() {
  printf 'identity publication: refused %s\n' "$1" >&2
  exit 1
}
[[ "${GITHUB_REPOSITORY:-}" == "$repo" && "${GITHUB_EVENT_NAME:-}" == workflow_dispatch && "${GITHUB_REF:-}" == refs/heads/main && "${GITHUB_SHA:-}" =~ ^[0-9a-f]{40}$ && "${IDENTITY_RELEASE_MODE:-}" == publish ]] || fail 'explicit publishing caller'
work=${IDENTITY_RELEASE_WORK:-}
[[ "$work" == "${RUNNER_TEMP:?}"/identity-forge.* && "$work" != *$'\n'* && -f "$work/saved/saved-image.json" && -f "$work/forge.json" ]] || fail 'fresh repeated admission'
readonly native="$work/restored/native" handoff="$work/restored/native/zitadel-handoff/handoff.json"
jq -e '.schemaVersion==1 and .profile=="identity-saved-image-v1" and .imageRetainedForPublication==true and .imagePublished==false and .portalActivated==false' "$work/saved/saved-image.json" >/dev/null || fail 'saved image has no current publication ownership'
image="$(jq -er '.localImageId|select(test("^sha256:[0-9a-f]{64}$"))' "$work/saved/saved-image.json")" || fail 'saved image config identity'
# Only the positively absent-then-loaded image belongs to this invocation.
local_tag=false
# cleanup removes only the positively owned local image/reference and temporary
# registry request files; any cleanup error prevents a successful exit.
cleanup() {
  result=$?
  if [[ "$local_tag" == true ]]; then
    if ! docker image rm "$image_name:$image_tag" >/dev/null; then result=1; fi
  else
    if ! docker image rm "$image" >/dev/null; then result=1; fi
  fi
  rm -f "$work/registry-auth.config" "$work/registry-read.config" "$work/registry-token.json"
  exit "$result"
}
trap cleanup EXIT
run="$(jq -er .ciRunId "$work/forge.json")"
attempt="$(jq -er .ciRunAttempt "$work/forge.json")"
source="$(jq -er '.sourceSha|select(test("^[0-9a-f]{40}$"))' "$work/forge.json")"
jq -e --arg caller "$GITHUB_SHA" --arg run "$IDENTITY_CI_RUN_ID" '.callerSha==$caller and .ciRunId==$run and .forgeAdmission==true and .imagePublished==false and .portalActivated==false' "$work/forge.json" >/dev/null || fail 'forge admission changed'
[[ "$run" =~ ^[1-9][0-9]{0,14}$ && "$attempt" =~ ^[1-9][0-9]{0,5}$ ]] || fail 'original run binding'
# This upload reference belongs to one original CI candidate, not a shared
# version alias. It is still mutable: GHCR does not make our read/push atomic.
# The supported publication/deployment identity is the verified digest below.
readonly image_tag="original-$run-$attempt-${image#sha256:}"
# Re-read main and the current original attempt immediately before the first write.
gh api --hostname github.com "repos/$repo/commits/main" >"$work/main-write.json"
jq -e --arg source "$GITHUB_SHA" '.sha==$source' "$work/main-write.json" >/dev/null || fail 'main changed before publication'
gh api --hostname github.com "repos/$repo/actions/runs/$run" >"$work/run-write.json"
jq -e --arg source "$source" --argjson attempt "$attempt" '.head_sha==$source and .run_attempt==$attempt and .path==".github/workflows/ci.yaml" and .event=="push" and .head_branch=="main" and .status=="completed" and .conclusion=="success"' "$work/run-write.json" >/dev/null || fail 'original CI changed before publication'
mkdir -m 700 "$work/offer" "$work/public-readback"
cp "$native/zitadel-offline-replay/inputs.tar" "$work/offer/inputs.tar"
cp "$native/zitadel-base-source/base-source.tar" "$work/offer/base-source.tar"
jq -n --slurpfile handoff "$handoff" --slurpfile forge "$work/forge.json" --slurpfile base "$work/base-auth.json" '{schemaVersion:1,profile:"identity-corresponding-source-v1",sourceRepository:"devantler-tech/business-site",sourceSha:$forge[0].sourceSha,ciRunId:$forge[0].ciRunId,ciRunAttempt:$forge[0].ciRunAttempt,artifactSha256:$forge[0].artifactSha256,binarySha256:$handoff[0].originalBinarySha256,localImageId:$handoff[0].localImageId,contextSha256:$handoff[0].contextSha256,sourceManifestSha256:$handoff[0].sourceManifestSha256,inputManifestSha256:$handoff[0].inputManifestSha256,baseManifestSha256:$base[0].baseManifestSha256,reproducible:false,portalActivated:false,archives:[$handoff[0].files[]|select(.path=="zitadel-offline-replay/inputs.tar" or .path=="zitadel-base-source/base-source.tar")|{name:(.path|split("/")[-1]),bytes:.bytes,sha256:.sha256}]}' >"$work/offer/identity-source.json"
jq -e --arg image "$image" '.localImageId==$image and (.binarySha256|test("^[a-f0-9]{64}$")) and (.archives|length)==2' "$work/offer/identity-source.json" >/dev/null || fail 'source offer binding'
for name in inputs.tar base-source.tar; do
  expected="$(jq -er --arg name "$name" '.archives[]|select(.name==$name)|.sha256' "$work/offer/identity-source.json")"
  [[ "$(sha256sum "$work/offer/$name" | cut -d' ' -f1)" == "$expected" ]] || fail 'source offer bytes changed'
done
# Registry credentials and existing-tag conflicts must fail before reserving
# the immutable source version. Keep tokens in private files and stdin only.
# registry_authenticate obtains bounded registry credentials without logging them.
registry_authenticate() {
  local bearer
  [[ "${GITHUB_ACTOR:-}" =~ ^[A-Za-z0-9_-]+$ && "${GH_TOKEN:-}" =~ ^[A-Za-z0-9._-]+$ ]] || fail 'registry credential shape'
  printf 'silent\nshow-error\nurl = "https://ghcr.io/token?service=ghcr.io&scope=repository:devantler-tech/business-site-identity:pull,push"\nheader = "Authorization: Basic %s"\n' "$(printf '%s:%s' "$GITHUB_ACTOR" "$GH_TOKEN" | base64 | tr -d '\n')" >"$work/registry-auth.config"
  chmod 600 "$work/registry-auth.config"
  curl --fail --config "$work/registry-auth.config" --max-time 30 --max-filesize 1048576 --output "$work/registry-token.json" || fail 'registry authentication request'
  # RFC 6750 section 2.1 permits standard and URL-safe base64 characters.
  # Absolute anchors also refuse line breaks, quotes and configuration injection.
  bearer="$(jq -er '.token|select(type=="string" and test("\\A[A-Za-z0-9._~+/-]+=*\\z"))' "$work/registry-token.json")" || fail 'registry bearer token shape'
  printf 'silent\nshow-error\nheader = "Authorization: Bearer %s"\nheader = "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"\n' "$bearer" >"$work/registry-read.config"
  chmod 600 "$work/registry-read.config"
  unset bearer
  printf '%s' "$GH_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin || fail 'registry authentication'
}
# registry_read distinguishes observed absence from transport failure and binds
# a returned manifest body to its config and advertised content digest.
registry_read() {
  status="$(curl --config "$work/registry-read.config" --max-time 60 --max-filesize 8388608 --dump-header "$work/registry-headers" --output "$work/registry-manifest.json" --write-out '%{http_code}' "https://ghcr.io/v2/devantler-tech/business-site-identity/manifests/$1")" || fail 'registry read failed'
  [[ "$status" == 200 || "$status" == 404 ]] || fail 'registry response is not readable or absent'
  if [[ "$status" == 200 ]]; then
    jq -e --arg image "$image" '.schemaVersion==2 and .config.digest==$image and (.layers|length)>0' "$work/registry-manifest.json" >/dev/null || fail 'existing registry image differs'
    digest="$(awk 'tolower($1)=="docker-content-digest:" {gsub("\r", "", $2); print $2}' "$work/registry-headers")"
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ && "sha256:$(sha256sum "$work/registry-manifest.json" | cut -d' ' -f1)" == "$digest" ]] || fail 'registry manifest digest differs'
    if [[ "$1" == sha256:* && "$1" != "$digest" ]]; then fail 'registry digest address differs'; fi
  fi
}
registry_authenticate
registry_read "$image_tag"
[[ "$status" == 404 ]] || fail 'image tag already exists; existing images are never adopted'
# The original image is positively owned by this invocation. Accept only a new
# target-repository digest introduced by its own successful Docker push, not a
# registry manifest that merely claims the same config digest.
# read_local_registry_digests checks one document's cardinality and digest-entry shapes.
read_local_registry_digests() {
  local output="$1"
  docker image inspect --format '{{json .RepoDigests}}' "$image" >"$output.raw" || fail 'original image registry metadata'
  jq -ces 'select(length==1)|.[0]|if .==null then [] else . end|
    select(type=="array" and length<=100 and all(.[];type=="string" and length<=512 and (test("[[:cntrl:]]")|not)))' \
    "$output.raw" >"$output" || fail 'original image registry metadata shape'
}
read_local_registry_digests "$work/local-registry-before.json"
jq -e --arg prefix "$image_name@" 'all(.[];startswith($prefix)|not)' "$work/local-registry-before.json" >/dev/null || fail 'original image already has target registry identity'
# A successful matching-refs list proves tag absence; an arbitrary failed view
# is never interpreted as permission to create or overwrite a ref.
gh api --hostname github.com "repos/$repo/git/matching-refs/tags/$tag" >"$work/tags.json"
jq -e 'type=="array" and length<=1' "$work/tags.json" >/dev/null || fail 'ambiguous source release tag'
if jq -e 'length==1' "$work/tags.json" >/dev/null; then
  jq -e --arg ref "refs/tags/$tag" --arg source "$source" '.[0].ref==$ref and .[0].object.type=="commit" and .[0].object.sha==$source' "$work/tags.json" >/dev/null || fail 'existing source tag differs'
fi
# Drafts need not have a tag ref yet. Read explicit, shape-checked API pages
# until a short page proves completion. Refuse a full hundredth page without
# fetching beyond the request budget; incomplete inventory authorizes no write.
# read_release_inventory reaches a short terminating page within the fixed budget.
read_release_inventory() {
  local output="$1" page=1 count
  : >"$output.pages"
  while [[ "$page" -le 100 ]]; do
    gh api --hostname github.com "repos/$repo/releases?per_page=100&page=$page" >"$output.page" || fail 'release inventory retrieval'
    jq -ces 'select(length==1)|.[0]|
      select(type=="array" and length<=100 and all(.[];type=="object" and (.tag_name|type)=="string"))' \
      "$output.page" >>"$output.pages" || fail 'complete bounded release inventory'
    count="$(jq -er 'length' "$output.page")" || fail 'release inventory page size'
    if [[ "$count" -lt 100 ]]; then
      jq -cs 'add' "$output.pages" >"$output" || fail 'complete bounded release inventory'
      return
    fi
    page=$((page+1))
  done
  fail 'release inventory request bound'
}
read_release_inventory "$work/releases.json"
jq --arg tag "$tag" '[.[]|select(.tag_name==$tag)]' "$work/releases.json" >"$work/matching-releases.json"
jq -e 'length<=1' "$work/matching-releases.json" >/dev/null || fail 'duplicate source release'
if jq -e 'length==0' "$work/matching-releases.json" >/dev/null; then
  cat >"$work/release-notes.md" <<'NOTES'
> 🤖 Generated by the Agentic Engineer

Corresponding source for the original Devantler customer-identity image based on
ZITADEL 4.20.0. The two original source archives and identity-source.json bind the
source and the exact exercised native candidate to one successful original CI.
Follow the retained source build instructions and scoped third-party licenses.
Bit-for-bit reproducibility and portal activation are not claimed.
NOTES
  gh release create "$tag" --repo "$repo" --target "$source" --title 'Customer identity 4.20.0 — corresponding source' --notes-file "$work/release-notes.md" --draft --latest=false
  read_release_inventory "$work/releases-created.json"
  jq -ce --arg tag "$tag" '[.[]|select(.tag_name==$tag)]|select(length==1)|.[0]' "$work/releases-created.json" >"$work/release.json" || fail 'unique created source draft'
else
  jq '.[0]' "$work/matching-releases.json" >"$work/release.json"
fi
jq -e --arg tag "$tag" --arg source "$source" '.tag_name==$tag and .target_commitish==$source and (.draft|type)=="boolean" and .prerelease==false and (.id|type)=="number" and (.assets|type)=="array" and (.assets|length)<=3 and all(.assets[];.name=="inputs.tar" or .name=="base-source.tar" or .name=="identity-source.json")' "$work/release.json" >/dev/null || fail 'source release state'
release_id="$(jq -er '.id|tostring' "$work/release.json")"
[[ "$release_id" =~ ^[1-9][0-9]{0,14}$ ]] || fail 'source release database identity'
for name in inputs.tar base-source.tar identity-source.json; do
  count="$(jq --arg name "$name" '[.assets[]|select(.name==$name)]|length' "$work/release.json")"
  [[ "$count" == 0 || "$count" == 1 ]] || fail 'duplicate source release asset'
  if [[ "$count" == 1 ]]; then
    sha="$(sha256sum "$work/offer/$name" | cut -d' ' -f1)"
    jq -e --arg name "$name" --arg digest "sha256:$sha" '[.assets[]|select(.name==$name)]|.[0].digest==$digest' "$work/release.json" >/dev/null || fail 'existing source asset differs'
  fi
done
for name in inputs.tar base-source.tar identity-source.json; do
  count="$(jq --arg name "$name" '[.assets[]|select(.name==$name)]|length' "$work/release.json")"
  if [[ "$count" == 0 ]]; then
    jq -e '.draft==true' "$work/release.json" >/dev/null || fail 'published source release is incomplete'
    gh release upload "$tag" --repo "$repo" "$work/offer/$name"
  fi
done
gh api --hostname github.com "repos/$repo/releases/$release_id" >"$work/release-staged.json"
jq -e --slurpfile before "$work/release.json" '.id==$before[0].id and .tag_name==$before[0].tag_name and .target_commitish==$before[0].target_commitish and .draft==$before[0].draft and .prerelease==false and (.assets|length)==3 and ([.assets[].name]|sort)==["base-source.tar","identity-source.json","inputs.tar"]' "$work/release-staged.json" >/dev/null || fail 'complete staged source release'
for name in inputs.tar base-source.tar identity-source.json; do
  sha="$(sha256sum "$work/offer/$name" | cut -d' ' -f1)"
  jq -e --arg name "$name" --arg digest "sha256:$sha" '[.assets[]|select(.name==$name)]|length==1 and .[0].digest==$digest' "$work/release-staged.json" >/dev/null || fail 'staged source asset digest'
done
if jq -e '.draft==true' "$work/release-staged.json" >/dev/null; then
  gh release edit "$tag" --repo "$repo" --draft=false --latest=false
fi
gh api --hostname github.com "repos/$repo/releases/$release_id" >"$work/release-readback.json"
jq -e --slurpfile staged "$work/release-staged.json" '.id==$staged[0].id and .tag_name==$staged[0].tag_name and .draft==false and .prerelease==false and .immutable==true and
 ([.assets[]|{id,name,digest,size}]|sort_by(.id))==([$staged[0].assets[]|{id,name,digest,size}]|sort_by(.id))' "$work/release-readback.json" >/dev/null || fail 'sealed immutable source release'
gh api --hostname github.com "repos/$repo/git/matching-refs/tags/$tag" >"$work/sealed-tag.json"
jq -e --arg ref "refs/tags/$tag" --arg source "$source" 'length==1 and .[0].ref==$ref and .[0].object.type=="commit" and .[0].object.sha==$source' "$work/sealed-tag.json" >/dev/null || fail 'sealed source tag changed'
for name in inputs.tar base-source.tar identity-source.json; do
  # Independent complete anonymous readback is mandatory after sealing and
  # before any image push or signature operation.
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --max-redirs 5 --connect-timeout 15 --max-time 600 --max-filesize 1342177280 "https://github.com/$repo/releases/download/$tag/$name" --output "$work/public-readback/$name"
  cmp "$work/offer/$name" "$work/public-readback/$name" || fail 'public corresponding source differs'
done
source_offer="$(sha256sum "$work/offer/identity-source.json" | cut -d' ' -f1)"
# Refresh credentials and refuse a conflict visible after source readback.
# This is not CAS protection against another writer to the same upload reference.
registry_authenticate
registry_read "$image_tag"
[[ "$status" == 404 ]] || fail 'image tag appeared during source publication'
docker tag "$image" "$image_name:$image_tag"
local_tag=true
docker push "$image_name:$image_tag"
read_local_registry_digests "$work/local-registry-after.json"
pushed_digest="$(jq -er --arg prefix "$image_name@" 'map(select(startswith($prefix)))|select(length==1)|.[0]|ltrimstr($prefix)|select(test("^sha256:[0-9a-f]{64}$"))' "$work/local-registry-after.json")" || fail 'unique original push digest'
readonly pushed_digest
registry_read "$image_tag"
[[ "$status" == 200 && "$digest" == "$pushed_digest" ]] || fail 'registry manifest differs from original push'
readonly published="$image_name@$pushed_digest"
identity="https://github.com/$repo/.github/workflows/publish-identity.yaml@refs/heads/main"
annotations=(-a "source=$source" -a "caller=$GITHUB_SHA" -a "ciRunId=$run" -a "ciRunAttempt=$attempt" -a "sourceOfferSha256=$source_offer")
cosign sign --yes "${annotations[@]}" "$published"
cosign verify --certificate-identity "$identity" --certificate-oidc-issuer https://token.actions.githubusercontent.com "${annotations[@]}" "$published" >"$work/image-signature.json"
jq -n --slurpfile forge "$work/forge.json" --slurpfile offer "$work/offer/identity-source.json" --arg image "$image" --arg sourceOfferSha256 "$source_offer" '{schemaVersion:1,profile:"identity-original-publication-v1",sourceSha:$forge[0].sourceSha,callerSha:$forge[0].callerSha,ciRunId:$forge[0].ciRunId,ciRunAttempt:$forge[0].ciRunAttempt,artifactSha256:$forge[0].artifactSha256,sourceOfferSha256:$sourceOfferSha256,sourceOffer:$offer[0],localImageId:$image,portalActivated:false}' >"$work/predicate.json"
readonly predicate_type=https://devantler.tech/attestations/identity-release/v1
cosign attest --yes --type "$predicate_type" --predicate "$work/predicate.json" "$published"
cosign verify-attestation --type "$predicate_type" --certificate-identity "$identity" --certificate-oidc-issuer https://token.actions.githubusercontent.com "$published" >"$work/verified-attestation.jsonl"
jq -se --arg digest "${digest#sha256:}" --slurpfile expected "$work/predicate.json" 'any(.[]; (.payload|@base64d|fromjson) as $statement | any($statement.subject[];.digest.sha256==$digest) and $statement.predicate==$expected[0])' "$work/verified-attestation.jsonl" >/dev/null || fail 'signed original provenance differs'
registry_read "$digest"
[[ "$status" == 200 ]] || fail 'signed registry digest readback'
jq -n --arg source "$source" --arg caller "$GITHUB_SHA" --arg run "$run" --argjson attempt "$attempt" --arg image "$published" --arg offer "https://github.com/$repo/releases/tag/$tag" --arg offer_sha "$source_offer" --arg identity "$identity" '{schemaVersion:1,profile:"identity-original-publication-v1",sourceSha:$source,callerSha:$caller,ciRunId:$run,ciRunAttempt:$attempt,image:$image,sourceOffer:$offer,sourceOfferSha256:$offer_sha,signerIdentity:$identity,sourceOfferPublished:true,imagePublished:true,signatureVerified:true,portalActivated:false}' >"$RUNNER_TEMP/identity-publication.json"
printf 'Matching original source and signed image published and independently verified. Portal remains unactivated.\n'
