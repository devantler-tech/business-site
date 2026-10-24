#!/usr/bin/env bash
# Offline, explicit source preparation. No build, image or deployment authority.
set -euo pipefail
export LC_ALL=C
# Exported source has committed public file modes inside a private reservation.
umask 022
# No implicit promisor fetch or configured Git transport belongs to this offline operation.
export GIT_NO_LAZY_FETCH=1 GIT_ALLOW_PROTOCOL=''
[[ "$#" -gt 0 ]] || exit 0
# Print a source-preparation diagnostic and terminate the explicit operation.
fail() { printf 'zitadel-source: %s\n' "$1" >&2; exit 1; }
source_repo='' output='' requested=0
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --prepare) [[ "$requested" -eq 0 ]] || fail 'duplicate opt-in'; requested=1; shift ;;
    --source) [[ -z "$source_repo" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid source'; source_repo="$2"; shift 2 ;;
    --output) [[ -z "$output" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid output'; output="$2"; shift 2 ;;
    *) fail 'expected --prepare --source <repository> --output <new-directory>' ;;
  esac
done
[[ "$requested" -eq 1 && -n "$source_repo" && -n "$output" ]] || fail 'explicit preparation, source and output are required'
[[ -d "$source_repo" ]] || fail 'source repository is absent'
[[ ! -e "$output" && ! -L "$output" ]] || fail 'output already exists'
for tool in git tar shasum jq awk cmp find sort grep sed mktemp mkdir mv rm basename dirname; do
  command -v "$tool" >/dev/null || fail "missing tool: $tool"
done
pin=8afa5dd37a8e723efb9eace8d09fd5c1cccef694
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
source_repo="$(cd "$source_repo" && pwd -P)"
[[ "$(git -C "$source_repo" rev-parse --show-toplevel)" = "$source_repo" ]] || fail 'source must be a repository root'
[[ "$(git -C "$source_repo" --no-replace-objects rev-parse --verify "$pin^{commit}")" = "$pin" ]] || fail 'frozen upstream commit is unavailable'
parent="$(cd "$(dirname "$output")" && pwd -P)"
name="$(basename "$output")"
[[ "$name" != . && "$name" != .. && "$name" != / ]] || fail 'invalid output name'
output="$parent/$name"
[[ ! -e "$output" && ! -L "$output" ]] || fail 'output already exists'
[[ "$output" != "$source_repo/"* ]] || fail 'output must be outside the input repository'
stage="$(mktemp -d "$parent/.zitadel-source.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
trap 'exit 1' HUP INT TERM
mkdir "$stage/source"
# Admit only regular committed files before extraction; never working-copy bytes,
# symlinks, submodules, LFS downloads, hooks or generated files from the input.
git -C "$source_repo" --no-replace-objects -c core.quotePath=false ls-tree -r "$pin" >"$stage/tree"
test -s "$stage/tree"
awk '$2 != "blob" || ($1 != "100644" && $1 != "100755") {bad=1} END {exit bad}' "$stage/tree" || fail 'unsupported upstream entry type'
awk '{print $3}' "$stage/tree" >"$stage/expected-hashes"
awk '{print substr($0,index($0,"\t")+1)}' "$stage/tree" >"$stage/paths"
# Git quotes control-character paths; do not interpret or silently omit one.
if grep -q '^"' "$stage/paths"; then fail 'unsupported quoted source path'; fi
git -C "$source_repo" --no-replace-objects -c tar.umask=0022 archive --format=tar "$pin" | tar -xpf - -C "$stage/source"
# Absolute paths cannot be reinterpreted relative to an enclosing Git worktree.
awk -v prefix="$stage/source/" '{print prefix $0}' "$stage/paths" >"$stage/absolute-paths"
git -C "$source_repo" --no-replace-objects hash-object --no-filters --stdin-paths <"$stage/absolute-paths" >"$stage/actual-hashes"
cmp "$stage/expected-hashes" "$stage/actual-hashes" || fail 'archive contents differ from committed source'
(cd "$stage/source" && find . -type f | sed 's@^./@@' | sort) >"$stage/actual-paths"
sort "$stage/paths" >"$stage/expected-paths"
cmp "$stage/expected-paths" "$stage/actual-paths" || fail 'archive has missing or additional files'
(cd "$stage/source" && shasum -a 256 -c "$root/scripts/zitadel-source-inputs.sha256") >/dev/null
console=console/src/styles.scss
login=internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss
sed "/^@import '\.\/styles\/ailerons\.scss';$/d" "$stage/source/$console" >"$stage/console"
sed "/^@import 'faces\/ailerons_font_faces';$/d" "$stage/source/$login" >"$stage/login"
mv "$stage/console" "$stage/source/$console"
mv "$stage/login" "$stage/source/$login"
# This hash-bound profile has no JSONPath metadata selector. Preserve host/IP
# identification and the exact raw-response path; configured selectors refuse.
metadata=internal/id/sonyflake.go
awk '
  $0 == "\t\"encoding/json\"" || $0 == "\t\"github.com/jarcoal/jpath\"" { next }
  /^func machineID\(\)/ {
    machine++
    print
    print "\tif GeneratorConfig != nil && GeneratorConfig.Identification.Webhook.JPath != nil {"
    print "\t\treturn 0, fmt.Errorf(\"metadata JSONPath is unsupported by this source profile\")"
    print "\t}"
    next
  }
  /^func extractMetadataResponse\(/ {
    selected++
    print "func extractMetadataResponse(path *string, data []byte) ([]byte, error) {"
    print "\tif path != nil {"
    print "\t\treturn nil, fmt.Errorf(\"metadata JSONPath is unsupported by this source profile\")"
    print "\t}"
    print "\treturn data, nil"
    print "}"
  }
  !selected { print }
  END { if (machine != 1 || selected != 1) exit 1 }
' "$stage/source/$metadata" >"$stage/metadata"
metadata_hash="$(shasum -a 256 "$stage/metadata" | awk '{print $1}')" || fail 'metadata transform hash failed'
[[ "$metadata_hash" = 74ad3d761a3101ec720429c664e55cf3083ad17903b762139717406114c62adc ]] || fail 'metadata transform differs'
mv "$stage/metadata" "$stage/source/$metadata"
rm -- "$stage/source/console/src/assets/ailerons.otf" \
  "$stage/source/console/src/styles/ailerons.scss" \
  "$stage/source/internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf" \
  "$stage/source/internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss"
# Check every surviving regular-file mode, not just bytes or executable examples.
(cd "$stage/source" && find . -type f ! -perm 0644 ! -perm 0755 -print) >"$stage/invalid-modes"
[[ ! -s "$stage/invalid-modes" ]] || fail 'source has unexpected file permissions'
awk '$1 == "100755" {print substr($0,index($0,"\t")+1)}' "$stage/tree" | sort >"$stage/expected-executables"
(cd "$stage/source" && find . -type f -perm 0755 | sed 's@^./@@' | sort) >"$stage/actual-executables"
cmp "$stage/expected-executables" "$stage/actual-executables" || fail 'source executable modes differ from committed source'
recipe_hash="$(shasum -a 256 "$0" | awk '{print $1}')" || fail 'recipe hash read failed'
input_hash="$(shasum -a 256 "$root/scripts/zitadel-source-inputs.sha256" | awk '{print $1}')" || fail 'input-manifest hash read failed'
[[ "$recipe_hash" =~ ^[0-9a-f]{64}$ && "$input_hash" =~ ^[0-9a-f]{64}$ ]] || fail 'receipt hash is incomplete'
jq -n --arg pin "$pin" --arg recipe "$recipe_hash" --arg inputs "$input_hash" \
  '{schemaVersion:1,upstreamCommit:$pin,recipeSha256:$recipe,inputManifestSha256:$inputs,
    imageAdmission:"unknown",licenseClosure:"unknown",metadataProfile:"machine-id-no-jsonpath-v1",
    removedFiles:["console/src/assets/ailerons.otf","console/src/styles/ailerons.scss",
      "internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf",
      "internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss"],
    changedFiles:["console/src/styles.scss","internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss","internal/id/sonyflake.go"]}' >"$stage/source-preparation.json"
# mkdir is the atomic output reservation: a concurrent creator wins, never gets
# overwritten or becomes a directory into which mv silently nests our output.
reserved=0 complete=0 interrupted=0
# Arm ownership-aware cleanup before mkdir. Defer catchable termination until its
# result is recorded, closing the mkdir-to-flag window without deleting a rival.
trap 'if [[ "$reserved" -eq 1 && "$complete" -eq 0 ]]; then rm -rf -- "$output"; fi; rm -rf -- "$stage"' EXIT
trap 'interrupted=1' HUP INT TERM
mkdir -m 700 -- "$output"
reserved=1
trap 'exit 1' HUP INT TERM
[[ "$interrupted" -eq 0 ]] || fail 'interrupted during output reservation'
mv "$stage/source" "$output/source"
mv "$stage/source-preparation.json" "$output/source-preparation.json"
complete=1
printf 'Prepared frozen source only: %s\n' "$output"
