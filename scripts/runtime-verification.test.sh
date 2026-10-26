#!/usr/bin/env bash
# Test our verifier boundaries; registry and public API calls are external fixtures.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
# Report the broken verifier boundary; the EXIT trap removes only this fixture's scratch tree.
fail() {
  printf 'runtime verification: FAIL — %s\n' "$*" >&2
  exit 1
}
mkdir -p "$scratch/bin" "$scratch/runner" "$scratch/source/deploy" "$scratch/source/.runtime-publisher/.github/scripts"
cp "$root"/deploy/*.yaml "$scratch/source/deploy/"
readonly source=0123456789abcdef0123456789abcdef01234567
readonly image=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
readonly manifests=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
readonly public=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
readonly workflow=devantler-tech/.github/.github/workflows/publish-app.yaml@f5d078cc4b04ecb15432ed4e34cba1142a6279d4
cat >"$scratch/source/.runtime-publisher/.github/scripts/prepare-prebuilt-webapp.sh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$REPOSITORY" == devantler-tech/business-site && "$SOURCE_SHA" == 0123456789abcdef0123456789abcdef01234567 &&
   "$APP_NAME" == business-site && "$DEPLOY_PATH" == deploy && "$PREBUILT_IMAGE_DIGEST" == "sha256:$(printf 'b%.0s' {1..64})" &&
   "$PREBUILT_PUBLIC_DIGEST" == "$(printf 'a%.0s' {1..64})" && "$PREBUILT_CI_RUN_ID" == 123 && "$PREBUILT_CI_RUN_ATTEMPT" == 2 ]]
[[ "${ORIGIN_FAIL:-false}" == false ]] || exit 1
PREBUILT_IMAGE="ghcr.io/devantler-tech/business-site@$PREBUILT_IMAGE_DIGEST" yq -i '.spec.image=strenv(PREBUILT_IMAGE)' deploy/webapp.yaml
printf 'digest=%s\n' "$PREBUILT_IMAGE_DIGEST" >> "$GITHUB_OUTPUT"
FIXTURE
cat >"$scratch/bin/gh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 4 && "$1" == api && "$2" == --hostname && "$3" == github.com ]] || exit 97
case "$4" in
  repos/devantler-tech/business-site/actions/runs/123)
    jq -n --arg source "$SOURCE_SHA" --argjson attempt "${CI_ATTEMPT:-2}" '{id:123,run_attempt:$attempt,event:"push",head_sha:$source,head_branch:"main",path:".github/workflows/ci.yaml",status:"completed",conclusion:"success",repository:{full_name:"devantler-tech/business-site"},head_repository:{full_name:"devantler-tech/business-site"}}' ;;
  repos/devantler-tech/business-site/actions/runs/999)
    jq -n --arg source "$SOURCE_SHA" --argjson attempt "${PUBLISH_ATTEMPT:-1}" '{id:999,run_attempt:$attempt,event:"release",head_sha:$source,path:".github/workflows/publish-runtime.yaml",status:"in_progress",conclusion:null,repository:{full_name:"devantler-tech/business-site"}}' ;;
  repos/devantler-tech/business-site/releases/tags/v1.2.3)
    printf 'read\n' >> "$FIXTURE/release-reads"
    draft=${RELEASE_DRAFT:-false}
    if [[ "${WITHDRAW_AFTER_VERIFY:-false}" == true && -s "$FIXTURE/calls" ]]; then draft=true; fi
    jq -n --argjson draft "$draft" --argjson prerelease "${RELEASE_PRERELEASE:-false}" --arg published "${PUBLISHED_AT-2026-10-22T12:00:00Z}" '{tag_name:"v1.2.3",draft:$draft,prerelease:$prerelease,published_at:$published}' ;;
  repos/devantler-tech/business-site/git/ref/tags/v1.2.3)
    printf 'read\n' >> "$FIXTURE/tag-reads"
    source=${TAG_SOURCE:-$SOURCE_SHA}
    if [[ "${MOVE_AFTER_VERIFY:-false}" == true && -s "$FIXTURE/calls" ]]; then source=1123456789abcdef0123456789abcdef01234567; fi
    jq -n --arg source "$source" --arg kind "${TAG_KIND:-commit}" '{ref:"refs/tags/v1.2.3",object:{sha:$source,type:$kind}}' ;;
  repos/devantler-tech/business-site/git/tags/0123456789abcdef0123456789abcdef01234567)
    jq -n --arg source "$SOURCE_SHA" --arg kind "${TAG_TARGET_KIND:-commit}" '{sha:$source,object:{sha:$source,type:$kind}}' ;;
  *) exit 98 ;;
esac
FIXTURE
cat >"$scratch/bin/cosign" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == verify && "$2" == --certificate-identity && "$3" == "https://github.com/$EXPECTED_WORKFLOW" &&
   "$4" == --certificate-oidc-issuer && "$5" == https://token.actions.githubusercontent.com ]] || exit 97
shift 5
subject=''
while (( $# > 0 )); do
  if [[ "$1" == -a && "$#" -ge 2 ]]; then shift 2; else subject=$1; shift; fi
done
printf '%s\n' "$subject" >> "$FIXTURE/calls"
case "$subject" in
  ghcr.io/devantler-tech/business-site@*) cat "$FIXTURE/image-signature.json" ;;
  ghcr.io/devantler-tech/business-site:1.2.3) cat "$FIXTURE/image-tag.json" ;;
  ghcr.io/devantler-tech/business-site/manifests@*) cat "$FIXTURE/manifests-signature.json" ;;
  ghcr.io/devantler-tech/business-site/manifests:1.2.3) cat "$FIXTURE/manifests-tag.json" ;;
  *) exit 98 ;;
esac
[[ "${VERIFY_FAIL:-false}" == false ]] || exit 1
FIXTURE
cat >"$scratch/bin/flux" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 5 && "$1" == pull && "$2" == artifact && "$3" == "oci://ghcr.io/devantler-tech/business-site/manifests@$MANIFESTS_DIGEST" && "$4" == --output ]] || exit 97
# Flux v2.9.6 requires the caller to create its extraction directory.
[[ -d "$5" && ! -L "$5" ]] || { printf 'invalid output path: directory must already exist\n' >&2; exit 1; }
cp "$FIXTURE/source/deploy/"*.yaml "$5/"
IMAGE="ghcr.io/devantler-tech/business-site@$IMAGE_DIGEST" yq -i '.spec.image=strenv(IMAGE)' "$5/webapp.yaml"
[[ "${PULL_FAIL:-false}" == false ]] || exit 1
if [[ "${ALTER_MANIFEST:-false}" == true ]]; then yq -i '.spec.host="unapproved.example.invalid"' "$5/webapp.yaml"; fi
if [[ "${EXTRA_MEMBER:-false}" == true ]]; then printf 'unapproved\n' > "$5/extra.txt"; fi
if [[ "${MISSING_STORE:-false}" == true ]]; then rm "$5/secret-store.yaml"; fi
if [[ "${MISSING_DISCLOSURE:-false}" == true ]]; then rm "$5/external-secret-disclosure.yaml"; fi
if [[ "${ALTER_STORE:-false}" == true ]]; then yq -i '.spec.provider.vault.auth.kubernetes.role="foreign-tenant"' "$5/secret-store.yaml"; fi
if [[ "${ALTER_DISCLOSURE:-false}" == true ]]; then yq -i '.spec.data[0].remoteRef.property="unapproved"' "$5/external-secret-disclosure.yaml"; fi
FIXTURE
chmod +x "$scratch/bin/gh" "$scratch/bin/cosign" "$scratch/bin/flux"
# Restore independent signed-subject fixtures and clear receipts/read counters between controls.
reset_fixture() {
  rm -f "$scratch/runner/runtime-publication.json"
  printf '' >"$scratch/calls"
  printf '' >"$scratch/release-reads"
  printf '' >"$scratch/tag-reads"
  jq -n --arg sha "$source" --arg image "$image" --arg manifests "$manifests" --arg public "$public" --arg workflow "$workflow" '[{
    critical:{identity:{"docker-reference":("ghcr.io/devantler-tech/business-site@"+$image)},image:{"docker-manifest-digest":$image}},
    optional:{"devantler.repository":"devantler-tech/business-site","devantler.source-sha":$sha,"devantler.source-ref":"v1.2.3","devantler.version":"1.2.3",
      "devantler.run-id":"999","devantler.run-attempt":"1","devantler.workflow-ref":$workflow,"devantler.image-digest":$image,"devantler.manifests-digest":$manifests,
      "devantler.public-digest":$public,"devantler.public-profile":"production-template","devantler.ci-run-id":"123","devantler.ci-run-attempt":"2",
      "devantler.image-producer":"https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main"}}]' >"$scratch/image-signature.json"
  # Cosign v3.0.6's new-bundle transformOutput uses the exact verified reference,
  # including the tag; the independently authenticated manifest digest stays fixed.
  jq '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site:1.2.3"' "$scratch/image-signature.json" >"$scratch/image-tag.json"
  jq --arg digest "$manifests" '.[] .critical |= {identity:{"docker-reference":("ghcr.io/devantler-tech/business-site/manifests@"+$digest)},image:{"docker-manifest-digest":$digest}}' "$scratch/image-signature.json" >"$scratch/manifests-signature.json"
  jq '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site/manifests:1.2.3"' "$scratch/manifests-signature.json" >"$scratch/manifests-tag.json"
}
# Run the real final verifier with only external registry/API operations replaced at PATH.
verify() {
  (cd "$scratch/source" && env PATH="$scratch/bin:$PATH" FIXTURE="$scratch" RUNNER_TEMP="$scratch/runner" \
    GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_SHA="$source" GITHUB_REF=refs/tags/v1.2.3 \
    GITHUB_RUN_ID=999 GITHUB_RUN_ATTEMPT=1 SOURCE_SHA="$source" VERSION=1.2.3 IMAGE_DIGEST="$image" \
    MANIFESTS_DIGEST="$manifests" PUBLIC_DIGEST="$public" ORIGINAL_CI_RUN_ID=123 ORIGINAL_CI_RUN_ATTEMPT=2 \
    EXPECTED_WORKFLOW="$workflow" "$@" bash "$root/scripts/verify-runtime-publication.sh") >"$scratch/stdout" 2>"$scratch/stderr"
}
# Run the real origin adapter against an isolated deployment and an explicit verifier outcome.
origin() {
  (cd "$scratch/source" && env RUNNER_TEMP="$scratch/runner" SOURCE_SHA="$source" IMAGE_DIGEST="$image" \
    PUBLIC_DIGEST="$public" ORIGINAL_CI_RUN_ID=123 ORIGINAL_CI_RUN_ATTEMPT=2 "$@" \
    bash "$root/scripts/verify-runtime-origin.sh") >"$scratch/stdout" 2>"$scratch/stderr"
}
cp "$scratch/source/deploy/webapp.yaml" "$scratch/original.yaml"
origin || fail 'original-image verification did not complete'
cmp "$scratch/original.yaml" "$scratch/source/deploy/webapp.yaml" || fail 'original verifier mutated the source deployment'
if origin ORIGIN_FAIL=true; then fail 'original cryptographic verifier failure was suppressed'; fi
reset_fixture
verify || fail 'the independently signed matching immutable subjects and version aliases must verify'
[[ "$(wc -l <"$scratch/calls" | tr -d ' ')" == 4 ]] || fail 'both immutable subjects and both version aliases must be read'
[[ "$(wc -l <"$scratch/release-reads" | tr -d ' ')" == 2 && "$(wc -l <"$scratch/tag-reads" | tr -d ' ')" == 2 ]] || fail 'current release and tag must be checked before and after registry reads'
jq -e --arg source "$source" --arg image "ghcr.io/devantler-tech/business-site@$image" --arg manifests "ghcr.io/devantler-tech/business-site/manifests@$manifests" '
  .sourceRevision==$source and .image==$image and .manifests==$manifests and .version=="1.2.3" and
  .publicationRunId=="999" and .publicationRunAttempt=="1" and .ciRunId=="123" and .ciRunAttempt=="2"
' "$scratch/runner/runtime-publication.json" >/dev/null || fail 'verified receipt does not identify the pair'
reset_fixture
if verify TAG_SOURCE=1123456789abcdef0123456789abcdef01234567; then fail 'a moved Git tag with otherwise valid signatures was accepted'; fi
[[ ! -e "$scratch/runner/runtime-publication.json" ]] || fail 'moved Git tag left a receipt'
reset_fixture
verify TAG_KIND=tag || fail 'a matching annotated release tag must verify'
for context in VERIFY_FAIL=true PULL_FAIL=true CI_ATTEMPT=3 PUBLISH_ATTEMPT=2 ALTER_MANIFEST=true EXTRA_MEMBER=true MISSING_STORE=true MISSING_DISCLOSURE=true ALTER_STORE=true ALTER_DISCLOSURE=true VERSION=01.2.3 RELEASE_DRAFT=true RELEASE_PRERELEASE=true PUBLISHED_AT= WITHDRAW_AFTER_VERIFY=true MOVE_AFTER_VERIFY=true; do
  reset_fixture
  if verify "$context"; then fail 'failed signature/pull, stale attempt or substituted manifests were accepted'; fi
  [[ ! -e "$scratch/runner/runtime-publication.json" ]] || fail 'refusal left a successful publication receipt'
done
reset_fixture
if verify TAG_KIND=tag TAG_TARGET_KIND=tag; then fail 'an unbounded annotated-tag chain was accepted'; fi
[[ ! -e "$scratch/runner/runtime-publication.json" ]] || fail 'recursive tag left a receipt'
for target in image-signature image-tag manifests-signature manifests-tag; do
  for mutation in '.[0].optional["devantler.source-sha"]="wrong"' \
    '.[0].critical.identity["docker-reference"]="ghcr.io/foreign/site:1.2.3"' \
    '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site:1.2.4"' \
    '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site/manifests:1.2.4"' \
    '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site"' \
    '.[0].critical.identity["docker-reference"]="ghcr.io/devantler-tech/business-site/manifests"' \
    '.[0].optional["devantler.ci-run-attempt"]="1"' \
    '.[0].critical.image["docker-manifest-digest"]="sha256:wrong"' \
    '.[0].critical.image["docker-manifest-digest"]="sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"' \
    '.[0].optional["devantler.manifests-digest"]="sha256:wrong"' \
    '.[0].optional["devantler.manifests-digest"]="sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"'; do
    reset_fixture
    jq "$mutation" "$scratch/$target.json" >"$scratch/changed"
    mv "$scratch/changed" "$scratch/$target.json"
    if verify; then fail 'mismatched signature subject or paired claims were accepted'; fi
    [[ ! -e "$scratch/runner/runtime-publication.json" ]] || fail 'invalid signature left a receipt'
  done
done
printf 'runtime verification: PASS — origin failure, source immutability, both immutable/tag subjects, paired claims, rendered source closure and current release/tag checkpoints\n'
