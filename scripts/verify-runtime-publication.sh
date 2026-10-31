#!/usr/bin/env bash
# Independently read the published pair; never sign, push, tag or repair it.
set -euo pipefail
export LC_ALL=C
readonly repo=devantler-tech/business-site
readonly image=ghcr.io/devantler-tech/business-site
readonly artifact=$image/manifests
readonly workflow=devantler-tech/.github/.github/workflows/publish-app.yaml@f5d078cc4b04ecb15432ed4e34cba1142a6279d4
# Stop before emitting a receipt; errors describe the rejected boundary, not private input.
fail() {
  printf 'runtime publication: refused %s\n' "$1" >&2
  exit 1
}
[[ "${GITHUB_REPOSITORY:-}" == "$repo" && "${SOURCE_SHA:-}" =~ ^[0-9a-f]{40}$ && "${GITHUB_SHA:-}" == "$SOURCE_SHA" ]] || fail 'source context'
[[ "${VERSION:-}" =~ ^(0|[1-9][0-9]{0,14})\.(0|[1-9][0-9]{0,14})\.(0|[1-9][0-9]{0,14})$ && "${GITHUB_REF:-}" == "refs/tags/v$VERSION" ]] || fail 'version context'
[[ "${IMAGE_DIGEST:-}" =~ ^sha256:[0-9a-f]{64}$ && "${PUBLIC_DIGEST:-}" =~ ^[0-9a-f]{64}$ ]] || fail 'original digests'
[[ "${ORIGINAL_CI_RUN_ID:-}" =~ ^[1-9][0-9]{0,14}$ && "${ORIGINAL_CI_RUN_ATTEMPT:-}" =~ ^[1-9][0-9]{0,5}$ &&
  "${GITHUB_RUN_ID:-}" =~ ^[1-9][0-9]{0,14}$ && "${GITHUB_RUN_ATTEMPT:-}" =~ ^[1-9][0-9]{0,5}$ && -d "${RUNNER_TEMP:-}" ]] || fail 'run identity'
