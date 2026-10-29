#!/usr/bin/env bash
# Explicit native input replay; never an image publication or activation command.
set -euo pipefail
umask 022
export LC_ALL=C
[[ "$#" -gt 0 ]] || exit 0
fail() { printf 'zitadel-replay: %s\n' "$1" >&2; exit 1; }
build='' module_verifier='' bsr_verifier='' verifier_go='' output=''
while [[ "$#" -gt 0 ]]; do
  [[ "$#" -ge 2 && -n "$2" ]] || fail 'complete explicit replay arguments required'
  case "$1" in
    --build) [[ -z "$build" ]] || fail 'duplicate build'; build="$2" ;;
    --module-source-verifier) [[ -z "$module_verifier" ]] || fail 'duplicate module verifier'; module_verifier="$2" ;;
    --bsr-source-verifier) [[ -z "$bsr_verifier" ]] || fail 'duplicate schema verifier'; bsr_verifier="$2" ;;
    --verifier-go) [[ -z "$verifier_go" ]] || fail 'duplicate verifier compiler'; verifier_go="$2" ;;
    --output) [[ -z "$output" ]] || fail 'duplicate output'; output="$2" ;;
    *) fail 'unsupported explicit replay argument' ;;
  esac
  shift 2
done
[[ -n "$build" && -n "$module_verifier" && -n "$bsr_verifier" && -n "$verifier_go" && -n "$output" ]] || fail 'complete explicit replay arguments required'
for executable in "$module_verifier" "$bsr_verifier" "$verifier_go"; do
  [[ "$executable" = /* && -f "$executable" && -x "$executable" && ! -L "$executable" ]] || fail 'original verifier and compiler inputs must be regular executables'
done
[[ "$(GOTOOLCHAIN=local GOENV=off GOWORK=off "$verifier_go" env GOVERSION)" = go1.27.3 ]] || fail 'original Go 1.27.3 verifier compiler required'
verifier_go_sha="$(shasum -a 256 "$verifier_go" | awk '{print $1}')"
[[ "$(uname -s)" = Linux && "$(uname -m)" = x86_64 && "$(id -u)" != 0 ]] || fail 'native nonroot Linux amd64 required'
for tool in go node jq curl sudo unshare setpriv timeout readlink shasum; do command -v "$tool" >/dev/null || fail 'required native replay tool missing'; done
[[ "$(GOTOOLCHAIN=local go env GOVERSION)" = go1.26.10 ]] || fail 'frozen compiler required'
[[ "$build" = /* && -d "$build" && ! -L "$build" && "$build" = "$(cd "$build" && pwd -P)" ]] || fail 'physical build input required'
[[ "$output" = /* && ! -e "$output" && ! -L "$output" ]] || fail 'new absolute output required'
parent="$(cd "$(dirname "$output")" && pwd -P)" name="$(basename "$output")"
[[ "$name" != . && "$name" != .. ]] || fail 'invalid output name'
output="$parent/$name"
[[ "$output" != "$build" && "$output" != "$build/"* ]] || fail 'output must be outside the original build'
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
mkdir -m 700 "$output"
complete=0
trap 'if [[ "$complete" != 1 ]]; then printf "Incomplete replay retained; no success receipt.\n" >&2; fi' EXIT
trap 'exit 1' HUP INT TERM
export GOTOOLCHAIN=local GOENV=off GOWORK=off
go run "$root/scripts/zitadel-source-bundle.go" --seal-inputs "$build" "$output/retained-inputs" >"$output/input-retention.json"
seal="$(jq -er .manifestSha256 "$output/input-retention.json")"
go run "$root/scripts/zitadel-source-bundle.go" --pack "$output/retained-inputs" "$seal" "$output/inputs.tar" >"$output/input-archive.json"
archive="$(jq -er .archiveSha256 "$output/input-archive.json")"
go run "$root/scripts/zitadel-source-bundle.go" --unpack "$output/inputs.tar" "$archive" "$seal" "$output/roundtrip" >"$output/input-roundtrip.json"
cmp "$output/retained-inputs/source-manifest.json" "$output/roundtrip/source-manifest.json" || fail 'complete input membership changed'
original="$output/roundtrip/source/build-receipt.json"
jq -e --arg module "$(shasum -a 256 "$module_verifier" | awk '{print $1}')" --arg bsr "$(shasum -a 256 "$bsr_verifier" | awk '{print $1}')" --arg compiler "$verifier_go_sha" \
  '.sourceSnapshot.goModuleVerifier.sha256==$module and .sourceSnapshot.bsrVerifier.sha256==$bsr and .sourceSnapshot.verifierCompiler.go=="1.27.3" and .sourceSnapshot.verifierCompiler.sha256==$compiler' "$original" >/dev/null || fail 'original verifier or compiler identities differ'
caller_uid="$(id -u)" caller_gid="$(id -g)" parent_net="$(readlink /proc/self/ns/net)"
mkdir -m 700 "$output/driver-cache" "$output/driver-modules" "$output/driver-path"
curl --fail --silent --show-error --connect-timeout 5 --max-time 10 --output /dev/null https://proxy.golang.org || fail 'reachable network control failed'
# The entire unchanged install/generate/compile/notices pipeline runs in a new
# network namespace. Drop all capabilities and ambient configuration first.
if ! timeout 1800 sudo -n unshare --net -- setpriv --reuid "$caller_uid" --regid "$caller_gid" \
  --clear-groups --no-new-privs --bounding-set=-all -- \
  env -i PATH="$PATH" LC_ALL=C GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=sum.golang.org GOVCS='*:off' \
    GOCACHE="$output/driver-cache" GOMODCACHE="$output/driver-modules" GOPATH="$output/driver-path" \
    bash -s -- "$caller_uid" "$caller_gid" "$parent_net" "$output/network-boundary.txt" \
      "$output" "$root" "$verifier_go" "$module_verifier" "$seal" "$verifier_go_sha" \
      >"$output/native-replay.log" 2>&1 <<'REPLAY_CHILD'
set -euo pipefail
[[ "$(id -u)" = "$1" && "$(id -g)" = "$2" && "$1" != 0 && "$(readlink /proc/self/ns/net)" != "$3" ]]
awk '$1=="NoNewPrivs:" {n=($2==1)} $1=="CapEff:" {e=($2=="0000000000000000")} $1=="CapBnd:" {b=($2=="0000000000000000")} END {exit !(n&&e&&b)}' /proc/self/status
if curl --fail --silent --show-error --connect-timeout 3 --max-time 5 --output /dev/null https://proxy.golang.org; then exit 125; fi
printf 'verified\n' >"$4"
shift 4
output="$1" root="$2" verifier_go="$3" original_module="$4" seal="$5" compiler_sha="$6"
original="$output/roundtrip/source/build-receipt.json"
snapshot="$output/roundtrip/source/source-snapshot"
source_sha="$(jq -er .sourceSnapshot.manifestSha256 "$original")"
bootstrap="$output/roundtrip/source/selected-verifier-bootstrap"
bootstrap_sha="$(jq -er .sourceSnapshot.verifierBootstrap.manifestSha256 "$original")"
[[ "$(shasum -a 256 "$verifier_go" | awk '{print $1}')" = "$compiler_sha" && "$(GOTOOLCHAIN=local "$verifier_go" env GOVERSION)" = go1.27.3 ]]
# Match retained code to the reviewed caller before compiling or running it.
notice_root="$output/notice-originals-bootstrap"
go run "$root/scripts/zitadel-source-bundle.go" --restore-notice-originals "$output/roundtrip/source/selected-notice-originals" "$snapshot" "$source_sha" "$notice_root" >"$output/notice-originals-bootstrap.json"
go run "$root/scripts/zitadel-source-bundle.go" --verify-notice-originals "$notice_root" "$snapshot" "$source_sha" >"$output/notice-originals-bootstrap-readback.json"
cmp "$output/notice-originals-bootstrap.json" "$output/notice-originals-bootstrap-readback.json"
jq -e --slurpfile original "$original" '.files==43 and .originalInputsReadbackPassed==true and .==$original[0].sourceSnapshot.noticeOriginals' "$output/notice-originals-bootstrap.json" >/dev/null
go run "$root/scripts/zitadel-source-bundle.go" --verify-build-records "$snapshot" "$source_sha" "$root" "$notice_root" >/dev/null
go run "$root/scripts/zitadel-source-bundle.go" --restore-tools "$snapshot" "$source_sha" "$output/verifier-tool-source" >"$output/verifier-tool-source.json"
jq -e --slurpfile original "$original" '.==$original[0].sourceSnapshot.toolSource' "$output/verifier-tool-source.json" >/dev/null
tools_sha="$(jq -er .toolSourceSha256 "$output/verifier-tool-source.json")"
"$original_module" restore-verifier-bootstrap --tool-source "$output/verifier-tool-source" --source-sha256 "$source_sha" --tool-source-sha256 "$tools_sha" \
  --package "$bootstrap" --sha256 "$bootstrap_sha" --output "$output/verifier-modules" >"$output/verifier-bootstrap-restored.json"
jq -e --slurpfile original "$original" '.verifierBootstrapRestored==true and .goVerifiersRebuilt==false and (. + {verifierBootstrapRestored:false})==$original[0].sourceSnapshot.verifierBootstrap' "$output/verifier-bootstrap-restored.json" >/dev/null
mkdir -m 700 "$output/verifier-build-cache" "$output/verifier-go-path" "$output/verifier-bin" "$output/empty-verifier-modules" "$output/empty-verifier-build-cache"
verifier_go_run() {
  local modules="$1" cache="$2"; shift 2
  env GOENV=off GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off \
    GOMODCACHE="$modules" GOCACHE="$cache" GOPATH="$output/verifier-go-path" "$verifier_go" "$@"
}
cd "$output/verifier-tool-source/server"
verifier_go_run "$output/verifier-modules" "$output/verifier-build-cache" list -json ./cmd/zitadel-module-sources ./cmd/zitadel-bsr-sources >"$output/verifier-packages.jsonl"
jq -s -e 'if length==2 and all(.[]; .Name=="main" and .Incomplete!=true and .Error==null and ((.DepsErrors // [])|length)==0) then map({ImportPath,Name,GoFiles}) else error("incomplete reconstructed verifier composition") end' "$output/verifier-packages.jsonl" >"$output/verifier-composition.json"
cmp "$bootstrap/records/composition.json" "$output/verifier-composition.json"
for command in zitadel-module-sources zitadel-bsr-sources; do
  verifier_go_run "$output/verifier-modules" "$output/verifier-build-cache" build -trimpath -buildvcs=false -o "$output/verifier-bin/$command" "./cmd/$command"
done
module_sha="$(shasum -a 256 "$output/verifier-bin/zitadel-module-sources" | awk '{print $1}')"
bsr_sha="$(shasum -a 256 "$output/verifier-bin/zitadel-bsr-sources" | awk '{print $1}')"
jq -e --arg module "$module_sha" --arg bsr "$bsr_sha" '.sourceSnapshot.goModuleVerifier.sha256==$module and .sourceSnapshot.bsrVerifier.sha256==$bsr' "$original" >/dev/null
# A positive real build precedes the cold-cache control, so unrelated compiler
# or source failures cannot masquerade as a successful missing-input refusal.
if verifier_go_run "$output/empty-verifier-modules" "$output/empty-verifier-build-cache" build -trimpath -buildvcs=false -o "$output/unexpected-verifier" ./cmd/zitadel-module-sources >"$output/empty-verifier-cache.log" 2>&1; then exit 126; fi
[[ ! -e "$output/unexpected-verifier" ]]
grep -q 'module lookup disabled by GOPROXY=off' "$output/empty-verifier-cache.log"
rebuilt_module="$output/verifier-bin/zitadel-module-sources"
"$rebuilt_module" verify-verifier-bootstrap --tool-source "$output/verifier-tool-source" --source-sha256 "$source_sha" --tool-source-sha256 "$tools_sha" --package "$bootstrap" --sha256 "$bootstrap_sha" >"$output/verifier-bootstrap-readback.json"
jq -e --slurpfile original "$original" '.==$original[0].sourceSnapshot.verifierBootstrap' "$output/verifier-bootstrap-readback.json" >/dev/null
# Only these rebuilt programs reach the real service pipeline. Original inputs
# authenticate bootstrap capture; they are never an executable replay fallback.
replay_root="$output/verifier-tool-source"
# The real native caller sees only authenticated authored tools and terms. None
# of the five ambient checkout notice families exists in this source layout.
for family in zitadel-console-notices zitadel-go-module-notices zitadel-login-icons zitadel-text-fonts zitadel-copied-assets; do
  [[ ! -e "$replay_root/third_party/$family" && ! -L "$replay_root/third_party/$family" ]]
done
cd "$replay_root"
bash "$replay_root/scripts/build-zitadel-linux.sh" --build --retained-inputs "$output/roundtrip" --retained-sha256 "$seal" \
  --pnpm-source-sdk "$root/tools/zitadel-pnpm-source" \
  --module-source-verifier "$output/verifier-bin/zitadel-module-sources" --bsr-source-verifier "$output/verifier-bin/zitadel-bsr-sources" --output "$output/native"
go run "$replay_root/scripts/zitadel-source-bundle.go" --verify-notice-originals "$notice_root" "$snapshot" "$source_sha" >"$output/notice-originals-bootstrap-final.json"
cmp "$output/notice-originals-bootstrap.json" "$output/notice-originals-bootstrap-final.json"
go run "$root/scripts/zitadel-source-bundle.go" --verify-tools "$snapshot" "$source_sha" "$output/verifier-tool-source" >"$output/verifier-tool-source-final.json"
cmp "$output/verifier-tool-source.json" "$output/verifier-tool-source-final.json"
"$rebuilt_module" verify-verifier-bootstrap --tool-source "$output/verifier-tool-source" --source-sha256 "$source_sha" --tool-source-sha256 "$tools_sha" --package "$bootstrap" --sha256 "$bootstrap_sha" >"$output/verifier-bootstrap-final.json"
cmp "$output/verifier-bootstrap-readback.json" "$output/verifier-bootstrap-final.json"
[[ "$(shasum -a 256 "$output/verifier-bin/zitadel-module-sources" | awk '{print $1}')" = "$module_sha" && "$(shasum -a 256 "$output/verifier-bin/zitadel-bsr-sources" | awk '{print $1}')" = "$bsr_sha" && "$(shasum -a 256 "$verifier_go" | awk '{print $1}')" = "$compiler_sha" ]]
jq -n --arg source "$source_sha" --arg bootstrap "$bootstrap_sha" --arg compiler "$compiler_sha" --arg module "$module_sha" --arg bsr "$bsr_sha" \
  '{schemaVersion:1,profile:"identity-source-verifier-reconstruction-v1",sourceManifestSha256:$source,bootstrapManifestSha256:$bootstrap,compilerSha256:$compiler,go:"1.27.3",moduleSha256:$module,bsrSha256:$bsr,goVerifiersRebuilt:true,originalBinaryIdentitiesMatched:true,compositionsMatched:true,emptyCacheRefused:true}' >"$output/verifier-reconstruction.json"
REPLAY_CHILD
then
  tail -80 "$output/native-replay.log" >&2
  fail 'complete native replay failed'
fi
[[ "$(cat "$output/network-boundary.txt")" = verified ]] || fail 'kernel boundary was not verified'
native="$output/native/build-receipt.json"
jq -e --slurpfile original "$original" '.schemaVersion==1 and .profile=="identity-source-verifier-reconstruction-v1" and .go=="1.27.3" and .compilerSha256==$original[0].sourceSnapshot.verifierCompiler.sha256 and .sourceManifestSha256==$original[0].sourceSnapshot.manifestSha256 and .bootstrapManifestSha256==$original[0].sourceSnapshot.verifierBootstrap.manifestSha256 and .moduleSha256==$original[0].sourceSnapshot.goModuleVerifier.sha256 and .bsrSha256==$original[0].sourceSnapshot.bsrVerifier.sha256 and .goVerifiersRebuilt==true and .originalBinaryIdentitiesMatched==true and .compositionsMatched==true and .emptyCacheRefused==true' "$output/verifier-reconstruction.json" >/dev/null || fail 'native verifier reconstruction join differs'
[[ "$(shasum -a 256 "$verifier_go" | awk '{print $1}')" = "$verifier_go_sha" ]] || fail 'original verifier compiler changed'
jq -e --slurpfile original "$original" '
 .schemaVersion==1 and .retainedInputsRestored==true and .recipeSha256==$original[0].recipeSha256 and
 .sourceInput.manifestSha256==$original[0].sourceSnapshot.manifestSha256 and
 .noticeOriginals==$original[0].sourceSnapshot.noticeOriginals and .noticeOriginals.files==43 and .noticeOriginals.originalInputsReadbackPassed==true and
 .preparation==$original[0].preparation and .upstreamCommit==$original[0].upstreamCommit and
 .tools==$original[0].tools and .platform=="linux/amd64" and .profile=="api-login-v1-console" and
 .consoleDownloads.manifestSha256==$original[0].consoleDownloads.manifestSha256 and
 .consoleDownloads.originalConsoleDownloadsConsumed==true and
 .imageAdmission=="unknown" and .licenseClosure=="unknown" and .reproducible==false
' "$native" >/dev/null || fail 'native replay source or recipe join differs'
jq -e --slurpfile original "$original" --slurpfile restored "$output/native/retained-go-restoration.json" \
  --slurpfile service "$output/roundtrip/source/selected-go-sources/manifest.json" '
  .schemaVersion==1 and .sourceManifestSha256==$original[0].sourceSnapshot.manifestSha256 and
  .restorationManifestSha256==$restored[0].manifestSha256 and
  .serviceModules==([$service[0].modules[].effective|[.Path,.Version]]|unique|length) and .serviceModules>0 and
  .nativeCacheReadbackPassed==true and .sourceChecksumsUnchanged==true and
  .dependencyInputsCaptured==false and .nativeOfflineReplayProved==false
' "$output/native/retained-go-service-readback.json" >/dev/null || fail 'native service checksum readback differs'
for notice in console-notices-inventory.json console-notices-supplement-inventory.json login-icons-inventory.json text-font-notices-inventory.json copied-asset-notices-inventory.json; do
  [[ -s "$output/native/$notice" && ! -L "$output/native/$notice" ]] || fail 'complete same-build notice inventory missing'
done
go run "$root/scripts/zitadel-source-bundle.go" --verify "$output/roundtrip" "$seal" >/dev/null || fail 'input archive changed during replay'
console_notice_sha="$(shasum -a 256 "$output/native/console-notices-inventory.json" | awk '{print $1}')"
supplement_notice_sha="$(shasum -a 256 "$output/native/console-notices-supplement-inventory.json" | awk '{print $1}')"
login_notice_sha="$(shasum -a 256 "$output/native/login-icons-inventory.json" | awk '{print $1}')"
font_notice_sha="$(shasum -a 256 "$output/native/text-font-notices-inventory.json" | awk '{print $1}')"
copied_notice_sha="$(shasum -a 256 "$output/native/copied-asset-notices-inventory.json" | awk '{print $1}')"
for notice_sha in "$console_notice_sha" "$supplement_notice_sha" "$login_notice_sha" "$font_notice_sha" "$copied_notice_sha"; do
  [[ "$notice_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'complete notice inventory digest required'
done
# A failed read/write leaves only private, unsealed diagnosis. The uploader does
# not admit pending output; publish the receipt name only after the writer closes.
jq -n --arg input "$seal" --arg archive "$archive" --slurpfile native "$native" --slurpfile verifiers "$output/verifier-reconstruction.json" \
  --arg console "$console_notice_sha" --arg supplement "$supplement_notice_sha" --arg login "$login_notice_sha" --arg fonts "$font_notice_sha" --arg copied "$copied_notice_sha" '
 {schemaVersion:1,profile:"native-retained-input-replay-v1",platform:"linux/amd64",inputManifestSha256:$input,inputArchiveSha256:$archive,
 sourceManifestSha256:$native[0].sourceInput.manifestSha256,recipeSha256:$native[0].recipeSha256,binarySha256:$native[0].binarySha256,
 noticeInventories:{console:$console,supplement:$supplement,login:$login,textFonts:$fonts,copiedAssets:$copied},noticeOriginals:$native[0].noticeOriginals,checkoutNoticeOriginalsUnavailable:true,
 networkDenied:true,nonRoot:true,noNewPrivileges:true,capabilitiesDropped:true,
 verifierReconstruction:$verifiers[0],
 dependencyInputsCaptured:true,nativeOfflineReplayProved:true,reproducible:false,imageAdmission:"unknown",licenseClosure:"unknown"}
' >"$output/replay-receipt.pending"
mv "$output/replay-receipt.pending" "$output/replay-receipt.json"
complete=1
printf 'Complete native retained-input replay passed; publication and activation remain separate.\n'
