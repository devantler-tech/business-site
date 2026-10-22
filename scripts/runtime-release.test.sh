#!/usr/bin/env bash
# Exercise real release admission with the real archive reader and a bounded API fixture.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
fail() {
  printf 'runtime release: FAIL — %s\n' "$*" >&2
  exit 1
}
mkdir -p "$scratch/bin" "$scratch/members" "$scratch/runner"
go -C "$root/server" build -o "$scratch/runner/runtime-receipt" ./cmd/runtime-receipt
readonly source=0123456789abcdef0123456789abcdef01234567
readonly other=1123456789abcdef0123456789abcdef01234567
readonly public=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
readonly image=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
for member in runtime signature provenance attestation; do
  printf '{}\n' >"$scratch/members/$member.json"
done
jq -n --arg source "$source" --arg public "$public" --arg image "$image" '{
  repository:"devantler-tech/business-site",sourceRevision:$source,publicDigest:$public,
  publicProfile:"production-template",image:("ghcr.io/devantler-tech/business-site@"+$image),
  signerIdentity:"https://github.com/devantler-tech/business-site/.github/workflows/ci.yaml@refs/heads/main",
  workflowRevision:$source,ciRunId:"123",ciRunAttempt:"2",publicationRunId:"123",publicationRunAttempt:"2"
}' >"$scratch/members/receipt.json"
(cd "$scratch/members" && zip -q "$scratch/publication.zip" ./*.json)
archive_digest="$(shasum -a 256 "$scratch/publication.zip" | awk '{print $1}')"
readonly archive_digest
jq -n --arg source "$source" '{id:123,run_attempt:2,repository:{full_name:"devantler-tech/business-site"},
  head_repository:{full_name:"devantler-tech/business-site"},head_sha:$source,head_branch:"main",
  path:".github/workflows/ci.yaml",event:"push",status:"completed",conclusion:"success"}' >"$scratch/run-good.json"
jq -n --arg source "$source" --arg digest "$archive_digest" '{id:456,name:"business-site-container-publication",
  expired:false,size_in_bytes:3000,digest:("sha256:"+$digest),workflow_run:{id:123,head_sha:$source,
  head_branch:"main",repository_id:1409101115,head_repository_id:1409101115}}' >"$scratch/artifact-good.json"
cat >"$scratch/bin/gh" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" == 4 && "$1" == api && "$2" == --hostname && "$3" == github.com ]] || exit 97
printf '%s\n' "$4" >> "$FIXTURE/calls"
case "$4" in
  repos/devantler-tech/business-site/actions/runs/123|repos/devantler-tech/business-site/actions/runs/123/attempts/2)
    if [[ -f "$FIXTURE/downloaded" && -f "$FIXTURE/run-after.json" ]]; then
      cat "$FIXTURE/run-after.json"
    else cat "$FIXTURE/run.json"; fi ;;
  'repos/devantler-tech/business-site/actions/runs?per_page=100')
    jq '{total_count:1,workflow_runs:[.]}' "$FIXTURE/run.json" ;;
  'repos/devantler-tech/business-site/actions/runs/123/artifacts?per_page=100')
    jq --argjson copies "${COPIES:-1}" '. as $a | {total_count:$copies,artifacts:[range($copies)|$a]}' "$FIXTURE/artifact.json" ;;
  repos/devantler-tech/business-site/actions/artifacts/456/zip)
    touch "$FIXTURE/downloaded"; cat "$FIXTURE/publication.zip" ;;
  repos/devantler-tech/business-site/actions/artifacts/456) cat "$FIXTURE/artifact.json" ;;
  repos/devantler-tech/business-site/git/ref/tags/v1.2.3)
    jq -n --arg sha "${TAG_SOURCE:-$EXPECTED_SOURCE}" '{ref:"refs/tags/v1.2.3",object:{type:"commit",sha:$sha}}' ;;
  repos/devantler-tech/business-site/releases/789) cat "$FIXTURE/release.json" ;;
  *) exit 98 ;;
esac
FIXTURE
chmod +x "$scratch/bin/gh"
reset_fixture() {
  cp "$scratch/run-good.json" "$scratch/run.json"
  cp "$scratch/artifact-good.json" "$scratch/artifact.json"
  rm -f "$scratch/run-after.json" "$scratch/downloaded"
  printf '' >"$scratch/output"
  printf '' >"$scratch/calls"
  jq -n --arg source "$source" '{action:"completed",workflow_run:{id:123,head_sha:$source,run_attempt:2}}' >"$scratch/event.json"
  jq -n '{id:789,tag_name:"v1.2.3",draft:false,prerelease:false,published_at:"2026-10-22T12:00:00Z"}' >"$scratch/release.json"
}
admit() {
  env PATH="$scratch/bin:$PATH" FIXTURE="$scratch" RUNNER_TEMP="$scratch/runner" \
    GITHUB_EVENT_PATH="$scratch/event.json" GITHUB_OUTPUT="$scratch/output" \
    GITHUB_REPOSITORY=devantler-tech/business-site GITHUB_SHA="$source" \
    GITHUB_REF=refs/heads/main GITHUB_EVENT_NAME=workflow_run \
    RUNTIME_PUBLICATION_ENABLED=true RELEASE_MODE=release EXPECTED_SOURCE="$source" \
    "$@" bash "$root/scripts/admit-runtime-release.sh" >"$scratch/stdout" 2>"$scratch/stderr"
}
refused() {
  if admit "$@"; then fail 'forbidden source/CI/artifact context was admitted'; fi
  [[ ! -s "$scratch/output" ]] || fail 'refusal emitted reusable-job outputs'
}
reset_fixture
admit || fail 'the actual successful original main archive must be admitted'
printf 'source=%s\nci-run-id=123\nci-run-attempt=2\nimage-digest=%s\npublic-digest=%s\nversion=\n' "$source" "$image" "$public" >"$scratch/expected"
cmp "$scratch/expected" "$scratch/output" || fail 'real admitted receipt outputs changed'
[[ "$(grep -c '/zip$' "$scratch/calls")" == 1 ]] || fail 'valid input must download exactly one archive'
for value in '' false TRUE yes; do
  reset_fixture
  refused RUNTIME_PUBLICATION_ENABLED="$value"
  [[ ! -s "$scratch/calls" ]] || fail 'off-state made network requests'
done
for context in GITHUB_REPOSITORY=foreign/site GITHUB_REF=refs/heads/other GITHUB_EVENT_NAME=pull_request GITHUB_SHA=main; do
  reset_fixture
  refused "$context"
  [[ ! -s "$scratch/calls" ]] || fail 'untrusted context made network requests'
done
for mutation in '.action="requested"' '.workflow_run.id=0' '.workflow_run.head_sha="bad"'; do
  reset_fixture
  jq "$mutation" "$scratch/event.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/event.json"
  refused
  [[ ! -f "$scratch/downloaded" ]] || fail 'invalid event downloaded an archive'
done
reset_fixture
jq --arg sha "$other" '.workflow_run.head_sha=$sha' "$scratch/event.json" >"$scratch/changed"
mv "$scratch/changed" "$scratch/event.json"
refused
[[ ! -f "$scratch/downloaded" ]] || fail 'workflow-run source mismatch downloaded an archive'
for mutation in '.repository.full_name="foreign/site"' '.head_repository.full_name="foreign/site"' \
  '.event="pull_request"' '.path="other.yaml"' '.head_branch="other"' '.run_attempt=3' \
  '.conclusion="failure"' '.status="in_progress"' '.id=0' '.head_sha="bad"'; do
  reset_fixture
  jq "$mutation" "$scratch/run.json" >"$scratch/changed"
  mv "$scratch/changed" "$scratch/run.json"
  refused
  [[ ! -f "$scratch/downloaded" ]] || fail 'invalid original CI downloaded an archive'
done
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
admit GITHUB_EVENT_NAME=workflow_dispatch RELEASE_MODE=dry-run RUNTIME_PUBLICATION_ENABLED= || fail 'explicit nonpublishing preflight must work while off'
reset_fixture
jq -n --slurpfile release "$scratch/release.json" '{action:"published",release:$release[0]}' >"$scratch/event.json"
admit GITHUB_EVENT_NAME=release GITHUB_REF=refs/tags/v1.2.3 || fail 'published real version bound to original CI must be admitted'
grep -qx 'version=1.2.3' "$scratch/output" || fail 'release version output is incorrect'
for context in TAG_SOURCE="$other" GITHUB_REF=refs/tags/v01.2.3 RUNTIME_PUBLICATION_ENABLED=false; do
  reset_fixture
  jq -n --slurpfile release "$scratch/release.json" '{action:"published",release:$release[0]}' >"$scratch/event.json"
  refused GITHUB_EVENT_NAME=release GITHUB_REF=refs/tags/v1.2.3 "$context"
done
printf 'runtime release: PASS — real archive admission, API/source/attempt closure, off-state and version refusals\n'