[[ ! -e "$RUNNER_TEMP/runtime-publication.json" && ! -L "$RUNNER_TEMP/runtime-publication.json" ]] || fail 'receipt path already exists'
work="$(mktemp -d "$RUNNER_TEMP/runtime-pair.XXXXXX")"
trap 'rm -rf "$work"' EXIT
# Read one bounded public API object into a caller-owned file; reject JSON streams.
read_release_json() {
  (
    ulimit -f 8192
    gh api --hostname github.com "repos/$repo/$1" >"$2"
  ) 2>/dev/null || fail 'current release or tag read'
  jq -se 'length==1 and (.[0]|type)=="object"' "$2" >/dev/null || fail 'current release or tag document'
}
# Re-read publication status and peel at most five tag objects back to the frozen source.
check_current_release() {
  local oid kind depth=0
  read_release_json "releases/tags/v$VERSION" "$work/release.json"
  jq -e --arg tag "v$VERSION" '.tag_name==$tag and .draft==false and .prerelease==false and
    (.published_at|type)=="string" and (.published_at|length)>0' "$work/release.json" >/dev/null || fail 'release withdrawn or changed'
  read_release_json "git/ref/tags/v$VERSION" "$work/tag.json"
  jq -e --arg ref "refs/tags/v$VERSION" '.ref==$ref' "$work/tag.json" >/dev/null || fail 'release tag ref'
  oid="$(jq -er '.object.sha' "$work/tag.json")"
  kind="$(jq -er '.object.type' "$work/tag.json")"
  while [[ "$kind" == tag && "$depth" -lt 5 ]]; do
    [[ "$oid" =~ ^[0-9a-f]{40}$ ]] || fail 'release tag object identity'
    read_release_json "git/tags/$oid" "$work/tag-object.json"
    jq -e --arg sha "$oid" '.sha==$sha' "$work/tag-object.json" >/dev/null || fail 'release tag object mismatch'
    oid="$(jq -er '.object.sha' "$work/tag-object.json")"
    kind="$(jq -er '.object.type' "$work/tag-object.json")"
    depth=$((depth + 1))
  done
  [[ "$kind" == commit && "$oid" == "$SOURCE_SHA" ]] || fail 'release tag source changed'
}
# Require the original successful CI attempt and this publication attempt to remain current.
check_current_runs() {
  check_current_release
  gh api --hostname github.com "repos/$repo/actions/runs/$ORIGINAL_CI_RUN_ID" >"$work/ci.json" || fail 'original CI read'
  jq -se --arg source "$SOURCE_SHA" --arg run "$ORIGINAL_CI_RUN_ID" --arg attempt "$ORIGINAL_CI_RUN_ATTEMPT" '
    length==1 and (.[0]|.id==($run|tonumber) and .run_attempt==($attempt|tonumber) and
      .head_sha==$source and .head_branch=="main" and .event=="push" and .path==".github/workflows/ci.yaml" and
      .repository.full_name=="devantler-tech/business-site" and .head_repository.full_name=="devantler-tech/business-site" and
      .status=="completed" and .conclusion=="success")
  ' "$work/ci.json" >/dev/null || fail 'original CI changed or failed'
  gh api --hostname github.com "repos/$repo/actions/runs/$GITHUB_RUN_ID" >"$work/publisher.json" || fail 'publication run read'
  jq -se --arg source "$SOURCE_SHA" --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" '
    length==1 and (.[0]|.id==($run|tonumber) and .run_attempt==($attempt|tonumber) and
      .head_sha==$source and .event=="release" and .path==".github/workflows/publish-runtime.yaml" and
      .repository.full_name=="devantler-tech/business-site" and
      ((.status=="in_progress" and .conclusion==null) or (.status=="completed" and .conclusion=="success")))
  ' "$work/publisher.json" >/dev/null || fail 'publication attempt changed or failed'
}
check_current_runs
claims="$(jq -n --arg source "$SOURCE_SHA" --arg version "$VERSION" --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" \
  --arg image "$IMAGE_DIGEST" --arg public "$PUBLIC_DIGEST" --arg ci "$ORIGINAL_CI_RUN_ID" --arg ci_attempt "$ORIGINAL_CI_RUN_ATTEMPT" --arg workflow "$workflow" '{
    "devantler.repository":"devantler-tech/business-site","devantler.source-sha":$source,"devantler.source-ref":("v"+$version),
    "devantler.version":$version,"devantler.run-id":$run,"devantler.run-attempt":$attempt,"devantler.workflow-ref":$workflow,
    "devantler.image-digest":$image,"devantler.public-digest":$public,"devantler.public-profile":"production-template",
    "devantler.ci-run-id":$ci,"devantler.ci-run-attempt":$ci_attempt,
    "devantler.image-producer":"https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main"}')"
annotations=()
while IFS= read -r claim; do annotations+=(-a "$claim"); done < <(jq -r 'to_entries[]|.key+"="+.value' <<<"$claims")
# Authenticate the exact supplied reference with the pinned publisher and complete pair claims.
verify_signature() {
  cosign verify --certificate-identity "https://github.com/$workflow" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com "${annotations[@]}" "$1" >"$2" || fail 'signature or publication claims'
}
# New-bundle output identifies the requested reference, while its digest independently binds content.
# Require every returned signature to agree, rather than selecting a convenient matching entry.
check_signature() {
  jq -se --arg ref "$2" --arg digest "$3" --argjson claims "$claims" '
    length==1 and (.[0]|type=="array" and length>0 and all(.[];
      . as $signature | .critical.identity["docker-reference"]==$ref and .critical.image["docker-manifest-digest"]==$digest and
      all($claims|to_entries[]; $signature.optional[.key]==.value) and
      (.optional["devantler.manifests-digest"]|type)=="string" and
      (.optional["devantler.manifests-digest"]|test("^sha256:[0-9a-f]{64}$"))))
  ' "$1" >/dev/null || fail 'verified subject or paired claim mismatch'
}
verify_signature "$image@$IMAGE_DIGEST" "$work/image.json"
check_signature "$work/image.json" "$image@$IMAGE_DIGEST" "$IMAGE_DIGEST"
manifests_digest="$(jq -er '[.[].optional["devantler.manifests-digest"]]|unique|select(length==1)|.[0]' "$work/image.json")" || fail 'ambiguous signed manifest digest'
claims="$(jq --arg digest "$manifests_digest" '.["devantler.manifests-digest"]=$digest' <<<"$claims")"
annotations+=(-a "devantler.manifests-digest=$manifests_digest")
check_signature "$work/image.json" "$image@$IMAGE_DIGEST" "$IMAGE_DIGEST"
for target in "$artifact@$manifests_digest" "$image:$VERSION" "$artifact:$VERSION"; do
  verify_signature "$target" "$work/subject.json"
  if [[ "$target" == "$image:$VERSION" ]]; then
    check_signature "$work/subject.json" "$target" "$IMAGE_DIGEST"
  else
    check_signature "$work/subject.json" "$target" "$manifests_digest"
  fi
