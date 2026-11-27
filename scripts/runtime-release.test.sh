#!/usr/bin/env bash
# Exercise real release admission with the real archive reader and a bounded API fixture.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
# Report a broken admission boundary, retaining fixture cleanup on every exit.
fail() {
  printf 'runtime release: FAIL — %s\n' "$*" >&2
  exit 1
}
mkdir -p "$scratch/bin" "$scratch/members" "$scratch/runner" "$scratch/source/.github"
go -C "$root/server" build -o "$scratch/runner/runtime-receipt" ./cmd/runtime-receipt
git -C "$scratch/source" init -q
policy_file="$scratch/source/.github/runtime-publication"
# Commit the actual fixture blob so admission can read the frozen Git source.
commit_policy() {
  git -C "$scratch/source" add -- .github/runtime-publication
  git -C "$scratch/source" -c user.name='Runtime Fixture' -c user.email=runtime@example.invalid \
    -c commit.gpgsign=false commit -qm 'Test publication policy'
  git -C "$scratch/source" rev-parse HEAD
}
printf 'enabled\n' >"$policy_file"
approved_source="$(commit_policy)"
printf 'disabled\n' >"$policy_file"
disabled_source="$(commit_policy)"
printf 'enabled\nextra\n' >"$policy_file"
invalid_source="$(commit_policy)"
rm "$policy_file"
ln -s $'enabled\n' "$policy_file"
symlink_source="$(commit_policy)"
rm "$policy_file"
missing_source="$(commit_policy)"
git -C "$scratch/source" checkout -q --detach "$approved_source"
source=$approved_source
readonly other=1123456789abcdef0123456789abcdef01234567
readonly publisher='botantler-1[bot]'
readonly publisher_id=185060876
readonly public=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
readonly image=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
build_original_ci_fixture() {
  local archive_digest member
  for member in runtime signature provenance attestation; do
    printf '{}\n' >"$scratch/members/$member.json"
  done
  jq -n --arg source "$source" --arg public "$public" --arg image "$image" '{
  repository:"devantler-tech/business-site",sourceRevision:$source,publicDigest:$public,
  publicProfile:"production-template",image:("ghcr.io/devantler-tech/business-site@"+$image),
  signerIdentity:"https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main",
  workflowRevision:$source,ciRunId:"123",ciRunAttempt:"2",publicationRunId:"123",publicationRunAttempt:"2"
  }' >"$scratch/members/receipt.json"
  rm -f "$scratch/publication.zip"
  (cd "$scratch/members" && zip -q "$scratch/publication.zip" ./*.json)
  archive_digest="$(shasum -a 256 "$scratch/publication.zip" | awk '{print $1}')"
  jq -n --arg source "$source" '{id:123,run_attempt:2,repository:{full_name:"devantler-tech/business-site"},
  head_repository:{full_name:"devantler-tech/business-site"},head_sha:$source,head_branch:"main",
  path:".github/workflows/ci.yaml",event:"push",status:"completed",conclusion:"success"}' >"$scratch/run-good.json"
  jq -n --arg source "$source" --arg digest "$archive_digest" '{id:456,name:"business-site-container-publication",
  expired:false,size_in_bytes:3000,digest:("sha256:"+$digest),workflow_run:{id:123,head_sha:$source,
  head_branch:"main",repository_id:1409101115,head_repository_id:1409101115}}' >"$scratch/artifact-good.json"
}
build_original_ci_fixture
cat >"$scratch/bin/gh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 4 && "$1" == api && "$2" == --hostname && "$3" == github.com ]] || exit 97
printf '%s\n' "$4" >> "$FIXTURE/calls"
case "$4" in
  "repos/devantler-tech/business-site/commits/$EXPECTED_SOURCE") cat "$FIXTURE/commit.json" ;;
  repos/devantler-tech/business-site/actions/runs/123)
    if [[ -f "$FIXTURE/downloaded" && -f "$FIXTURE/run-after.json" ]]; then
      cat "$FIXTURE/run-after.json"
    else cat "$FIXTURE/run.json"; fi ;;
  repos/devantler-tech/business-site/actions/runs/123/attempts/2|repos/devantler-tech/business-site/actions/runs/123/attempts/3)
    if [[ -f "$FIXTURE/run-attempt.json" ]]; then cat "$FIXTURE/run-attempt.json"; else cat "$FIXTURE/run.json"; fi ;;
  'repos/devantler-tech/business-site/actions/runs?per_page=100')
    if [[ "${BUSY_WORKFLOW_INVENTORY:-false}" == true ]]; then
      jq -n '{total_count:101,workflow_runs:[range(223;123;-1)|{id:.,head_branch:"draft",event:"pull_request",path:".github/workflows/ci.yaml"}]}'
    else
      jq '{total_count:1,workflow_runs:[.]}' "$FIXTURE/run.json"
    fi ;;
  'repos/devantler-tech/business-site/actions/workflows/ci.yaml/runs?branch=main&event=push&per_page=100')
    if [[ -n "${NEWER_MAIN_SOURCE:-}" ]]; then
      jq --arg newer "$NEWER_MAIN_SOURCE" '{total_count:2,workflow_runs:[(.|.id=124|.head_sha=$newer),.]}' "$FIXTURE/run.json"
    else jq '{total_count:1,workflow_runs:[.]}' "$FIXTURE/run.json"; fi ;;
  'repos/devantler-tech/business-site/actions/runs/123/artifacts?per_page=100')
    jq --argjson copies "${COPIES:-1}" '. as $a | {total_count:$copies,artifacts:[range($copies)|$a]}' "$FIXTURE/artifact.json" ;;
  repos/devantler-tech/business-site/actions/artifacts/456/zip)
    touch "$FIXTURE/downloaded"; cat "$FIXTURE/publication.zip" ;;
  repos/devantler-tech/business-site/actions/artifacts/456)
    if [[ -f "$FIXTURE/artifact-after.json" ]]; then cat "$FIXTURE/artifact-after.json"; else cat "$FIXTURE/artifact.json"; fi ;;
  repos/devantler-tech/business-site/git/ref/tags/v1.2.3)
    target=${TAG_SOURCE:-$EXPECTED_SOURCE}; kind=commit
    if [[ -f "$FIXTURE/downloaded" && -n "${TAG_SOURCE_AFTER:-}" ]]; then target=$TAG_SOURCE_AFTER; fi
    if [[ "${ANNOTATED_TAG:-false}" == true ]]; then target=dddddddddddddddddddddddddddddddddddddddd; kind=tag; fi
    jq -n --arg sha "$target" --arg kind "$kind" '{ref:"refs/tags/v1.2.3",object:{type:$kind,sha:$sha}}' ;;
  repos/devantler-tech/business-site/git/tags/dddddddddddddddddddddddddddddddddddddddd)
    target=${TAG_SOURCE:-$EXPECTED_SOURCE}; kind=commit
    if [[ "${TAG_LOOP:-false}" == true ]]; then target=dddddddddddddddddddddddddddddddddddddddd; kind=tag; fi
    jq -n --arg sha "$target" --arg kind "$kind" '{sha:"dddddddddddddddddddddddddddddddddddddddd",object:{type:$kind,sha:$sha}}' ;;
  repos/devantler-tech/business-site/releases/789)
    if [[ -f "$FIXTURE/downloaded" && -f "$FIXTURE/release-after.json" ]]; then
      cat "$FIXTURE/release-after.json"
    else cat "$FIXTURE/release.json"; fi ;;
  *) exit 98 ;;
esac
FIXTURE
chmod +x "$scratch/bin/gh"
# Reset API snapshots, outputs and side-effect probes without rebuilding the actual archive.
reset_fixture() {
  cp "$scratch/run-good.json" "$scratch/run.json"
  cp "$scratch/artifact-good.json" "$scratch/artifact.json"
  rm -f "$scratch/run-after.json" "$scratch/run-attempt.json" "$scratch/artifact-after.json" \
    "$scratch/release-after.json" "$scratch/downloaded"
  printf '' >"$scratch/output"
  printf '' >"$scratch/calls"
  jq -n --arg source "$source" '{inputs:{mode:"release",expected_source:$source}}' >"$scratch/event.json"
  jq -n --arg source "$source" '{sha:$source,commit:{verification:{verified:true}}}' >"$scratch/commit.json"
  jq -n --arg publisher "$publisher" --argjson publisher_id "$publisher_id" '{
    id:789,tag_name:"v1.2.3",draft:false,prerelease:false,published_at:"2026-10-22T12:00:00Z",
    author:{id:$publisher_id,login:$publisher,type:"Bot"}
  }' >"$scratch/release.json"
}
published_event() {
  jq -n --slurpfile release "$scratch/release.json" '{
    action:"published",release:$release[0],sender:$release[0].author
  }' >"$scratch/event.json"
}
release_context=("GITHUB_ACTOR=$publisher" GITHUB_EVENT_NAME=release GITHUB_REF=refs/tags/v1.2.3)
# Invoke production admission and the compiled archive reader with fixture-only public API reads.
admit() (
  cd "$scratch/source"
  env PATH="$scratch/bin:$PATH" FIXTURE="$scratch" RUNNER_TEMP="$scratch/runner" \
    GITHUB_EVENT_PATH="$scratch/event.json" GITHUB_OUTPUT="$scratch/output" \
    GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_SHA="$source" \
    GITHUB_REF=refs/heads/main GITHUB_EVENT_NAME=workflow_dispatch \
    RELEASE_MODE=release RUNTIME_PUBLICATION_ENABLED=true EXPECTED_SOURCE="$source" \
    "$@" bash "$root/scripts/admit-runtime-release.sh" >"$scratch/stdout" 2>"$scratch/stderr"
)
# A rejected context must fail and leave no outputs consumable by a privileged downstream job.
refused() {
  if admit "$@"; then fail 'forbidden source/CI/artifact context was admitted'; fi
  [[ ! -s "$scratch/output" ]] || fail 'refusal emitted reusable-job outputs'
}
for denied_source in "$disabled_source" "$invalid_source" "$symlink_source" "$missing_source"; do
  git -C "$scratch/source" checkout -q --detach "$denied_source"
  source=$denied_source
  reset_fixture
  refused RUNTIME_PUBLICATION_ENABLED=true
  [[ ! -s "$scratch/calls" ]] || fail 'disabled manual release made an API request'
  reset_fixture
  published_event
  refused "${release_context[@]}" RUNTIME_PUBLICATION_ENABLED=true
  [[ ! -s "$scratch/calls" ]] || fail 'disabled App release made an API request'
done
git -C "$scratch/source" checkout -q --detach "$disabled_source"
source=$disabled_source
printf 'enabled\n' >"$policy_file"
reset_fixture
refused RUNTIME_PUBLICATION_ENABLED=true
[[ ! -s "$scratch/calls" ]] || fail 'modified checkout policy made an API request'
git -C "$scratch/source" restore -- .github/runtime-publication
source=$approved_source
reset_fixture
refused RUNTIME_PUBLICATION_ENABLED=true
[[ ! -s "$scratch/calls" ]] || fail 'different checkout made an API request'
git -C "$scratch/source" checkout -q --detach "$approved_source"
for setting in '' false TRUE 1; do
  reset_fixture
  refused RUNTIME_PUBLICATION_ENABLED="$setting"
  [[ ! -s "$scratch/calls" ]] || fail 'repository switch did not stop the approved manual source'
  reset_fixture
  published_event
  refused "${release_context[@]}" RUNTIME_PUBLICATION_ENABLED="$setting"
  [[ ! -s "$scratch/calls" ]] || fail 'repository switch did not stop the approved App source'
done
reset_fixture
admit || fail 'approved source and repository switch must admit the confirmed and verified release'
printf 'source=%s\nci-run-id=123\nci-run-attempt=2\nimage-digest=%s\npublic-digest=%s\nversion=\n' "$source" "$image" "$public" >"$scratch/expected"
cmp "$scratch/expected" "$scratch/output" || fail 'real admitted receipt outputs changed'
[[ "$(grep -c '/zip$' "$scratch/calls")" == 1 ]] || fail 'valid input must download exactly one archive'
reset_fixture
admit BUSY_WORKFLOW_INVENTORY=true || fail 'unrelated workflow runs displaced the original main CI evidence'
cmp "$scratch/expected" "$scratch/output" || fail 'busy repository changed the admitted receipt'
for value in '' "$other" "${source:0:12}" refs/heads/main; do
  reset_fixture
  jq --arg value "$value" '.inputs.expected_source=$value' "$scratch/event.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/event.json"
  refused
  [[ ! -s "$scratch/calls" ]] || fail 'unconfirmed release source made network requests'
done
reset_fixture
jq '.inputs.mode="dry-run"' "$scratch/event.json" >"$scratch/changed"
mv "$scratch/changed" "$scratch/event.json"
refused
[[ ! -s "$scratch/calls" ]] || fail 'inconsistent dispatch mode made network requests'
for mode in '' unknown DRY-RUN; do
  reset_fixture
  refused RELEASE_MODE="$mode"
  [[ ! -s "$scratch/calls" ]] || fail 'invalid explicit release mode made an API request'
done
for context in GITHUB_REPOSITORY=foreign/site GITHUB_REF=refs/heads/other GITHUB_EVENT_NAME=pull_request GITHUB_EVENT_NAME=workflow_run GITHUB_SHA=main; do
  reset_fixture
  refused "$context"
  [[ ! -s "$scratch/calls" ]] || fail 'untrusted context made network requests'
done
for mutation in '.commit.verification.verified=false' '.commit.verification.verified="true"' '.sha="bad"'; do
  reset_fixture
  jq "$mutation" "$scratch/commit.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/commit.json"
  refused
  [[ ! -f "$scratch/downloaded" ]] || fail 'unverified source downloaded an archive'
done
for mutation in '.repository.full_name="foreign/site"' '.head_repository.full_name="foreign/site"' \
  '.event="pull_request"' '.path="other.yaml"' '.head_branch="other"' \
  '.conclusion="failure"' '.status="in_progress"' '.id=0' '.head_sha="bad"'; do
  reset_fixture
  jq "$mutation" "$scratch/run.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/run.json"
  refused
  [[ ! -f "$scratch/downloaded" ]] || fail 'invalid original CI downloaded an archive'
  if grep -q '/attempts/' "$scratch/calls"; then fail 'invalid current CI was forwarded to an attempt request'; fi
done
reset_fixture
jq '.run_attempt=3' "$scratch/run.json" >"$scratch/run-attempt.json"
refused
[[ ! -f "$scratch/downloaded" ]] || fail 'mismatched immutable attempt allowed download'
reset_fixture
jq '.run_attempt=3' "$scratch/run.json" >"$scratch/changed"
mv "$scratch/changed" "$scratch/run.json"
refused
[[ -f "$scratch/downloaded" ]] || fail 'stale receipt control did not exercise the actual archive reader'
for mutation in '.expired=true' '.size_in_bytes=4194305' '.size_in_bytes=0' '.digest="bad"' \
  '.workflow_run.repository_id=1' '.workflow_run.head_repository_id=1' '.workflow_run.id=124' \
  '.workflow_run.head_sha="bad"' '.workflow_run.head_branch="other"'; do
  reset_fixture
  jq "$mutation" "$scratch/artifact.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/artifact.json"
  refused
  [[ ! -f "$scratch/downloaded" ]] || fail 'invalid artifact metadata allowed download'
done
reset_fixture
refused COPIES=2
[[ ! -f "$scratch/downloaded" ]] || fail 'ambiguous archive selection allowed download'
reset_fixture
jq '.run_attempt=3' "$scratch/run.json" >"$scratch/run-after.json"
refused
reset_fixture
jq '.digest="sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"' "$scratch/artifact.json" >"$scratch/artifact-after.json"
refused
git -C "$scratch/source" checkout -q --detach "$disabled_source"
source=$disabled_source
build_original_ci_fixture
reset_fixture
printf '{"inputs":{"mode":"dry-run"}}\n' >"$scratch/event.json"
admit RELEASE_MODE=dry-run RUNTIME_PUBLICATION_ENABLED=false || fail 'nonpublishing preflight must work while publication is disabled'
git -C "$scratch/source" checkout -q --detach "$approved_source"
source=$approved_source
build_original_ci_fixture
reset_fixture
published_event
admit "${release_context[@]}" NEWER_MAIN_SOURCE="$other" || fail 'published App release must retain original CI after main advances'
grep -qx 'version=1.2.3' "$scratch/output" || fail 'release version output is incorrect'
reset_fixture
published_event
admit "${release_context[@]}" GITHUB_ACTOR=fictional-maintainer || fail 'ambient actor name overrode the release event identity'
for mutation in '.sender.id=9001' '.sender.login="another-app[bot]"' '.release.author.id=9001'; do
  reset_fixture
  published_event
  jq "$mutation" "$scratch/event.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/event.json"
  refused "${release_context[@]}"
  [[ ! -s "$scratch/calls" ]] || fail 'untrusted release event made network requests'
done
reset_fixture
published_event
jq '.author.id=9001' "$scratch/release.json" >"$scratch/changed"
mv "$scratch/changed" "$scratch/release.json"
refused "${release_context[@]}"
[[ ! -f "$scratch/downloaded" ]] || fail 'untrusted release author allowed download'
reset_fixture
published_event
jq '.author.id=9001' "$scratch/release.json" >"$scratch/release-after.json"
refused "${release_context[@]}"
[[ -f "$scratch/downloaded" ]] || fail 'changed publisher did not exercise post-download readback'
reset_fixture
published_event
admit "${release_context[@]}" ANNOTATED_TAG=true || fail 'correctly peeled annotated tag was refused'
reset_fixture
published_event
refused "${release_context[@]}" TAG_SOURCE_AFTER="$other"
[[ -f "$scratch/downloaded" ]] || fail 'moving-tag control did not reach the post-download readback'
reset_fixture
published_event
refused "${release_context[@]}" ANNOTATED_TAG=true TAG_LOOP=true
[[ ! -f "$scratch/downloaded" && "$(grep -c '/git/tags/' "$scratch/calls")" == 5 ]] || fail 'recursive tag lookup was not bounded before download'
for context in TAG_SOURCE="$other" GITHUB_REF=refs/tags/v01.2.3; do
  reset_fixture
  published_event
  refused "${release_context[@]}" "$context"
done
printf 'runtime release: PASS — source confirmation, publisher identity, and original CI archive admission\n'
