#!/usr/bin/env bash
# Explicit native input replay; never an image publication or activation command.
set -euo pipefail
umask 022
export LC_ALL=C
[[ "$#" -gt 0 ]] || exit 0
fail() { printf 'zitadel-replay: %s\n' "$1" >&2; exit 1; }
build='' module_verifier='' bsr_verifier='' output=''
while [[ "$#" -gt 0 ]]; do
  [[ "$#" -ge 2 && -n "$2" ]] || fail 'complete explicit replay arguments required'
  case "$1" in
    --build) [[ -z "$build" ]] || fail 'duplicate build'; build="$2" ;;
    --module-source-verifier) [[ -z "$module_verifier" ]] || fail 'duplicate module verifier'; module_verifier="$2" ;;
    --bsr-source-verifier) [[ -z "$bsr_verifier" ]] || fail 'duplicate schema verifier'; bsr_verifier="$2" ;;
    --output) [[ -z "$output" ]] || fail 'duplicate output'; output="$2" ;;
    *) fail 'unsupported explicit replay argument' ;;
  esac
  shift 2
done
[[ -n "$build" && -n "$module_verifier" && -n "$bsr_verifier" && -n "$output" ]] || fail 'complete explicit replay arguments required'
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
      "$root/scripts/build-zitadel-linux.sh" --build --retained-inputs "$output/roundtrip" --retained-sha256 "$seal" \
      --module-source-verifier "$module_verifier" --bsr-source-verifier "$bsr_verifier" --output "$output/native" \
      >"$output/native-replay.log" 2>&1 <<'REPLAY_CHILD'
set -euo pipefail
[[ "$(id -u)" = "$1" && "$(id -g)" = "$2" && "$1" != 0 && "$(readlink /proc/self/ns/net)" != "$3" ]]
awk '$1=="NoNewPrivs:" {n=($2==1)} $1=="CapEff:" {e=($2=="0000000000000000")} $1=="CapBnd:" {b=($2=="0000000000000000")} END {exit !(n&&e&&b)}' /proc/self/status
if curl --fail --silent --show-error --connect-timeout 3 --max-time 5 --output /dev/null https://proxy.golang.org; then exit 125; fi
printf 'verified\n' >"$4"
shift 4
exec bash "$@"
REPLAY_CHILD
then
  tail -80 "$output/native-replay.log" >&2
  fail 'complete native replay failed'
fi
[[ "$(cat "$output/network-boundary.txt")" = verified ]] || fail 'kernel boundary was not verified'
original="$output/roundtrip/source/build-receipt.json"
native="$output/native/build-receipt.json"
jq -e --slurpfile original "$original" '
 .schemaVersion==1 and .retainedInputsRestored==true and .recipeSha256==$original[0].recipeSha256 and
 .sourceInput.manifestSha256==$original[0].sourceSnapshot.manifestSha256 and
 .preparation==$original[0].preparation and .upstreamCommit==$original[0].upstreamCommit and
 .tools==$original[0].tools and .platform=="linux/amd64" and .profile=="api-login-v1-console" and
 .consoleDownloads.manifestSha256==$original[0].consoleDownloads.manifestSha256 and
 .consoleDownloads.originalConsoleDownloadsConsumed==true and
 .imageAdmission=="unknown" and .licenseClosure=="unknown" and .reproducible==false
' "$native" >/dev/null || fail 'native replay source or recipe join differs'
for notice in console-notices-inventory.json console-notices-supplement-inventory.json login-icons-inventory.json; do
  [[ -s "$output/native/$notice" && ! -L "$output/native/$notice" ]] || fail 'complete same-build notice inventory missing'
done
go run "$root/scripts/zitadel-source-bundle.go" --verify "$output/roundtrip" "$seal" >/dev/null || fail 'input archive changed during replay'
console_notice_sha="$(shasum -a 256 "$output/native/console-notices-inventory.json" | awk '{print $1}')"
supplement_notice_sha="$(shasum -a 256 "$output/native/console-notices-supplement-inventory.json" | awk '{print $1}')"
login_notice_sha="$(shasum -a 256 "$output/native/login-icons-inventory.json" | awk '{print $1}')"
for notice_sha in "$console_notice_sha" "$supplement_notice_sha" "$login_notice_sha"; do
  [[ "$notice_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'complete notice inventory digest required'
done
# A failed read/write leaves only private, unsealed diagnosis. The uploader does
# not admit pending output; publish the receipt name only after the writer closes.
jq -n --arg input "$seal" --arg archive "$archive" --slurpfile native "$native" \
  --arg console "$console_notice_sha" --arg supplement "$supplement_notice_sha" --arg login "$login_notice_sha" '
 {schemaVersion:1,profile:"native-retained-input-replay-v1",platform:"linux/amd64",inputManifestSha256:$input,inputArchiveSha256:$archive,
 sourceManifestSha256:$native[0].sourceInput.manifestSha256,recipeSha256:$native[0].recipeSha256,binarySha256:$native[0].binarySha256,
 noticeInventories:{console:$console,supplement:$supplement,login:$login},
 networkDenied:true,nonRoot:true,noNewPrivileges:true,capabilitiesDropped:true,
 dependencyInputsCaptured:true,nativeOfflineReplayProved:true,reproducible:false,imageAdmission:"unknown",licenseClosure:"unknown"}
' >"$output/replay-receipt.pending"
mv "$output/replay-receipt.pending" "$output/replay-receipt.json"
complete=1
printf 'Complete native retained-input replay passed; publication and activation remain separate.\n'