done
# Extract only the already authenticated immutable manifest package, never the raw CI ZIP.
# Flux reads the read-only registry credentials established by the workflow's login action.
mkdir "$work/manifests"
flux pull artifact "oci://$artifact@$manifests_digest" --output "$work/manifests" || fail 'immutable manifest pull'
entries="$(find "$work/manifests" -mindepth 1 -print)" || fail 'manifest inventory'
while IFS= read -r entry; do
  member=${entry#"$work/manifests/"}
  case "$member" in webapp.yaml | network-policy.yaml | disruption-budget.yaml | secret-store.yaml | external-secret-disclosure.yaml | kustomization.yaml | README.md) ;; *) fail 'unexpected manifest member' ;; esac
  [[ -f "$entry" && ! -L "$entry" && "$(wc -c <"$entry")" -le 1048576 ]] || fail 'manifest member bounds'
done <<<"$entries"
for member in webapp.yaml network-policy.yaml disruption-budget.yaml secret-store.yaml external-secret-disclosure.yaml kustomization.yaml; do
  [[ -f "$work/manifests/$member" && -f "deploy/$member" && ! -L "deploy/$member" ]] || fail 'missing source manifest'
  yq -o=json -I=0 '.' "deploy/$member" >"$work/source.json" || fail 'source manifest parse'
  if [[ "$member" == webapp.yaml ]]; then
    jq --arg image "$image@$IMAGE_DIGEST" '.spec.image=$image' "$work/source.json" >"$work/pinned.json"
    mv "$work/pinned.json" "$work/source.json"
  fi
  jq -S -s '.' "$work/source.json" >"$work/expected.json"
  yq -o=json -I=0 '.' "$work/manifests/$member" | jq -S -s '.' >"$work/actual.json" || fail 'published manifest parse'
  cmp -s "$work/expected.json" "$work/actual.json" || fail 'published deployment differs from source'
done
if [[ -f "$work/manifests/README.md" ]]; then
  cmp -s deploy/README.md "$work/manifests/README.md" || fail 'published documentation differs from source'
fi
check_current_runs
jq -n --arg source "$SOURCE_SHA" --arg version "$VERSION" --arg image "$image@$IMAGE_DIGEST" --arg manifests "$artifact@$manifests_digest" \
  --arg run "$GITHUB_RUN_ID" --arg attempt "$GITHUB_RUN_ATTEMPT" --arg ci "$ORIGINAL_CI_RUN_ID" --arg ci_attempt "$ORIGINAL_CI_RUN_ATTEMPT" \
  --arg public "$PUBLIC_DIGEST" --arg identity "https://github.com/$workflow" '{repository:"devantler-tech/business-site",sourceRevision:$source,version:$version,
    image:$image,manifests:$manifests,publicationRunId:$run,publicationRunAttempt:$attempt,ciRunId:$ci,ciRunAttempt:$ci_attempt,
    publicDigest:$public,publicProfile:"production-template",publisherIdentity:$identity}' >"$work/receipt.json"
mv "$work/receipt.json" "$RUNNER_TEMP/runtime-publication.json"
printf 'Signed immutable image/manifest pair, version aliases, source contents and current attempts verified.\n'
