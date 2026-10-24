#!/usr/bin/env bash
# Explicit build evidence only: no image, distribution or activation authority.
set -euo pipefail
export LC_ALL=C
[[ "$#" -gt 0 ]] || exit 0
# Report a failed build invariant and terminate without a success receipt.
fail() { printf 'zitadel-linux: %s\n' "$1" >&2; exit 1; }
requested=0 capture_source=0 source_repo='' output=''
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --build) [[ "$requested" -eq 0 ]] || fail 'duplicate opt-in'; requested=1; shift ;;
    --capture-source) [[ "$capture_source" -eq 0 ]] || fail 'duplicate source capture'; capture_source=1; shift ;;
    --source) [[ -z "$source_repo" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid source'; source_repo="$2"; shift 2 ;;
    --output) [[ -z "$output" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid output'; output="$2"; shift 2 ;;
    *) fail 'expected --build [--capture-source] --source <frozen-git-repository> --output <new-directory>' ;;
  esac
done
[[ "$requested" -eq 1 && -n "$source_repo" && -n "$output" ]] || fail 'explicit build, source and output are required'
for tool in bash git jq shasum node pnpm go time readelf find sort cmp comm diff grep sed awk; do
  command -v "$tool" >/dev/null || fail "missing tool: $tool"
done
[[ "$(uname -s)" = Linux && "$(uname -m)" = x86_64 ]] || fail 'only native Linux amd64 is admitted'
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOFIPS140=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64
export GOMAXPROCS=2 GOFLAGS=-p=2 GOEXPERIMENT=''
export NX_NO_CLOUD=true NX_SKIP_NX_CACHE=true NX_DAEMON=false NX_TUI=false NX_PARALLEL=1
export NX_TASKS_RUNNER_DYNAMIC_OUTPUT=false COREPACK_ENABLE_PROJECT_SPEC=0 COREPACK_DEFAULT_TO_LATEST=0
export npm_config_manage_package_manager_versions=false
[[ "$(go env GOVERSION)" = go1.26.10 ]] || fail 'Go 1.26.10 is required'
go_root="$(go env GOROOT)"
[[ "$go_root" = /* && -d "$go_root" && ! -L "$go_root" ]] || fail 'selected Go distribution is unavailable'
[[ "$(node --version)" = v22.23.3 ]] || fail 'Node 22.23.3 is required'
[[ "$(pnpm --version)" = 10.30.3 ]] || fail 'pnpm 10.30.3 is required'
[[ -d "$source_repo" && ! -e "$output" && ! -L "$output" ]] || fail 'input must exist and output must be new'
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
source_repo="$(cd "$source_repo" && pwd -P)"
parent="$(cd "$(dirname "$output")" && pwd -P)"
name="$(basename "$output")"
[[ "$name" != . && "$name" != .. ]] || fail 'invalid output name'
output="$parent/$name"
[[ "$output" != "$source_repo/"* ]] || fail 'output must be outside input repository'
# A fresh reservation owns all generated output. Failed builds have no success receipt.
mkdir -m 700 -- "$output"
complete=0
trap 'if [[ "$complete" -ne 1 ]]; then printf "Incomplete build retained for diagnosis: %s\n" "$output" >&2; fi' EXIT
trap 'exit 1' HUP INT TERM
pin=8afa5dd37a8e723efb9eace8d09fd5c1cccef694
version=4.20.0
source_date=2026-10-22T07:05:44Z
started="$(date -u +%s)"
bash "$root/scripts/prepare-zitadel-source.sh" --prepare --source "$source_repo" --output "$output/prepared"
jq -e --arg pin "$pin" '.schemaVersion == 1 and .upstreamCommit == $pin and .imageAdmission == "unknown" and .licenseClosure == "unknown" and .metadataProfile == "machine-id-no-jsonpath-v1"' "$output/prepared/source-preparation.json" >/dev/null
cd "$output/prepared/source"
inputs=(go.mod go.sum package.json pnpm-lock.yaml pnpm-workspace.yaml console/package.json proto/buf.lock)
shasum -a 256 "${inputs[@]}" >"$output/inputs.sha256"
cp apps/api/project.json "$output/api-project.original.json"
# Preserve every native command/dependency; bound the otherwise parallel 14 Go
# installs inside nx:run-commands as well as Nx's outer task scheduler.
jq '.targets["generate-install"].options.parallel = false' apps/api/project.json >"$output/api-project.serial.json"
cp "$output/api-project.serial.json" apps/api/project.json
export GOCACHE="$output/go-cache" GOMODCACHE="$output/go-modules"
mkdir "$GOCACHE" "$GOMODCACHE"
source_snapshot_json=null
if [[ "$capture_source" -eq 1 ]]; then
  # Capture before any install/generation can alter maintained source or add
  # caches. The helper admits the complete fixed profile, not just seven locks.
  go run "$root/scripts/zitadel-source-bundle.go" --capture-build "$output" "$root" "$output/source-snapshot" >"$output/source-capture.pending"
  jq -e '.preparedProfileSha256 == "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d" and .files == 9599 and .bytes == 261683913 and .records == 10 and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/source-capture.pending" >/dev/null || fail 'incomplete prepared-source capture'
  source_manifest="$(jq -er .manifestSha256 "$output/source-capture.pending")"
  go run "$root/scripts/zitadel-source-bundle.go" --pack "$output/source-snapshot" "$source_manifest" "$output/prepared-source.tar" >"$output/source-archive.pending"
  jq -e --arg manifest "$source_manifest" '.manifestSha256 == $manifest and (.archiveSha256 | test("^[0-9a-f]{64}$"))' "$output/source-archive.pending" >/dev/null || fail 'incomplete prepared-source archive'
fi
# Run one real phase with its own resource/log records and preserve failure status.
run_phase() {
  local name="$1"; shift
  # pipefail preserves a failing process even when its final log says success.
  command time -v -o "$output/$name.resources.txt" "$@" 2>&1 | tee "$output/$name.log"
}
run_phase install pnpm install --frozen-lockfile --filter @zitadel/zitadel --filter '@zitadel/console...'
[[ "$(pnpm --version)" = 10.30.3 && "$(cd console && pnpm --version)" = 10.30.3 ]] || fail 'nested pnpm version changed'
shasum -a 256 -c "$output/inputs.sha256" >/dev/null
run_phase generate pnpm exec nx run @zitadel/api:generate --parallel=1 --skip-nx-cache --no-cloud
for path in internal/api/assets/authz.go internal/api/assets/router.go internal/api/ui/login/statik/statik.go internal/notification/statik/statik.go internal/statik/statik.go internal/api/ui/login/static/resources/themes/zitadel/css/zitadel.css; do
  [[ -s "$path" && ! -L "$path" ]] || fail "generation output missing: $path"
done
find pkg/grpc -name '*.pb.go' -type f -size +0c | grep . >/dev/null || fail 'protobuf output missing'
find openapi/v2/zitadel -name '*.json' -type f -size +0c | grep . >/dev/null || fail 'OpenAPI output missing'
# The console installs OpenAPI 2.22 after API generation used 2.28. Never overlap.
run_phase console pnpm exec nx run @zitadel/api:build-console --parallel=1 --skip-nx-cache --no-cloud
console=console/dist/console
embedded=internal/api/ui/console/static
for path in index.html 3rdpartylicenses.txt; do
  [[ -s "$console/$path" && ! -L "$console/$path" ]] || fail "console output missing: $path"
done
grep -oE '(src|href)="[^" ]+\.(js|css)"' "$console/index.html" >"$output/console-references.txt"
[[ -s "$output/console-references.txt" ]] || fail 'console script/style references absent'
while IFS= read -r reference; do
  reference="${reference#*\"}"; reference="${reference%\"}"
  [[ "$reference" != /* && "$reference" != *..* && "$reference" != *:* && -s "$console/$reference" && ! -L "$console/$reference" ]] || fail 'console reference is missing or nonlocal'
done <"$output/console-references.txt"
diff_status=0
diff -qr "$console" "$embedded" >"$output/console-copy.raw.diff" || diff_status=$?
[[ "$diff_status" -le 1 ]] || fail 'console comparison failed'
grep -v '^Only in .*: gitkeep$' "$output/console-copy.raw.diff" >"$output/console-copy.diff" || [[ "$?" -eq 1 ]]
[[ ! -s "$output/console-copy.diff" ]] || fail 'embedded console differs from built console'
find console/src internal/api/ui/login/static "$console" "$embedded" \( -type f -o -type l \) -iname '*aileron*' -print >"$output/font-paths.txt"
[[ ! -s "$output/font-paths.txt" ]] || fail 'unadmitted font file survived'
font_status=0
grep -riE 'aileron' "$console" "$embedded" internal/api/ui/login/static/resources/themes/zitadel/css >"$output/font-references.txt" || font_status=$?
[[ "$font_status" -eq 1 ]] || fail 'font reference survived or scan failed'
go mod verify
shasum -a 256 -c "$output/inputs.sha256" >/dev/null
go list -deps -json . >"$output/packages.jsonl"
jq -se 'length > 0 and all(.[];
  (.ImportPath != "github.com/jarcoal/jpath") and
  ((.ImportPath // "") | startswith("github.com/jarcoal/jpath/") | not) and
  (.Module.Path != "github.com/jarcoal/jpath") and
  (.Module.Replace.Path != "github.com/jarcoal/jpath"))' "$output/packages.jsonl" >/dev/null || fail 'unadmitted metadata selector remains in compiler inputs'
jq -se --arg directory "$PWD/internal/api/ui/login/statik" '
  map(select(.ImportPath == "github.com/zitadel/zitadel/internal/api/ui/login/statik")) |
  length == 1 and .[0].Dir == $directory and
  ((.[0].GoFiles // []) | sort) == ["generate.go", "statik.go"] and
  ((.[0].IgnoredGoFiles // []) | index("statik.go") == null)
' "$output/packages.jsonl" >/dev/null || fail 'Go compiler does not select the inspected login registration'
jq -e 'select(.ImportPath == "github.com/zitadel/zitadel/internal/api/ui/console") | .EmbedPatterns == ["static"]' "$output/packages.jsonl" >/dev/null
jq -er 'select(.ImportPath == "github.com/zitadel/zitadel/internal/api/ui/console") | .EmbedFiles[]' "$output/packages.jsonl" | sort >"$output/console-embed.txt"
# Native go:embed directory semantics omit dot/underscore path components, such
# as the copied assets/.gitignore. Preserve those inputs and record exclusions;
# referenced runtime resources must still appear in the actual compiler set.
(cd internal/api/ui/console && find static \( -name '.*' -o -name '_*' \) -prune -o -type f -print | sort) >"$output/console-files.txt"
(cd internal/api/ui/console && find static -type f | sort) >"$output/console-all-files.txt"
comm -23 "$output/console-all-files.txt" "$output/console-files.txt" >"$output/console-native-exclusions.txt"
cmp "$output/console-files.txt" "$output/console-embed.txt" || fail 'Go compiler does not embed the complete console'
while IFS= read -r reference; do
  reference="${reference#*\"}"; reference="${reference%\"}"
  while [[ "$reference" == ./* ]]; do reference="${reference#./}"; done
  grep -Fx "static/$reference" "$output/console-embed.txt" >/dev/null || fail 'referenced console resource is not embedded'
done <"$output/console-references.txt"
go run "$root/scripts/zitadel-console-notices.go" --snapshot --build "$output" --recipe "$root/scripts/build-zitadel-linux.sh" --supplement "$root/third_party/zitadel-console-notices" --toolchain "$go_root" --source-license --modules "$root/third_party/zitadel-go-module-notices" >"$output/console-notices.before.json"
shasum -a 256 internal/api/ui/login/statik/statik.go >"$output/login-statik.before.sha256"
mkdir "$output/service"
binary="$output/service/zitadel"
run_phase compile go build -buildvcs=false -o "$binary" \
  -ldflags="-s -w -X github.com/zitadel/zitadel/cmd/build.version=$version -X github.com/zitadel/zitadel/cmd/build.commit=$pin -X github.com/zitadel/zitadel/cmd/build.date=$source_date" .
[[ -s "$binary" && -x "$binary" && ! -L "$binary" ]] || fail 'service binary missing'
readelf -h "$binary" >"$output/elf.txt"
grep -q 'Advanced Micro Devices X86-64' "$output/elf.txt" || fail 'wrong ELF architecture'
readelf -l "$binary" >"$output/elf-program.txt"
if grep -q INTERP "$output/elf-program.txt"; then fail 'unexpected dynamic interpreter'; fi
go version -m "$binary" >"$output/build-info.txt"
awk '($1 == "dep" || $1 == "=>" || $1 == "mod") && $2 == "github.com/jarcoal/jpath" {bad=1} END {exit bad}' \
  "$output/build-info.txt" || fail 'unadmitted metadata selector remains in binary metadata'
for expected in go1.26.10 GOOS=linux GOARCH=amd64 CGO_ENABLED=0; do
  grep -qF "$expected" "$output/build-info.txt" || fail "service build info missing: $expected"
done
[[ "$("$binary" --version)" = "zitadel version $version" ]] || fail 'service version execution failed'
shasum -a 256 -c "$output/login-statik.before.sha256" >/dev/null
shasum -a 256 -c "$output/inputs.sha256" >/dev/null
# Recheck the same private module cache after compilation, before final capture.
go mod verify
find "$console" "$embedded" internal/api/ui/login/static internal/api/ui/login/statik internal/notification/statik internal/statik -type f -exec shasum -a 256 {} + | sort >"$output/generated.sha256"
shasum -a 256 "$root/scripts/zitadel-login-icons.go" "$root/third_party/zitadel-login-icons/"* >"$output/login-icon-verifier.sha256"
shasum -a 256 "$root/scripts/zitadel-console-notices.go" "$root/third_party/zitadel-console-notices/"* "$root/third_party/zitadel-go-module-notices/catalogue.json" >"$output/console-notice-verifier.sha256"
binary_hash="$(shasum -a 256 "$binary" | awk '{print $1}')"
recipe_hash="$(shasum -a 256 "$root/scripts/build-zitadel-linux.sh" | awk '{print $1}')"
[[ "$binary_hash" =~ ^[0-9a-f]{64}$ && "$recipe_hash" =~ ^[0-9a-f]{64}$ ]] || fail 'incomplete receipt digests'
if [[ "$capture_source" -eq 1 ]]; then
  go run "$root/scripts/zitadel-source-bundle.go" --verify "$output/source-snapshot" "$source_manifest" >"$output/source-readback.pending"
  archive_hash="$(shasum -a 256 "$output/prepared-source.tar" | awk '{print $1}')"
  jq -e --arg archive "$archive_hash" --arg manifest "$source_manifest" '.archiveSha256 == $archive and .manifestSha256 == $manifest' "$output/source-archive.pending" >/dev/null || fail 'prepared-source archive changed during build'
  jq -e --arg recipe "$recipe_hash" '.recipeSha256 == $recipe' "$output/source-capture.pending" >/dev/null || fail 'captured source recipe changed during build'
  source_snapshot_json="$(jq -n --slurpfile capture "$output/source-capture.pending" --slurpfile archive "$output/source-archive.pending" '$capture[0] + $archive[0] + {dependencyInputsCaptured:false,nativeOfflineReplayProved:false}')"
fi
jq -n --arg pin "$pin" --arg version "$version" --arg date "$source_date" --arg binary "$binary_hash" --arg recipe "$recipe_hash" \
  --argjson snapshot "$source_snapshot_json" \
  --argjson elapsed "$(($(date -u +%s) - started))" --slurpfile preparation "$output/prepared/source-preparation.json" \
    '{schemaVersion:1,upstreamCommit:$pin,version:$version,sourceDate:$date,platform:"linux/amd64",profile:"api-login-v1-console",recipeSha256:$recipe,binarySha256:$binary,elapsedSeconds:$elapsed,preparation:$preparation[0],tools:{go:"1.26.10",node:"22.23.3",pnpm:"10.30.3"},imageAdmission:"unknown",licenseClosure:"unknown",reproducible:false} + (if $snapshot == null then {} else {sourceSnapshot:$snapshot} end)' >"$output/build-receipt.pending"
[[ "$(go env GOROOT)" = "$go_root" && "$(go env GOVERSION)" = go1.26.10 ]] || fail 'selected Go compiler changed'
go run "$root/scripts/zitadel-console-notices.go" --inventory --build "$output" --recipe "$root/scripts/build-zitadel-linux.sh" --supplement "$root/third_party/zitadel-console-notices" --toolchain "$go_root" --source-license --modules "$root/third_party/zitadel-go-module-notices" >"$output/console-notices-inventory.pending"
go run "$root/scripts/zitadel-login-icons.go" --inventory --build "$output" --notices "$root/third_party/zitadel-login-icons" >"$output/login-icons-inventory.pending"
mv "$output/login-icons-inventory.pending" "$output/login-icons-inventory.json"
mv "$output/console-notices-inventory.pending" "$output/console-notices-inventory.json"
mv "$output/console-notices-supplement-inventory.pending" "$output/console-notices-supplement-inventory.json"
mv "$output/build-receipt.pending" "$output/build-receipt.json"
complete=1
printf 'Complete native build evidence only: %s\n' "$output"
