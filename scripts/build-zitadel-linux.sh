#!/usr/bin/env bash
# Explicit build evidence only: no image, distribution or activation authority.
set -euo pipefail
umask 022
export LC_ALL=C
[[ "$#" -gt 0 ]] || exit 0
# Report a failed build invariant and terminate without a success receipt.
fail() { printf 'zitadel-linux: %s\n' "$1" >&2; exit 1; }
requested=0 capture_source=0 source_repo='' output='' module_verifier='' bsr_verifier='' prepared_source='' prepared_sha='' console_inputs='' console_sha='' source_manifest=''
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --build) [[ "$requested" -eq 0 ]] || fail 'duplicate opt-in'; requested=1; shift ;;
    --capture-source) [[ "$capture_source" -eq 0 ]] || fail 'duplicate source capture'; capture_source=1; shift ;;
    --module-source-verifier) [[ -z "$module_verifier" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid module source verifier'; module_verifier="$2"; shift 2 ;;
    --bsr-source-verifier) [[ -z "$bsr_verifier" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid schema source verifier'; bsr_verifier="$2"; shift 2 ;;
    --source) [[ -z "$source_repo" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid source'; source_repo="$2"; shift 2 ;;
    --prepared-source) [[ -z "$prepared_source" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid prepared source'; prepared_source="$2"; shift 2 ;;
    --prepared-sha256) [[ -z "$prepared_sha" && "$#" -ge 2 && "$2" =~ ^[0-9a-f]{64}$ ]] || fail 'invalid prepared source seal'; prepared_sha="$2"; shift 2 ;;
    --console-inputs) [[ -z "$console_inputs" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid console inputs'; console_inputs="$2"; shift 2 ;;
    --console-sha256) [[ -z "$console_sha" && "$#" -ge 2 && "$2" =~ ^[0-9a-f]{64}$ ]] || fail 'invalid console input seal'; console_sha="$2"; shift 2 ;;
    --output) [[ -z "$output" && "$#" -ge 2 && -n "$2" ]] || fail 'invalid output'; output="$2"; shift 2 ;;
    *) fail 'expected --build and exactly one original --source or sealed --prepared-source/--prepared-sha256 input, with --output <new-directory>' ;;
  esac
done
[[ "$requested" -eq 1 && -n "$output" ]] || fail 'explicit build and output are required'
if [[ -n "$prepared_source" ]]; then
  [[ -z "$source_repo" && -n "$prepared_sha" && "$capture_source" -eq 0 ]] || fail 'prepared source requires its seal and excludes original source or capture'
else
  [[ -n "$source_repo" && -z "$prepared_sha" ]] || fail 'exactly one complete source mode is required'
fi
if [[ -n "$console_inputs$console_sha" ]]; then
  [[ -n "$prepared_source" && -n "$console_inputs" && -n "$console_sha" && "$capture_source" -eq 0 ]] || fail 'console replay requires a complete capsule/seal pair with prepared source'
fi
if [[ "$capture_source" -eq 1 ]]; then
  [[ "$module_verifier" = /* && -f "$module_verifier" && -x "$module_verifier" && ! -L "$module_verifier" ]] || fail 'source capture requires an explicit regular executable module verifier'
  [[ "$bsr_verifier" = /* && -f "$bsr_verifier" && -x "$bsr_verifier" && ! -L "$bsr_verifier" ]] || fail 'source capture requires an explicit regular executable schema verifier'
  for tool in curl sudo unshare setpriv id realpath; do command -v "$tool" >/dev/null || fail "missing source capture tool: $tool"; done
else
  [[ -z "$module_verifier" && -z "$bsr_verifier" ]] || fail 'source verifiers require source capture'
fi
for tool in bash git jq shasum node pnpm go time readelf find sort cmp comm diff grep sed awk; do
  command -v "$tool" >/dev/null || fail "missing tool: $tool"
done
[[ "$(uname -s)" = Linux && "$(uname -m)" = x86_64 ]] || fail 'only native Linux amd64 is admitted'
export GOTOOLCHAIN=local GOENV=off GOWORK=off GOFIPS140=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64
export GOMAXPROCS=2 GOFLAGS=-p=2 GOEXPERIMENT=''
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
export GONOPROXY='' GONOSUMDB='' GOPRIVATE='' GOINSECURE=''
export NX_NO_CLOUD=true NX_SKIP_NX_CACHE=true NX_DAEMON=false NX_TUI=false NX_PARALLEL=1
export NX_TASKS_RUNNER_DYNAMIC_OUTPUT=false COREPACK_ENABLE_PROJECT_SPEC=0 COREPACK_DEFAULT_TO_LATEST=0
# Environment keys are names only; never print or retain an inherited registry,
# token or proxy configuration as build diagnostics. The frozen project config
# remains active; host user/global configuration is replaced below.
while IFS= read -r config_key; do
  case "$config_key" in [Nn][Pp][Mm]_[Cc][Oo][Nn][Ff][Ii][Gg]_*) unset "$config_key";; esac
done < <(compgen -e)
export npm_config_manage_package_manager_versions=false
[[ "$(go env GOVERSION)" = go1.26.10 ]] || fail 'Go 1.26.10 is required'
go_root="$(go env GOROOT)"
[[ "$go_root" = /* && -d "$go_root" && ! -L "$go_root" ]] || fail 'selected Go distribution is unavailable'
[[ "$(node --version)" = v22.23.3 ]] || fail 'Node 22.23.3 is required'
[[ "$(pnpm --version)" = 10.30.3 ]] || fail 'pnpm 10.30.3 is required'
input_root="${source_repo:-$prepared_source}"
[[ -d "$input_root" && ! -e "$output" && ! -L "$output" ]] || fail 'input must exist and output must be new'
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
input_root="$(cd "$input_root" && pwd -P)"
if [[ -n "$source_repo" ]]; then source_repo="$input_root"; else prepared_source="$input_root"; fi
parent="$(cd "$(dirname "$output")" && pwd -P)"
name="$(basename "$output")"
[[ "$name" != . && "$name" != .. ]] || fail 'invalid output name'
output="$parent/$name"
[[ "$output" != "$input_root/"* ]] || fail 'output must be outside input source'
if [[ -n "$console_inputs" ]]; then
  [[ "$console_inputs" = /* && -d "$console_inputs" && ! -L "$console_inputs" ]] || fail 'console capsule must be an existing physical absolute directory'
  [[ "$console_inputs" = "$(cd "$console_inputs" && pwd -P)" ]] || fail 'console capsule path must be physical and clean'
  [[ "$output" != "$console_inputs" && "$output" != "$console_inputs/"* ]] || fail 'output must be outside the sealed console capsule'
fi
# A fresh reservation owns all generated output. Failed builds have no success receipt.
prepared_input_json=null
if [[ -n "$prepared_source" ]]; then
  prepared_input_json="$(go run "$root/scripts/zitadel-source-bundle.go" --restore-build "$prepared_source" "$prepared_sha" "$output")" || fail 'sealed prepared source restoration failed'
else
  mkdir -m 700 -- "$output"
fi
complete=0
trap 'if [[ "$complete" -ne 1 ]]; then printf "Incomplete build retained for diagnosis: %s\n" "$output" >&2; fi' EXIT
trap 'exit 1' HUP INT TERM
module_verifier_hash='' bsr_verifier_hash=''
# Both source modes acquire schemas from an owned cache and empty configuration;
# source restoration must not inherit a host cache or an account's credentials.
export BUF_CACHE_DIR="$output/buf-cache" XDG_CONFIG_HOME="$output/bsr-config"
unset BUF_TOKEN
mkdir -m 700 "$BUF_CACHE_DIR" "$XDG_CONFIG_HOME"
# Check the separately compiled command without changing the service compiler.
verifier_digest() {
  local name="$1" executable="$2" command="$3"
  go version -m "$executable" >"$output/$name-source-verifier.txt" || fail 'verifier metadata read failed'
  awk -v expected="github.com/devantler-tech/business-site/server/cmd/$command" '
    NR == 1 && $NF == "go1.27.3" {compiler=1}
    $1 == "path" && $2 == expected {command=1}
    END {exit !(compiler && command)}' "$output/$name-source-verifier.txt" || fail 'verifier command or compiler differs'
  shasum -a 256 "$executable" | awk '{print $1}'
}
if [[ "$capture_source" -eq 1 ]]; then
  # The reviewed command is built separately with the repository compiler. Do
  # not switch or auto-download toolchains inside the frozen upstream build.
  module_verifier_hash="$(verifier_digest go-module "$module_verifier" zitadel-module-sources)"
  bsr_verifier_hash="$(verifier_digest bsr "$bsr_verifier" zitadel-bsr-sources)"
  [[ "$module_verifier_hash" =~ ^[0-9a-f]{64}$ && "$bsr_verifier_hash" =~ ^[0-9a-f]{64}$ ]] || fail 'verifier digest unavailable'
  mkdir -m 700 "$output/bsr-tools"
fi
pin=8afa5dd37a8e723efb9eace8d09fd5c1cccef694
version=4.20.0
source_date=2026-10-22T07:05:44Z
started="$(date -u +%s)"
if [[ -z "$prepared_source" ]]; then
  bash "$root/scripts/prepare-zitadel-source.sh" --prepare --source "$source_repo" --output "$output/prepared"
fi
jq -e --arg pin "$pin" '.schemaVersion == 1 and .upstreamCommit == $pin and .imageAdmission == "unknown" and .licenseClosure == "unknown" and .metadataProfile == "machine-id-no-jsonpath-v1"' "$output/prepared/source-preparation.json" >/dev/null
cd "$output/prepared/source"
inputs=(go.mod go.sum package.json pnpm-lock.yaml pnpm-workspace.yaml console/package.json proto/buf.lock)
if [[ -z "$prepared_source" ]]; then
  shasum -a 256 "${inputs[@]}" >"$output/inputs.sha256"
  cp apps/api/project.json "$output/api-project.original.json"
  # Preserve every native command/dependency; bound the otherwise parallel 14 Go
  # installs inside nx:run-commands as well as Nx's outer task scheduler.
  jq '.targets["generate-install"].options.parallel = false' apps/api/project.json >"$output/api-project.serial.json"
  cp "$output/api-project.serial.json" apps/api/project.json
fi
export GOCACHE="$output/go-cache" GOMODCACHE="$output/go-modules" GOPATH="$output/go-path"
# This build's acquisition closure must not read a user's pnpm store or Go
# checksum-database state. Keep every writable acquisition location owned here;
# the frozen source's project configuration remains part of the sealed input.
export npm_config_userconfig="$output/npmrc" npm_config_globalconfig="$output/npm-globalrc" npm_config_cache="$output/npm-cache"
export npm_config_store_dir="$output/pnpm-store"
export npm_config_engine_strict=false npm_config_registry=https://registry.npmjs.org/
export npm_config_side_effects_cache=false
mkdir "$GOCACHE" "$GOMODCACHE" "$GOPATH" "$npm_config_cache" "$npm_config_store_dir"
: >"$npm_config_userconfig"
: >"$npm_config_globalconfig"
if [[ -n "$prepared_source" ]]; then
  jq -e --arg seal "$prepared_sha" '.manifestSha256 == $seal and .preparedProfileSha256 == "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d" and .files == 9599 and .records == 14 and .bytes == 261683913' <<<"$prepared_input_json" >/dev/null || fail 'restored source does not match the frozen complete profile'
  # Reuse capture's exact source/recipe/transformation admission before install.
  # Equality also binds the retained modification record and original API bytes;
  # neither preparation nor the API serialization transform is rerun here.
  go run "$root/scripts/zitadel-source-bundle.go" --capture-build "$output" "$root" "$output/prepared-input-check" >"$output/prepared-input.pending"
  jq -e --arg seal "$prepared_sha" '.manifestSha256 == $seal' "$output/prepared-input.pending" >/dev/null || fail 'prepared source or retained recipes differ from this native builder'
  prepared_input_json="$(jq '. + {nativeOfflineReplayProved:false}' "$output/prepared-input.pending")"
fi
source_snapshot_json=null
if [[ "$capture_source" -eq 1 ]]; then
  # Capture before any install/generation can alter maintained source or add
  # caches. The helper admits the complete fixed profile, not just seven locks.
  go run "$root/scripts/zitadel-source-bundle.go" --capture-build "$output" "$root" "$output/source-snapshot" >"$output/source-capture.pending"
  jq -e '.preparedProfileSha256 == "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d" and .files == 9599 and .bytes == 261683913 and .records == 14 and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/source-capture.pending" >/dev/null || fail 'incomplete prepared-source capture'
  source_manifest="$(jq -er .manifestSha256 "$output/source-capture.pending")"
  go run "$root/scripts/zitadel-source-bundle.go" --pack "$output/source-snapshot" "$source_manifest" "$output/prepared-source.tar" >"$output/source-archive.pending"
  jq -e --arg manifest "$source_manifest" '.manifestSha256 == $manifest and (.archiveSha256 | test("^[0-9a-f]{64}$"))' "$output/source-archive.pending" >/dev/null || fail 'incomplete prepared-source archive'
  for member in index.mjs package.json package-lock.json; do
    cmp "$root/tools/zitadel-pnpm-source/$member" "$output/source-snapshot/records/recipes/zitadel-pnpm-source/$member" || fail 'native pnpm adapter differs from captured source'
  done
fi
# Run one real phase with its own resource/log records and preserve failure status.
run_phase() {
  local name="$1"; shift
  # pipefail preserves a failing process even when its final log says success.
  command time -v -o "$output/$name.resources.txt" "$@" 2>&1 | tee "$output/$name.log"
}
# Retain the actual native executables, never a package-manager launcher.
save_bsr_tool() {
  local phase="$1" input="$2" expected="$3" resolved
  resolved="$(realpath "$input")" || fail 'native schema executable is unavailable'
  [[ "$resolved" = "$output/prepared/source/"* && -f "$resolved" && -x "$resolved" && ! -L "$resolved" ]] || fail 'native schema executable escaped the owned build'
  "$resolved" --version >"$output/bsr-tools/$phase.version"
  [[ "$(cat "$output/bsr-tools/$phase.version")" = "$expected" && "$(wc -l <"$output/bsr-tools/$phase.version")" -eq 1 ]] || fail 'native schema compiler version differs'
  cp "$resolved" "$output/bsr-tools/$phase"
  chmod 700 "$output/bsr-tools/$phase"
  cmp "$resolved" "$output/bsr-tools/$phase" || fail 'native schema executable copy differs'
  shasum -a 256 "$resolved" >>"$output/bsr-tool-originals.sha256"
  shasum -a 256 "$output/bsr-tools/$phase" >>"$output/bsr-tool-copies.sha256"
}
# Refuse changes to either the native command or its retained execution copy.
check_bsr_tools() {
  shasum -a 256 -c "$output/bsr-tool-originals.sha256" >/dev/null || fail 'native schema executable changed'
  shasum -a 256 -c "$output/bsr-tool-copies.sha256" >/dev/null || fail 'retained schema executable changed'
}
# Every invocation proves its kernel privilege barrier before executing a command.
without_network() {
  local marker status=0
  marker="$(mktemp "$output/network-boundary.XXXXXX")" || fail 'cannot reserve network boundary proof'
  sudo -n unshare --net -- setpriv --no-new-privs --reuid "$(id -u)" --regid "$(id -g)" --clear-groups -- \
    env -u BUF_TOKEN -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u NO_PROXY \
      -u http_proxy -u https_proxy -u all_proxy -u no_proxy bash -c '
        verified=0
        while IFS=: read -r key value; do
          if [[ "$key" = NoNewPrivs ]]; then
            [[ "$value" =~ ^[[:space:]]*1[[:space:]]*$ ]] || exit 125
            verified=1; break
          fi
        done </proc/self/status
        [[ "$verified" = 1 ]] || exit 125
        printf "verified\n" >"$1" || exit 125
        shift
        exec "$@"
      ' bash "$marker" "$@" || status=$?
  [[ "$(cat "$marker")" = verified ]] || fail 'network boundary did not prove its privilege barrier'
  rm "$marker"
  return "$status"
}
run_phase install pnpm install --store-dir "$npm_config_store_dir" --side-effects-cache=false --frozen-lockfile --filter @zitadel/zitadel --filter '@zitadel/console...'
if [[ "$capture_source" -eq 1 ]]; then
  pnpm --filter @zitadel/zitadel --filter '@zitadel/console...' list --depth -1 --json >"$output/pnpm-selected-projects.json" || fail 'native installed workspace observation failed'
  node "$root/tools/zitadel-pnpm-source/index.mjs" inspect --build "$output" >"$output/pnpm-installation-check.json"
  jq -e '.schemaVersion == 1 and .profile == "native-installation-selection-v1" and .packages > 0 and .selectedPNPMSourcesCaptured == false and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false' "$output/pnpm-installation-check.json" >/dev/null || fail 'incomplete early native installation check'
fi
[[ "$(pnpm --version)" = 10.30.3 && "$(cd console && pnpm --version)" = 10.30.3 ]] || fail 'nested pnpm version changed'
shasum -a 256 -c "$output/inputs.sha256" >/dev/null
run_phase generate pnpm exec nx run @zitadel/api:generate --parallel=1 --skip-nx-cache --no-cloud
for path in internal/api/assets/authz.go internal/api/assets/router.go internal/api/ui/login/statik/statik.go internal/notification/statik/statik.go internal/statik/statik.go internal/api/ui/login/static/resources/themes/zitadel/css/zitadel.css; do
  [[ -s "$path" && ! -L "$path" ]] || fail "generation output missing: $path"
done
find pkg/grpc -name '*.pb.go' -type f -size +0c | grep . >/dev/null || fail 'protobuf output missing'
find openapi/v2/zitadel -name '*.json' -type f -size +0c | grep . >/dev/null || fail 'OpenAPI output missing'
if [[ "$capture_source" -eq 1 ]]; then
  # Observe all fourteen actual API generators before the console overwrites
  # protoc-gen-openapiv2 2.28.0 with its distinct 2.22.0 original download.
  "$module_verifier" observe-generators --build "$output" --source-sha256 "$source_manifest" >"$output/generator-checkpoint.json"
  generator_checkpoint="$(shasum -a 256 "$output/generator-checkpoint.json" | awk '{print $1}')"
  "$module_verifier" verify-generator-checkpoint --checkpoint "$output/generator-checkpoint.json" --sha256 "$generator_checkpoint" --source-sha256 "$source_manifest" >"$output/generator-checkpoint-receipt.json"
  jq -e --arg source "$source_manifest" --arg seal "$generator_checkpoint" '.schemaVersion == 1 and .sourceManifestSha256 == $source and .checkpointSha256 == $seal and .tools == 14 and .modules > 0 and .generatorBuildMetadataValidated == true and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false' "$output/generator-checkpoint-receipt.json" >/dev/null || fail 'incomplete native generator checkpoint'
  [[ "$(shasum -a 256 "$module_verifier" | awk '{print $1}')" = "$module_verifier_hash" ]] || fail 'module verifier changed during native generator observation'
  save_bsr_tool api "$PWD/.artifacts/bin/linux/amd64/buf" 1.67.0
  # Resolve the optional native package from the same root wrapper used by
  # console's pnpm command. Resolution reads metadata, not a retained command.
  console_buf="$(cd console && pnpm exec node -p 'require.resolve("@bufbuild/buf-linux-x64/bin/buf", {paths:[require.resolve("@bufbuild/buf/package.json")]})')"
  console_buf_version="$(cd console && pnpm exec buf --version)"
  [[ "$console_buf_version" = 1.72.0 ]] || fail 'console schema command version differs'
  save_bsr_tool console "$console_buf" 1.72.0
fi
# The console installs OpenAPI 2.22 after API generation used 2.28. Never overlap.
console_downloads_json=null
if [[ "$capture_source" -eq 1 || -n "$console_inputs" ]]; then
  console_checkpoint="$output/source-snapshot"
  [[ "$capture_source" -eq 1 ]] || console_checkpoint="$output/prepared-input-check"
  cmp "$root/scripts/zitadel-console-assets.go" "$console_checkpoint/records/recipes/zitadel-console-assets.go" || fail 'console helper differs from admitted source records'
  cmp console/scripts/install-proto-plugins.sh "$console_checkpoint/source/console/scripts/install-proto-plugins.sh" || fail 'native console installer differs from admitted source'
  console_tool="$output/console-originals-tool"
  run_phase console-input-tool go build -trimpath -buildvcs=false -o "$console_tool" "$root/scripts/zitadel-console-assets.go"
  if [[ "$capture_source" -eq 1 ]]; then
    console_inputs="$output/selected-console-originals"
    "$console_tool" capture --capsule "$console_inputs" --curl "$(command -v curl)" >"$output/console-originals-capture.json"
    console_sha="$(jq -er .manifestSha256 "$output/console-originals-capture.json")"
  fi
  "$console_tool" verify --capsule "$console_inputs" --sha256 "$console_sha" >"$output/console-originals.before.json"
  go version -m .artifacts/bin/linux/amd64/protoc-gen-openapiv2 >"$output/api-openapi-version.txt"
  awk '$1 == "mod" && $2 == "github.com/grpc-ecosystem/grpc-gateway/v2" && $3 == "v2.28.0" {found=1} END {exit !found}' "$output/api-openapi-version.txt" || fail 'API generation did not retain OpenAPI 2.28.0 before console installation'
  mkdir -m 700 "$output/console-download-adapter" "$output/console-download-tmp" "$output/console-download-requests"
  cat >"$output/console-download-adapter/curl" <<'ADAPTER'
#!/usr/bin/env bash
set -euo pipefail
exec "$CONSOLE_ORIGINALS_TOOL" native-request --capsule "$CONSOLE_ORIGINALS_CAPSULE" --sha256 "$CONSOLE_ORIGINALS_SHA256" --temp-root "$CONSOLE_ORIGINALS_TMP" --requests "$CONSOLE_ORIGINALS_REQUESTS" -- "$@"
ADAPTER
  chmod 700 "$output/console-download-adapter/curl"
  shasum -a 256 "$console_tool" "$output/console-download-adapter/curl" "$root/scripts/zitadel-console-assets.go" console/scripts/install-proto-plugins.sh >"$output/console-download-tools.sha256"
  run_phase console env PATH="$output/console-download-adapter:$PATH" TMPDIR="$output/console-download-tmp" \
    CONSOLE_ORIGINALS_TOOL="$console_tool" CONSOLE_ORIGINALS_CAPSULE="$console_inputs" CONSOLE_ORIGINALS_SHA256="$console_sha" \
    CONSOLE_ORIGINALS_TMP="$output/console-download-tmp" CONSOLE_ORIGINALS_REQUESTS="$output/console-download-requests" \
    pnpm exec nx run @zitadel/api:build-console --parallel=1 --skip-nx-cache --no-cloud
  shasum -a 256 -c "$output/console-download-tools.sha256" >/dev/null || fail 'console source or compiled adapter changed during native consumption'
  "$console_tool" verify --capsule "$console_inputs" --sha256 "$console_sha" >"$output/console-originals.after.json"
  cmp "$output/console-originals.before.json" "$output/console-originals.after.json" || fail 'console original inputs changed during native consumption'
  "$console_tool" consumption --capsule "$console_inputs" --sha256 "$console_sha" --requests "$output/console-download-requests" >"$output/console-download-consumption.json"
  cmp "$console_inputs/protoc-gen-grpc-web" .artifacts/bin/linux/amd64/protoc-gen-grpc-web || fail 'installed grpc-web differs from the retained original'
  cmp "$console_inputs/protoc-gen-openapiv2" .artifacts/bin/linux/amd64/protoc-gen-openapiv2 || fail 'console did not install the retained OpenAPI 2.22.0 original'
  console_downloads_json="$(jq --arg source "${source_manifest:-$prepared_sha}" \
    --arg helper "$(shasum -a 256 "$root/scripts/zitadel-console-assets.go" | awk '{print $1}')" \
    --arg adapter "$(shasum -a 256 "$console_tool" | awk '{print $1}')" \
    --arg launcher "$(shasum -a 256 "$output/console-download-adapter/curl" | awk '{print $1}')" \
    --arg installer "$(shasum -a 256 console/scripts/install-proto-plugins.sh | awk '{print $1}')" \
    '. + {sourceManifestSha256:$source,helperSourceSha256:$helper,adapterSha256:$adapter,launcherSha256:$launcher,installerSha256:$installer}' "$output/console-download-consumption.json")"
else
  run_phase console pnpm exec nx run @zitadel/api:build-console --parallel=1 --skip-nx-cache --no-cloud
fi
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
if [[ -n "$prepared_source" ]]; then
  jq -e --arg recipe "$recipe_hash" '.recipeSha256 == $recipe' <<<"$prepared_input_json" >/dev/null || fail 'prepared source recipe changed during build'
fi
if [[ "$capture_source" -eq 1 ]]; then
  "$module_verifier" verify-generator-checkpoint --checkpoint "$output/generator-checkpoint.json" --sha256 "$generator_checkpoint" --source-sha256 "$source_manifest" >"$output/generator-checkpoint-final.json"
  cmp "$output/generator-checkpoint-receipt.json" "$output/generator-checkpoint-final.json" || fail 'native generator checkpoint changed during build'
  # Authenticate selected generator inputs using only this build's signed
  # checksum lookup/tile cache. The verifier has no network fallback or key flag.
  "$module_verifier" capture-generator-sources --build "$output" --checkpoint-sha256 "$generator_checkpoint" --source-sha256 "$source_manifest" --output "$output/selected-generator-go-sources" >"$output/generator-source-capture.pending"
  jq -e --arg source "$source_manifest" --arg checkpoint "$generator_checkpoint" '.schemaVersion == 1 and .sourceManifestSha256 == $source and .checkpointSha256 == $checkpoint and .modules > 0 and .proofFiles > 0 and .files > 0 and .selectedGeneratorGoSourcesCaptured == true and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/generator-source-capture.pending" >/dev/null || fail 'incomplete selected generator source capture'
  generator_sources="$(jq -er .manifestSha256 "$output/generator-source-capture.pending")"
  "$module_verifier" verify-generator-sources --package "$output/selected-generator-go-sources" --sha256 "$generator_sources" --source-sha256 "$source_manifest" >"$output/generator-source-readback.json"
  cmp "$output/generator-source-capture.pending" "$output/generator-source-readback.json" || fail 'selected generator source readback differs'
  # The source capsule retains original registry archives, not generated install
  # outputs or a global cache. Its native SDK is acquired separately by CI.
  pnpm_tool="$root/tools/zitadel-pnpm-source/index.mjs"
  node "$pnpm_tool" capture --build "$output" --source-sha256 "$source_manifest" --output "$output/selected-pnpm-sources" --scratch "$output/pnpm-source-work" >"$output/pnpm-source-capture.pending"
  jq -e --arg source "$source_manifest" '.schemaVersion == 1 and .sourceManifestSha256 == $source and .packages > 0 and .archives > 0 and .files > 0 and .selectedPNPMSourcesCaptured == true and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/pnpm-source-capture.pending" >/dev/null || fail 'incomplete selected pnpm source capture'
  pnpm_manifest="$(jq -er .manifestSha256 "$output/pnpm-source-capture.pending")"
  node "$pnpm_tool" verify --capsule "$output/selected-pnpm-sources" --sha256 "$pnpm_manifest" --scratch "$output/pnpm-source-readback-work" >"$output/pnpm-source-readback.pending"
  cmp "$output/pnpm-source-capture.pending" "$output/pnpm-source-readback.pending" || fail 'selected pnpm source readback differs'
  for member in index.mjs package.json package-lock.json; do
    cmp "$root/tools/zitadel-pnpm-source/$member" "$output/source-snapshot/records/recipes/zitadel-pnpm-source/$member" || fail 'native pnpm adapter changed during build'
  done
  # Retain only the complete service-selected graph, after compilation and the
  # final module verification. The command's checksum-bound dependency is
  # tooling, not an extra service module or a complete offline-replay closure.
  "$module_verifier" --capture --build "$output" --catalogue "$root/third_party/zitadel-go-module-notices/catalogue.json" --output "$output/selected-go-sources" >"$output/go-module-source-capture.pending"
  jq -e '.schemaVersion == 1 and .modules == 231 and .files == 466 and .selectedServiceGoSourcesCaptured == true and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/go-module-source-capture.pending" >/dev/null || fail 'incomplete selected Go source capture'
  go_modules_manifest="$(jq -er .manifestSha256 "$output/go-module-source-capture.pending")"
  "$module_verifier" --verify "$output/selected-go-sources" --sha256 "$go_modules_manifest" >"$output/go-module-source-readback.pending"
  cmp "$output/go-module-source-capture.pending" "$output/go-module-source-readback.pending" || fail 'selected Go source readback differs'
  [[ "$(shasum -a 256 "$module_verifier" | awk '{print $1}')" = "$module_verifier_hash" ]] || fail 'module verifier changed during build'
  shasum -a 256 "$module_verifier" "$root/server/cmd/zitadel-module-sources/"*.go "$root/server/go.mod" "$root/server/go.sum" >"$output/go-module-source-verifier.sha256"
  check_bsr_tools
  mkdir -m 700 "$output/bsr-records" "$output/bsr-exports"
  for selected in \
    envoyproxy/protoc-gen-validate:6607b10f00ed4a3d98f906807131c44a \
    googleapis/googleapis:75b4300737fb4efca0831636be94e517 \
    grpc-ecosystem/grpc-gateway:a1ecdc58eccd49aa8bea2a7a9022dc27; do
    owner_module="${selected%%:*}" commit="${selected##*:}"
    mkdir -p "$output/bsr-exports/${owner_module%/*}"
    "$output/bsr-tools/api" registry module commit info "buf.build/$selected" --format json --timeout 45s >"$output/bsr-records/$commit.json"
    "$output/bsr-tools/api" export "buf.build/$selected" --all --exclude-imports --disable-symlinks --timeout 60s --output "$output/bsr-exports/$owner_module"
  done
  "$bsr_verifier" capture --build "$output" --recipe "$root/scripts/build-zitadel-linux.sh" --source-sha256 "$source_manifest" --output "$output/selected-bsr-sources" >"$output/bsr-source-capture.pending"
  jq -e '.schemaVersion == 1 and .modules == 3 and .files == 170 and .cacheFiles == 113 and .exportFiles == 49 and .selectedBSRSourcesCaptured == true and .dependencyInputsCaptured == false and .nativeOfflineReplayProved == false and (.manifestSha256 | test("^[0-9a-f]{64}$"))' "$output/bsr-source-capture.pending" >/dev/null || fail 'incomplete selected schema source capture'
  bsr_manifest="$(jq -er .manifestSha256 "$output/bsr-source-capture.pending")"
  "$bsr_verifier" verify --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" >"$output/bsr-source-readback.pending"
  cmp "$output/bsr-source-capture.pending" "$output/bsr-source-readback.pending" || fail 'selected schema source readback differs'
  jq -e --arg api "$(awk 'NR == 1 {print $1}' "$output/bsr-tool-copies.sha256")" \
    --arg console "$(awk 'NR == 2 {print $1}' "$output/bsr-tool-copies.sha256")" \
    '.tools == [{phase:"api",version:"1.67.0",sha256:$api},{phase:"console",version:"1.72.0",sha256:$console}]' \
    "$output/selected-bsr-sources/manifest.json" >/dev/null || fail 'sealed schema tool identities differ from the native build'
  # The restored source uses the original closed archive, not a generated tree.
  archive_hash="$(jq -er .archiveSha256 "$output/source-archive.pending")"
  go run "$root/scripts/zitadel-source-bundle.go" --unpack "$output/prepared-source.tar" "$archive_hash" "$source_manifest" "$output/bsr-workspace" >"$output/bsr-workspace-roundtrip.pending"
  [[ "$(id -u)" -gt 0 ]] || fail 'schema replay requires a non-root caller'
  [[ "$(without_network id -u)" = "$(id -u)" ]] || fail 'network namespace did not retain the unprivileged user'
  curl --fail --silent --show-error --connect-timeout 5 --max-time 10 --output /dev/null https://buf.build || fail 'reachable network control failed'
  if without_network curl --fail --silent --show-error --connect-timeout 5 --max-time 10 --output /dev/null https://buf.build; then fail 'network-denied control reached the network'; fi
  : >"$output/bsr-replay-compilers.pending"
  for phase in api console; do
    check_bsr_tools
    for state in connected offline; do
      cache="$output/bsr-$phase-$state-cache"
      "$bsr_verifier" restore --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" --output "$cache" >/dev/null
      "$bsr_verifier" check-cache --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" --cache "$cache" >/dev/null
      if [[ "$state" = connected ]]; then
        env BUF_CACHE_DIR="$cache" "$output/bsr-tools/$phase" build "$output/bsr-workspace/source/proto" --exclude-source-info --output "$output/bsr-$phase-$state.binpb"
      else
        without_network env BUF_CACHE_DIR="$cache" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" "$output/bsr-tools/$phase" build "$output/bsr-workspace/source/proto" --exclude-source-info --output "$output/bsr-$phase-$state.binpb"
      fi
      [[ -s "$output/bsr-$phase-$state.binpb" ]] || fail 'schema descriptor is empty'
    done
    cmp "$output/bsr-$phase-connected.binpb" "$output/bsr-$phase-offline.binpb" || fail 'offline schema compilation differs'
    mkdir -m 700 "$output/bsr-$phase-empty-cache"
    if without_network env BUF_CACHE_DIR="$output/bsr-$phase-empty-cache" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" "$output/bsr-tools/$phase" build "$output/bsr-workspace/source/proto" --exclude-source-info --output "$output/bsr-$phase-empty.binpb"; then fail 'empty cache compiled imported schemas without networking'; fi
    for mutation in missing changed; do
      cache="$output/bsr-$phase-$mutation-cache"
      "$bsr_verifier" restore --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" --output "$cache" >/dev/null
      "$bsr_verifier" check-cache --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" --cache "$cache" >/dev/null
      member="$cache/v3/modules/b5/buf.build/googleapis/googleapis/75b4300737fb4efca0831636be94e517/files/google/api/annotations.proto"
      [[ -f "$member" && ! -L "$member" ]] || fail 'schema refusal control member is absent'
      if [[ "$mutation" = missing ]]; then rm "$member"; else printf 'changed\n' >"$member"; fi
      if "$bsr_verifier" check-cache --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" --cache "$cache" >/dev/null; then fail 'altered schema cache was accepted'; fi
    done
    descriptor="$(shasum -a 256 "$output/bsr-$phase-offline.binpb" | awk '{print $1}')"
    jq -n --arg phase "$phase" --arg version "$(cat "$output/bsr-tools/$phase.version")" --arg descriptor "$descriptor" '{phase:$phase,version:$version,descriptorSha256:$descriptor,connectedMatchesOffline:true,emptyCacheRefused:true,missingMemberRefused:true,changedMemberRefused:true}' >>"$output/bsr-replay-compilers.pending"
  done
  go run "$root/scripts/zitadel-source-bundle.go" --verify "$output/bsr-workspace" "$source_manifest" >/dev/null
  check_bsr_tools
  "$bsr_verifier" verify --package "$output/selected-bsr-sources" --sha256 "$bsr_manifest" >"$output/bsr-source-final.pending"
  cmp "$output/bsr-source-readback.pending" "$output/bsr-source-final.pending" || fail 'sealed schema package changed during replay'
  [[ "$(shasum -a 256 "$bsr_verifier" | awk '{print $1}')" = "$bsr_verifier_hash" ]] || fail 'schema verifier changed during build'
  shasum -a 256 "$bsr_verifier" "$root/server/cmd/zitadel-bsr-sources/"*.go >"$output/bsr-source-verifier.sha256"
  jq -s --arg source "$source_manifest" --arg package "$bsr_manifest" --arg recipe "$recipe_hash" '
    if length == 2 and map(.phase) == ["api","console"]
    then {schemaVersion:1,platform:"linux/amd64",sourceManifestSha256:$source,bsrManifestSha256:$package,recipeSha256:$recipe,compilers:.,networkDenialProved:true,nonRootReplay:true,noNewPrivilegesReplay:true,schemaOfflineReplayProved:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}
    else error("incomplete schema compiler replay") end' "$output/bsr-replay-compilers.pending" >"$output/bsr-schema-replay.json"
  go run "$root/scripts/zitadel-source-bundle.go" --verify "$output/source-snapshot" "$source_manifest" >"$output/source-readback.pending"
  archive_hash="$(shasum -a 256 "$output/prepared-source.tar" | awk '{print $1}')"
  jq -e --arg archive "$archive_hash" --arg manifest "$source_manifest" '.archiveSha256 == $archive and .manifestSha256 == $manifest' "$output/source-archive.pending" >/dev/null || fail 'prepared-source archive changed during build'
  jq -e --arg recipe "$recipe_hash" '.recipeSha256 == $recipe' "$output/source-capture.pending" >/dev/null || fail 'captured source recipe changed during build'
  source_snapshot_json="$(jq -n --arg verifier "$module_verifier_hash" --arg bsrVerifier "$bsr_verifier_hash" --slurpfile capture "$output/source-capture.pending" --slurpfile archive "$output/source-archive.pending" --slurpfile modules "$output/go-module-source-readback.pending" --slurpfile bsr "$output/bsr-source-readback.pending" --slurpfile pnpm "$output/pnpm-source-readback.pending" --slurpfile generators "$output/generator-checkpoint-receipt.json" --slurpfile generatorSources "$output/generator-source-readback.json" --slurpfile replay "$output/bsr-schema-replay.json" '$capture[0] + $archive[0] + {goModules:$modules[0],goModuleVerifier:{go:"1.27.3",sha256:$verifier},generatorBuildMetadata:$generators[0],generatorGoSources:$generatorSources[0],bsrSources:$bsr[0],bsrVerifier:{go:"1.27.3",sha256:$bsrVerifier},pnpmSources:$pnpm[0],bsrSchemaReplay:$replay[0],dependencyInputsCaptured:false,nativeOfflineReplayProved:false}')"
fi
jq -n --arg pin "$pin" --arg version "$version" --arg date "$source_date" --arg binary "$binary_hash" --arg recipe "$recipe_hash" \
  --argjson snapshot "$source_snapshot_json" --argjson sourceInput "$prepared_input_json" --argjson consoleDownloads "$console_downloads_json" \
  --argjson elapsed "$(($(date -u +%s) - started))" --slurpfile preparation "$output/prepared/source-preparation.json" \
    '{schemaVersion:1,upstreamCommit:$pin,version:$version,sourceDate:$date,platform:"linux/amd64",profile:"api-login-v1-console",recipeSha256:$recipe,binarySha256:$binary,elapsedSeconds:$elapsed,preparation:$preparation[0],tools:{go:"1.26.10",node:"22.23.3",pnpm:"10.30.3"},imageAdmission:"unknown",licenseClosure:"unknown",reproducible:false} + (if $snapshot == null then {} else {sourceSnapshot:$snapshot} end) + (if $sourceInput == null then {} else {sourceInput:$sourceInput} end) + (if $consoleDownloads == null then {} else {consoleDownloads:$consoleDownloads} end)' >"$output/build-receipt.pending"
[[ "$(go env GOROOT)" = "$go_root" && "$(go env GOVERSION)" = go1.26.10 ]] || fail 'selected Go compiler changed'
go run "$root/scripts/zitadel-console-notices.go" --inventory --build "$output" --recipe "$root/scripts/build-zitadel-linux.sh" --supplement "$root/third_party/zitadel-console-notices" --toolchain "$go_root" --source-license --modules "$root/third_party/zitadel-go-module-notices" >"$output/console-notices-inventory.pending"
go run "$root/scripts/zitadel-login-icons.go" --inventory --build "$output" --notices "$root/third_party/zitadel-login-icons" >"$output/login-icons-inventory.pending"
mv "$output/login-icons-inventory.pending" "$output/login-icons-inventory.json"
mv "$output/console-notices-inventory.pending" "$output/console-notices-inventory.json"
mv "$output/console-notices-supplement-inventory.pending" "$output/console-notices-supplement-inventory.json"
mv "$output/build-receipt.pending" "$output/build-receipt.json"
complete=1
printf 'Complete native build evidence only: %s\n' "$output"
