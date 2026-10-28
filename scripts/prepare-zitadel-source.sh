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
for tool in git tar shasum jq awk cmp find sort grep sed mktemp mkdir mv rm basename dirname go; do
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
# Native provider decoration reuses the already-admitted arrow fonts. The exact
# closed transform runs only inside this fresh exported stage, before generation.
GOTOOLCHAIN=local go run "$root/scripts/zitadel-login-icons.go" "$root/scripts/zitadel-text-fonts.go" "$root/scripts/zitadel-copied-assets.go" \
  --neutral-provider-icons --prepare --source "$stage/source" >"$stage/provider-icons.json"
jq -e '.profile == "native-neutral-provider-icons-v1" and .changedFiles == 18 and .removedFiles == 16 and .consoleSlots == 27 and .loginSlots == 2' "$stage/provider-icons.json" >/dev/null
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
# The patched CSRF library requires an explicit marker for the already
# configured plaintext development mode. TLS-facing requests retain its default.
login_handler=internal/api/ui/login/login.go
awk '
  $0 == "\t\t\tcsrf.Protect(csrfCookieKey," {
    print "\t\t\tif !externalSecure {"
    print "\t\t\t\tr = csrf.PlaintextHTTPRequest(r)"
    print "\t\t\t}"
    changed++
  }
  { print }
  END { if (changed != 1) exit 1 }
' "$stage/source/$login_handler" >"$stage/login-handler" || fail 'login transport patch differs'
[[ "$(shasum -a 256 "$stage/login-handler" | awk '{print $1}')" = 28da3dd3fd55002ea3239495cba504be6a355e9eed74133327aa564e634e812e ]] || fail 'login transport output differs'
mv "$stage/login-handler" "$stage/source/$login_handler"
# Apply the exact native Go 1.26.10 metadata for the selected security patches.
# Keep all other selections and original checksums.
awk '
  $0 == "\tgithub.com/go-jose/go-jose/v4 v4.1.4" {
    print "\tgithub.com/go-jose/go-jose/v4 v4.1.6"; jose++; next
  }
  $0 == "\tgolang.org/x/net v0.61.0" {
    print "\tgolang.org/x/net v0.62.0"; net++; next
  }
  $0 == "\tgithub.com/gorilla/csrf v1.7.2" {
    print "\tgithub.com/gorilla/csrf v1.7.3"; csrf++; next
  }
  { print }
  END { if (jose != 1 || net != 1 || csrf != 1) exit 1 }
' "$stage/source/go.mod" >"$stage/go.mod" || fail 'module patch differs'
awk '
  $0 == "github.com/gorilla/csrf v1.7.2 h1:oTUjx0vyf2T+wkrx09Trsev1TE+/EbDAeHtSTbtC2eI=" { originalCSRF++ }
  $0 == "github.com/gorilla/csrf v1.7.2/go.mod h1:F1Fj3KG23WYHE6gozCmBAezKookxbIvUJT+121wTuLk=" {
    print
    print "github.com/gorilla/csrf v1.7.3 h1:BHWt6FTLZAb2HtWT5KDBf6qgpZzvtbp9QWDRKZMXJC0="
    print "github.com/gorilla/csrf v1.7.3/go.mod h1:F1Fj3KG23WYHE6gozCmBAezKookxbIvUJT+121wTuLk="
    changedCSRF++; next
  }
  $0 == "github.com/go-jose/go-jose/v4 v4.1.4 h1:moDMcTHmvE6Groj34emNPLs/qtYXRVcd6S7NHbHz3kA=" { original++ }
  $0 == "github.com/go-jose/go-jose/v4 v4.1.4/go.mod h1:x4oUasVrzR7071A4TnHLGSPpNOm2a21K9Kf04k1rs08=" {
    print
    print "github.com/go-jose/go-jose/v4 v4.1.6 h1:sNRkftxFJxwsILiQukqp5z83QBSmntVwmgRY7AqBDO4="
    print "github.com/go-jose/go-jose/v4 v4.1.6/go.mod h1:x4oUasVrzR7071A4TnHLGSPpNOm2a21K9Kf04k1rs08="
    changed++; next
  }
  $0 == "golang.org/x/net v0.61.0 h1:RR+6j/BTBCrggOLugxa+66R8FvoN6MHlzDlZIbN9YFw=" { originalNet++ }
  $0 == "golang.org/x/net v0.61.0/go.mod h1:WgDCOTH8iwtB66T4/GfI94AHo/hhdb7Hah9NDseQfiI=" {
    print
    print "golang.org/x/net v0.62.0 h1:18ELalfM5+XNRwzM2nYqXsZQ9ByS1uhj2O/U7+5jqRY="
    print "golang.org/x/net v0.62.0/go.mod h1:WgDCOTH8iwtB66T4/GfI94AHo/hhdb7Hah9NDseQfiI="
    changedNet++; next
  }
  { print }
  END { if (original != 1 || changed != 1 || originalNet != 1 || changedNet != 1 || originalCSRF != 1 || changedCSRF != 1) exit 1 }
