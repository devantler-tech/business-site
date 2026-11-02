#!/usr/bin/env bash
# Explicit source-data retention only; never publishes or executes retained data.
set -euo pipefail
if [[ "$#" -eq 0 ]]; then
  printf '%s\n' 'Explicit operation: --capture --command /absolute/identity-release --output /absolute/new-root'
  exit 0
fi
[[ "$#" -eq 5 && "$1" == --capture && "$2" == --command && "$4" == --output ]] || exit 2
command_path="$3"
output="$5"
[[ "$command_path" == /* && -f "$command_path" && -x "$command_path" && ! -L "$command_path" && "$output" == /* ]] || exit 2
umask 077
mkdir -m 700 -- "$output"
mkdir -m 700 -- "$output/evidence"
"$command_path" capture-base-source --output "$output/capsule" > "$output/evidence/capture.json"
seal="$(jq -er '.manifestSha256 | select(test("^[0-9a-f]{64}$"))' "$output/evidence/capture.json")"
"$command_path" verify-base-source --input "$output/capsule" --sha256 "$seal" > "$output/evidence/verify.json"
"$command_path" pack-base-source --input "$output/capsule" --sha256 "$seal" --output "$output/base-source.tar" > "$output/evidence/pack.json"
archive_sha="$(shasum -a 256 "$output/base-source.tar" | awk '{print $1}')"
jq -e --arg seal "$seal" --arg archive "$archive_sha" --slurpfile capture "$output/evidence/capture.json" --slurpfile verify "$output/evidence/verify.json" '
  .schemaVersion==1 and .operation=="pack-base-source" and .manifestSha256==$seal and .archiveSha256==$archive and
  .payloadFiles==41 and .baseImage=="gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3" and
  .baseImageSignatureVerified==false and .openpgpVerified==false and .sourceReleaseVerified==false and .imageAdmission==false and
  ($capture|length)==1 and ($verify|length)==1 and
  $capture[0].operation=="capture-base-source" and $verify[0].operation=="verify-base-source" and
  (del(.operation,.archiveSha256)==($capture[0]|del(.operation))) and
  (del(.operation,.archiveSha256)==($verify[0]|del(.operation)))
' "$output/evidence/pack.json" >/dev/null
cp -p "$output/capsule/base-source.json" "$output/evidence/base-source.pending"
test "$(shasum -a 256 "$output/evidence/base-source.pending" | awk '{print $1}')" = "$seal"
cmp "$output/capsule/base-source.json" "$output/evidence/base-source.pending"
mv "$output/evidence/base-source.pending" "$output/evidence/base-source.json"
