#!/usr/bin/env bash
# Offline, explicit source preparation. No build, image or deployment authority.
set -euo pipefail
export LC_ALL=C
[[ "$#" -gt 0 ]] || exit 0
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
stage="$(mktemp -d "$parent/.zitadel-source.XXXXXX")"
trap 'rm -rf -- "$stage"' EXIT
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
git -C "$source_repo" --no-replace-objects archive --format=tar "$pin" | tar -xf - -C "$stage/source"
(cd "$stage/source" && git hash-object --no-filters --stdin-paths <"$stage/paths") >"$stage/actual-hashes"
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
rm -- "$stage/source/console/src/assets/ailerons.otf" \
  "$stage/source/console/src/styles/ailerons.scss" \
  "$stage/source/internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf" \
  "$stage/source/internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss"
recipe_hash="$(shasum -a 256 "$0" | awk '{print $1}')" || fail 'recipe hash read failed'
input_hash="$(shasum -a 256 "$root/scripts/zitadel-source-inputs.sha256" | awk '{print $1}')" || fail 'input-manifest hash read failed'
[[ "$recipe_hash" =~ ^[0-9a-f]{64}$ && "$input_hash" =~ ^[0-9a-f]{64}$ ]] || fail 'receipt hash is incomplete'
jq -n --arg pin "$pin" --arg recipe "$recipe_hash" --arg inputs "$input_hash" \
  '{schemaVersion:1,upstreamCommit:$pin,recipeSha256:$recipe,inputManifestSha256:$inputs,
    imageAdmission:"unknown",licenseClosure:"unknown",
    removedFiles:["console/src/assets/ailerons.otf","console/src/styles/ailerons.scss",
      "internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf",
      "internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss"],
    changedFiles:["console/src/styles.scss","internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss"]}' >"$stage/source-preparation.json"
# mkdir is the atomic output reservation: a concurrent creator wins, never gets
# overwritten or becomes a directory into which mv silently nests our output.
mkdir -m 700 -- "$output"
mv "$stage/source" "$output/source"
mv "$stage/source-preparation.json" "$output/source-preparation.json"
printf 'Prepared frozen source only: %s\n' "$output"