' "$stage/source/go.sum" >"$stage/go.sum" || fail 'checksum patch differs'
[[ "$(shasum -a 256 "$stage/go.mod" | awk '{print $1}')" = d93f15da9568e8845430997cf6a1f5831decb2c3a526d71ffa8a0d869fe65b50 ]] || fail 'patched module differs'
[[ "$(shasum -a 256 "$stage/go.sum" | awk '{print $1}')" = 19e4a6df1f26dd45007ee996eb654923e561dd9aa818315abed3a40577d2f8ba ]] || fail 'patched checksums differ'
mv "$stage/go.mod" "$stage/source/go.mod"
mv "$stage/go.sum" "$stage/source/go.sum"
rm -- "$stage/source/console/src/assets/ailerons.otf" \
  "$stage/source/console/src/styles/ailerons.scss" \
  "$stage/source/internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf" \
  "$stage/source/internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss"
# Check every surviving regular-file mode, not just bytes or executable examples.
(cd "$stage/source" && find . -type f ! -perm 0644 ! -perm 0755 -print) >"$stage/invalid-modes"
[[ ! -s "$stage/invalid-modes" ]] || fail 'source has unexpected file permissions'
awk '$1 == "100755" {print substr($0,index($0,"\t")+1)}' "$stage/tree" | sort >"$stage/expected-executables"
# Only the four removed Apple SVG originals were executable.
for removed in console/src/assets/images/idp/apple-dark.svg console/src/assets/images/idp/apple.svg \
  internal/api/ui/login/static/resources/images/idp/apple-dark.svg internal/api/ui/login/static/resources/images/idp/apple.svg; do
  grep -Fxv "$removed" "$stage/expected-executables" >"$stage/executables-next"
  mv "$stage/executables-next" "$stage/expected-executables"
done
(cd "$stage/source" && find . -type f -perm 0755 | sed 's@^./@@' | sort) >"$stage/actual-executables"
cmp "$stage/expected-executables" "$stage/actual-executables" || fail 'source executable modes differ from committed source'
recipe_hash="$(shasum -a 256 "$0" | awk '{print $1}')" || fail 'recipe hash read failed'
input_hash="$(shasum -a 256 "$root/scripts/zitadel-source-inputs.sha256" | awk '{print $1}')" || fail 'input-manifest hash read failed'
[[ "$recipe_hash" =~ ^[0-9a-f]{64}$ && "$input_hash" =~ ^[0-9a-f]{64}$ ]] || fail 'receipt hash is incomplete'
jq -n --arg pin "$pin" --arg recipe "$recipe_hash" --arg inputs "$input_hash" \
  '{schemaVersion:1,upstreamCommit:$pin,recipeSha256:$recipe,inputManifestSha256:$inputs,
    imageAdmission:"unknown",licenseClosure:"unknown",metadataProfile:"machine-id-no-jsonpath-v1",
    dependencyProfile:"go-jose-4.1.6-x-net-0.62.0-csrf-1.7.3-v1",
    removedFiles:["console/src/assets/ailerons.otf","console/src/styles/ailerons.scss",
      "internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf",
      "internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss","console/src/assets/images/idp/apple-dark.svg","console/src/assets/images/idp/apple.svg","console/src/assets/images/idp/github-dark.svg","console/src/assets/images/idp/github.svg","console/src/assets/images/idp/gitlab.svg","console/src/assets/images/idp/google.png","console/src/assets/images/idp/ms.svg","console/src/assets/images/idp/oauth.svg","console/src/assets/images/idp/saml-icon.svg","internal/api/ui/login/static/resources/images/idp/apple-dark.svg","internal/api/ui/login/static/resources/images/idp/apple.svg","internal/api/ui/login/static/resources/images/idp/github-white.png","internal/api/ui/login/static/resources/images/idp/github.png","internal/api/ui/login/static/resources/images/idp/gitlab.png","internal/api/ui/login/static/resources/images/idp/google.png","internal/api/ui/login/static/resources/images/idp/ms.svg"],
    changedFiles:["console/src/styles.scss","internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss","internal/id/sonyflake.go","internal/api/ui/login/login.go","go.mod","go.sum","console/src/app/modules/idp-table/idp-table.component.html","console/src/app/modules/policies/idp-settings/idp-settings.component.html","console/src/app/modules/providers/provider-apple/provider-apple.component.html","console/src/app/modules/providers/provider-azure-ad/provider-azure-ad.component.html","console/src/app/modules/providers/provider-github-es/provider-github-es.component.html","console/src/app/modules/providers/provider-github/provider-github.component.html","console/src/app/modules/providers/provider-gitlab-self-hosted/provider-gitlab-self-hosted.component.html","console/src/app/modules/providers/provider-gitlab/provider-gitlab.component.html","console/src/app/modules/providers/provider-google/provider-google.component.html","console/src/app/modules/providers/provider-oauth/provider-oauth.component.html","console/src/app/modules/providers/provider-saml-sp/provider-saml-sp.component.html","internal/api/ui/login/static/templates/login.html","internal/api/ui/login/static/templates/register_option.html","console/src/app/modules/idp-table/idp-table.component.scss","console/src/app/modules/policies/idp-settings/idp-settings.component.scss","console/src/app/modules/providers/providers.scss","internal/api/ui/login/static/resources/themes/scss/styles/identity_provider/identity_provider_base.scss","internal/api/ui/login/static/resources/themes/scss/styles/vars.scss"],providerProfile:"native-neutral-provider-icons-v1"}' >"$stage/source-preparation.json"
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
