#!/usr/bin/env bash
# Read only authenticated Go source inputs with the frozen native Go client.
# This is a cache-reader check, not full generation/compilation or image admission.
set -euo pipefail
[ "$#" -ne 0 ] || exit 0
fail() { printf 'Go input readback: %s\n' "$1" >&2; exit 1; }
build='' verifier=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --build) [ "$#" -ge 2 ] || fail 'build argument missing'; build="$2"; shift 2 ;;
    --module-source-verifier) [ "$#" -ge 2 ] || fail 'verifier argument missing'; verifier="$2"; shift 2 ;;
    *) fail 'unsupported argument' ;;
  esac
done
[[ "$build" = /* && "$verifier" = /* ]] || fail 'complete absolute arguments required'
[ "$(uname -s)/$(uname -m)" = Linux/x86_64 ] || fail 'native Linux amd64 required'
for tool in go jq shasum sudo unshare setpriv timeout; do command -v "$tool" >/dev/null || fail 'native reader tool unavailable'; done
[ "$(go version)" = 'go version go1.26.10 linux/amd64' ] || fail 'frozen native Go compiler required'
[ -x "$verifier" ] && [ -f "$build/build-receipt.json" ] || fail 'owned build or verifier missing'
sha() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
jq -es '
 length==1 and .[0].upstreamCommit=="8afa5dd37a8e723efb9eace8d09fd5c1cccef694" and
 .[0].profile=="api-login-v1-console" and .[0].tools.go=="1.26.10" and
 .[0].sourceSnapshot.dependencyInputsCaptured==false and .[0].sourceSnapshot.nativeOfflineReplayProved==false and
 .[0].sourceSnapshot.goModuleVerifier.go=="1.27.3" and
 all(.[0].sourceSnapshot|[.manifestSha256,.goModules.manifestSha256,.generatorGoSources.manifestSha256,
  .goGraphMetadata.manifestSha256,.goModuleVerifier.sha256][];type=="string" and test("^[0-9a-f]{64}$"))
' "$build/build-receipt.json" >/dev/null || fail 'native source receipt differs'
source_seal="$(jq -er .sourceSnapshot.manifestSha256 "$build/build-receipt.json")"
service_seal="$(jq -er .sourceSnapshot.goModules.manifestSha256 "$build/build-receipt.json")"
generator_seal="$(jq -er .sourceSnapshot.generatorGoSources.manifestSha256 "$build/build-receipt.json")"
metadata_seal="$(jq -er .sourceSnapshot.goGraphMetadata.manifestSha256 "$build/build-receipt.json")"
verifier_seal="$(jq -er .sourceSnapshot.goModuleVerifier.sha256 "$build/build-receipt.json")"
[ "$(sha "$verifier")" = "$verifier_seal" ] || fail 'verifier changed'
work="$build/go-input-readback-work"
mkdir -m 0700 "$work"
mkdir -m 0700 "$work/outside-module" "$work/compiler-cache"
native="$build/go-input-native"
restore() {
 "$verifier" restore-go-inputs --service "$build/selected-go-sources" --service-sha256 "$service_seal" \
  --generators "$build/selected-generator-go-sources" --generators-sha256 "$generator_seal" \
  --metadata "$build/go-graph-metadata" --metadata-sha256 "$metadata_seal" \
  --source-sha256 "$source_seal" --output "$1"
}
restore "$native" >"$build/go-input-restoration.json"
manifest_seal="$(jq -er .manifestSha256 "$build/go-input-restoration.json")"
[ "$(sha "$native/manifest.json")" = "$manifest_seal" ] || fail 'restoration manifest differs'
jq -e --arg source "$source_seal" '
 .schemaVersion==1 and .sourceManifestSha256==$source and .modules>0 and .files>0 and
 .selectedGoInputsRestored==true and .dependencyInputsCaptured==false and .nativeOfflineReplayProved==false
' "$build/go-input-restoration.json" >/dev/null || fail 'incomplete restoration'
jq '[.modules[].effective|{Path,Version,Sum,GoModSum}]|unique_by([.Path,.Version])|sort_by(.Path,.Version)' \
 "$build/selected-go-sources/manifest.json" >"$work/service-expected.json"
jq '[.modules[]|{Path,Version,Sum,GoModSum}]|unique_by([.Path,.Version])|sort_by(.Path,.Version)' \
 "$build/selected-generator-go-sources/manifest.json" >"$work/generator-expected.json"
cmp "$build/selected-go-sources/records/go.mod" "$build/prepared/source/go.mod" || fail 'source module definition changed'
cmp "$build/selected-go-sources/records/go.sum" "$build/prepared/source/go.sum" || fail 'source checksums changed'
reader_uid="$(id -u)" reader_gid="$(id -g)"
[ "$reader_uid" -ne 0 ] || fail 'nonroot native reader required'
go_binary="$(command -v go)"
unshare_binary="$(command -v unshare)" setpriv_binary="$(command -v setpriv)"
# A new network namespace denies every remote read. Drop privileges and capabilities
# before starting the known compiler. This is not a filesystem sandbox.
native_download() (
 cache="$1" directory="$2" expected="$3"
 modules=()
 while IFS= read -r id; do modules+=("$id"); done < <(jq -er '.[]|.Path+"@"+.Version' "$expected")
 [ "${#modules[@]}" -gt 0 ] || fail 'empty native module set'
 cd "$directory"
 timeout 180 sudo -n "$unshare_binary" --net -- "$setpriv_binary" \
  --reuid "$reader_uid" --regid "$reader_gid" --clear-groups --no-new-privs --bounding-set=-all -- \
  env -i PATH="$PATH" LC_ALL=C GOENV=off GOWORK=off GOTOOLCHAIN=local GOFLAGS=-modcacherw \
   GOPROXY=off GOVCS='*:off' GONOSUMDB= GONOPROXY= GOPRIVATE= GOSUMDB=sum.golang.org \
   GOCACHE="$work/compiler-cache" GOPATH="$cache/go-path" GOMODCACHE="$cache/go-modules" \
   "$go_binary" mod download -json "${modules[@]}"
)
check_result() {
 jq -se --slurpfile expected "$2" '
  all(.[];has("Error")|not) and
  (map({Path,Version,Sum,GoModSum})|sort_by(.Path,.Version))==$expected[0]
 ' "$1" >/dev/null || fail 'native module identity or checksum readback differs'
}
# Log only compact public module diagnostics if a complete native read fails.
verified_download() {
 local result="$4"
 if ! native_download "$1" "$2" "$3" >"$result"; then
  jq -sc 'map({Path,Version,Error:((.Error // "")|.[0:512])})' "$result" >&2 || true
  fail 'complete authenticated inputs were refused by the native reader'
 fi
 check_result "$result" "$3"
}
verified_download "$native" "$build/prepared/source" "$work/service-expected.json" "$work/service-result.json"
# The generator check has no project go.sum: it must use the retained signed log.
[ "$(cd "$work/outside-module" && GOENV=off GOWORK=off GOTOOLCHAIN=local "$go_binary" env GOMOD)" = /dev/null ] || fail 'generator reader inherited a module'
verified_download "$native" "$work/outside-module" "$work/generator-expected.json" "$work/generator-result.json"
cmp "$build/selected-go-sources/records/go.mod" "$build/prepared/source/go.mod" || fail 'reader changed source module definition'
cmp "$build/selected-go-sources/records/go.sum" "$build/prepared/source/go.sum" || fail 'reader changed source checksums'
# Independent complete/missing-proof reconstructions avoid derived ZIP markers.
jq '.[0:1]' "$work/generator-expected.json" >"$work/negative-expected.json"
control="$work/proof-control"
restore "$control" >"$work/control-restoration.json"
verified_download "$control" "$work/outside-module" "$work/negative-expected.json" "$work/control-result.json"
negative="$work/missing-proof"
restore "$negative" >"$work/negative-restoration.json"
cmp "$work/control-restoration.json" "$work/negative-restoration.json" || fail 'proof control and negative inputs differ'
selected_path="$(jq -er '.[0].Path' "$work/negative-expected.json")"
selected_version="$(jq -er '.[0].Version' "$work/negative-expected.json")"
lookup="$(jq -er --arg path "$selected_path" --arg version "$selected_version" '
 .modules[]|select(.Path==$path and .Version==$version)|.Prefix|
 "go-modules/cache/download/sumdb/sum.golang.org/lookup/"+sub("/@v/";"@")
' "$native/manifest.json")"
[ -f "$negative/$lookup" ] && [ ! -L "$negative/$lookup" ] || fail 'negative checksum input missing'
rm -- "$negative/$lookup"
if native_download "$negative" "$work/outside-module" "$work/negative-expected.json" >"$work/negative-result.json" 2>"$work/negative-error.txt"; then
 fail 'missing checksum proof was accepted'
else
 negative_rc=$?
 [ "$negative_rc" -eq 1 ] || fail 'negative probe did not complete natively'
fi
if ! jq -se --arg path "$selected_path" --arg version "$selected_version" '
 length==1 and .[0].Path==$path and .[0].Version==$version and
 (.[0].Error|type=="string" and length>0)
' "$work/negative-result.json" >/dev/null; then
 jq -sc 'map({Path,Version,Error:((.Error // "")|.[0:512])})' "$work/negative-result.json" >&2 || true
 fail 'negative probe did not refuse checksum verification'
fi
[ "$(sha "$verifier")" = "$verifier_seal" ] || fail 'verifier changed during readback'
jq -n --arg source "$source_seal" --arg seal "$manifest_seal" \
 --slurpfile service "$work/service-expected.json" --slurpfile generators "$work/generator-expected.json" '
 {schemaVersion:1,sourceManifestSha256:$source,restorationManifestSha256:$seal,go:"1.26.10",
  serviceModules:($service[0]|length),generatorModules:($generators[0]|length),
  selectedIdentities:([$service[0][], $generators[0][]]|unique_by([.Path,.Version])|length),
  nativeCacheReadbackPassed:true,networkDenied:true,independentProofControlPassed:true,missingProofRefused:true,sourceChecksumsUnchanged:true,
  dependencyInputsCaptured:false,nativeOfflineReplayProved:false}
' >"$build/go-input-readback.json"
printf 'Native Go input reader: authenticated cache readback and missing-proof refusal passed.\n'
