#!/usr/bin/env bash
# Process controls for the actual CI caller; no network or production identity.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
cat > "$scratch/collector" <<'FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$1" >> "$FIXTURE_LOG"
op="$1"
manifest_body='retained synthetic manifest'
seal="$(printf '%s\n' "$manifest_body" | shasum -a 256 | awk '{print $1}')"
archive_sha=''
case "$op" in
  capture-base-source)
    [[ "$#" -eq 3 && "$2" == --output ]]
    mkdir -m 700 "$3"
    printf '%s\n' "$manifest_body" > "$3/base-source.json"
    chmod 444 "$3/base-source.json"
    ;;
  verify-base-source)
    [[ "$#" -eq 5 && "$2" == --input && "$4" == --sha256 && "$5" == "$seal" ]]
    ;;
  pack-base-source)
    [[ "$#" -eq 7 && "$2" == --input && "$4" == --sha256 && "$5" == "$seal" && "$6" == --output ]]
    printf '%s\n' 'synthetic archive' > "$7"
    archive_sha="$(shasum -a 256 "$7" | awk '{print $1}')"
    if [[ "$FIXTURE_MODE" == changed-archive ]]; then printf '%s\n' 'changed archive' > "$7"; fi
    if [[ "$FIXTURE_MODE" == changed-manifest ]]; then chmod 600 "$3/base-source.json"; printf '%s\n' 'changed manifest' > "$3/base-source.json"; fi
    ;;
  *) exit 2 ;;
esac
jq -n --arg op "$op" --arg seal "$seal" --arg archive "$archive_sha" --arg mode "$FIXTURE_MODE" '
  {schemaVersion:1,operation:$op,baseImage:"gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3",manifestSha256:$seal,payloadFiles:41,baseImageSignatureVerified:false,openpgpVerified:false,sourceReleaseVerified:($mode=="false-admission"),imageAdmission:false} +
  (if $op=="pack-base-source" then {archiveSha256:$archive} else {} end)'
[[ "$FIXTURE_MODE" != "$op-failure" ]] || exit 42
FIXTURE
chmod 700 "$scratch/collector"
export FIXTURE_LOG="$scratch/inert.log" FIXTURE_MODE=original
bash "$root/scripts/retain-zitadel-base-source.sh" > "$scratch/inert.txt"
test ! -e "$FIXTURE_LOG"
grep -q -- '--capture --command' "$scratch/inert.txt"
for mode in original capture-base-source-failure verify-base-source-failure pack-base-source-failure changed-archive changed-manifest false-admission; do
  export FIXTURE_LOG="$scratch/$mode.log" FIXTURE_MODE="$mode"
  status=0
  bash "$root/scripts/retain-zitadel-base-source.sh" --capture --command "$scratch/collector" --output "$scratch/$mode" > "$scratch/$mode.stdout" 2> "$scratch/$mode.stderr" || status=$?
  if [[ "$mode" == original ]]; then
    test "$status" -eq 0
    test -f "$scratch/$mode/evidence/base-source.json"
    cmp "$scratch/$mode/capsule/base-source.json" "$scratch/$mode/evidence/base-source.json"
    if bash "$root/scripts/retain-zitadel-base-source.sh" --capture --command "$scratch/collector" --output "$scratch/$mode" >/dev/null 2>&1; then echo 'existing output overwritten' >&2; exit 1; fi
  else
    test "$status" -ne 0
    test ! -e "$scratch/$mode/evidence/base-source.json"
    case "$mode" in
      capture-base-source-failure) test "$(wc -l < "$FIXTURE_LOG" | tr -d ' ')" -eq 1 ;;
      verify-base-source-failure) test "$(wc -l < "$FIXTURE_LOG" | tr -d ' ')" -eq 2 ;;
      pack-base-source-failure) test "$(wc -l < "$FIXTURE_LOG" | tr -d ' ')" -eq 3 ;;
    esac
  fi
done
printf '%s\n' 'Identity base-source caller passed: original, inert call, existing-output refusal and six failed/changed/admission controls.'
