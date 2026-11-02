#!/usr/bin/env bash
# CI-only verification of the owner's exact file grant; not distribution admission.
set -euo pipefail
# Refuse without emitting a success result or inspected file contents.
fail() { printf 'identity-build license scope: %s\n' "$1" >&2; exit 1; }
[ "$#" -le 1 ] || fail 'expected at most one repository directory'
root="$(cd "${1:-$(dirname "$0")/..}" && pwd -P)"
command -v jq >/dev/null || fail 'jq unavailable'
# Scope members must be physical regular files, including every ancestor.
regular_member() {
  local relative="$1" current="$root" component
  local -a components
  IFS=/ read -r -a components <<< "$relative"
  for component in "${components[@]}"; do
    current="$current/$component"
    [ ! -L "$current" ] || fail 'linked scope member'
  done
  [ -f "$current" ] || fail 'missing or non-regular scope member'
}
work="$(mktemp -d)" || fail 'cannot reserve metadata snapshot'
trap 'rm -rf "$work"' EXIT
# Bound and completely compare each metadata observation before parsing it.
snapshot_member() {
  local member="$1" limit="$2" destination="$3"
  regular_member "$member"
  dd if="$root/$member" of="$work/$destination" bs="$((limit + 1))" count=1 2>/dev/null ||
    fail 'incomplete metadata read'
  [ "$(wc -c < "$work/$destination")" -le "$limit" ] || fail 'metadata exceeds bound'
  cmp -s "$root/$member" "$work/$destination" || fail 'metadata changed during snapshot'
}
scope=third_party/zitadel-build-tools/scope.json
snapshot_member "$scope" 16384 scope.json
snapshot_member third_party/zitadel-build-tools/COPYING 65536 terms
snapshot_member tools/zitadel-pnpm-source/package.json 16384 package.json
snapshot_member tools/zitadel-pnpm-source/package-lock.json 2097152 package-lock.json
expected='["scripts/build-zitadel-linux.sh","scripts/prepare-zitadel-source.sh","scripts/replay-zitadel-inputs.sh","scripts/zitadel-source-inputs.sha256","scripts/zitadel-source-bundle.go","scripts/zitadel-console-assets.go","scripts/zitadel-console-notices.go","scripts/zitadel-login-icons.go","scripts/zitadel-text-fonts.go","scripts/zitadel-copied-assets.go","server/go.mod","server/go.sum","server/cmd/zitadel-module-sources/capture.go","server/cmd/zitadel-module-sources/generator_sources.go","server/cmd/zitadel-module-sources/generators.go","server/cmd/zitadel-module-sources/graph_metadata.go","server/cmd/zitadel-module-sources/json_fields.go","server/cmd/zitadel-module-sources/main.go","server/cmd/zitadel-module-sources/payload.go","server/cmd/zitadel-module-sources/restore.go","server/cmd/zitadel-module-sources/selection.go","server/cmd/zitadel-bsr-sources/files.go","server/cmd/zitadel-bsr-sources/main.go","server/cmd/zitadel-bsr-sources/package.go","server/cmd/zitadel-bsr-sources/validation.go","tools/zitadel-pnpm-source/index.mjs","tools/zitadel-pnpm-source/package.json","tools/zitadel-pnpm-source/package-lock.json","Dockerfile.zitadel","scripts/zitadel-image-package.go","scripts/zitadel-image-smoke.go","scripts/zitadel-service-smoke.go","scripts/zitadel-browser.go","docs/ZITADEL-SOURCE.md","server/cmd/zitadel-module-sources/README.md","server/cmd/zitadel-bsr-sources/README.md","tools/zitadel-pnpm-source/README.md"]'
jq -se --argjson expected "$expected" '
  length == 1 and (.[0] |
    type == "object" and
    (keys | sort) == ["authoredMaterialOnly","license","licenseDocuments","schemaVersion","sourcePaths"] and
    .schemaVersion == 1 and .license == "AGPL-3.0-only" and .authoredMaterialOnly == true and
    (.sourcePaths | type) == "array" and (.sourcePaths | length) == 37 and
    (.sourcePaths | sort) == ($expected | sort) and
    .licenseDocuments == ["third_party/zitadel-build-tools/COPYING","third_party/zitadel-build-tools/README.md","third_party/zitadel-build-tools/scope.json"]
  )' "$work/scope.json" >/dev/null 2>&1 || fail 'scope differs from the approved exact grant'
# The admitted shape has exactly 43 primitive assignments. Duplicate keys,
# including an empty first array/object, add stream assignments and are refused.
jq --stream -se '[.[] | select(length == 2)] | length == 43' "$work/scope.json" >/dev/null 2>&1 ||
  fail 'ambiguous or repeated scope assignment'
members="$(jq -r '.sourcePaths[], .licenseDocuments[]' "$work/scope.json")" ||
  fail 'incomplete scope member read'
while IFS= read -r member; do regular_member "$member"; done <<< "$members"
terms="$work/terms"
[ "$(shasum -a 256 "$terms" | awk '{print $1}')" = 4df3c306dddaaf4baffdff5ca820cc679ac8cd6dc263c6a74517783e42fa7a3b ] ||
  fail 'complete AGPL text differs'
package="$work/package.json"
lock="$work/package-lock.json"
jq -se 'length == 1 and (.[0] | .name == "@devantler-tech/zitadel-pnpm-source-sdk" and .version == "0.0.0" and .private == true and .license == "AGPL-3.0-only")' "$package" >/dev/null 2>&1 ||
  fail 'private adapter grant differs'
jq -se --slurpfile package "$package" 'length == 1 and (.[0] |
  .lockfileVersion == 3 and .name == $package[0].name and .version == $package[0].version and
  .packages[""].name == $package[0].name and .packages[""].version == $package[0].version and
  .packages[""].license == "AGPL-3.0-only" and
  .packages[""].dependencies == $package[0].dependencies and
  .packages[""].devDependencies == $package[0].devDependencies)' "$lock" >/dev/null 2>&1 ||
  fail 'generated adapter lock differs'
regular_member "$scope"
regular_member third_party/zitadel-build-tools/COPYING
regular_member tools/zitadel-pnpm-source/package.json
regular_member tools/zitadel-pnpm-source/package-lock.json
if cmp -s "$root/$scope" "$work/scope.json" &&
  cmp -s "$root/third_party/zitadel-build-tools/COPYING" "$work/terms" &&
  cmp -s "$root/tools/zitadel-pnpm-source/package.json" "$package" &&
  cmp -s "$root/tools/zitadel-pnpm-source/package-lock.json" "$lock"; then
  :
else
  fail 'metadata changed after observation'
fi
printf '%s\n' '{"license":"AGPL-3.0-only","sourceFiles":37,"licenseDocuments":3,"distributionAdmission":false}'
