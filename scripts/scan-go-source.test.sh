#!/usr/bin/env bash
# Preserve the real source scanner, target arguments and its failure status.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
yq -o=json '.' "$root/.mega-linter.yml" > "$tmp/config.json"
jq -e '.REPOSITORY_OSV_SCANNER_CLI_EXECUTABLE == ["bash", "/github/workspace/scripts/scan-go-source.sh"] and
  .REPOSITORY_OSV_SCANNER_DISABLE_ERRORS != true and
  ((.DISABLE_LINTERS // []) | index("REPOSITORY_OSV_SCANNER") | not)' "$tmp/config.json" >/dev/null || {
  echo 'FAIL: OSV must retain complete analysis through the module-selected compiler' >&2
  exit 1
}
mkdir "$tmp/bin" "$tmp/module"
cat > "$tmp/bin/go" <<'GO'
#!/usr/bin/env bash
set -eu
if [ "$#" -eq 2 ] && [ "$1" = env ] && [ "$2" = GOVERSION ]; then
  if [ "$SCAN_CASE" = compiler-failure ]; then exit 19; fi
  case "$SCAN_CASE" in
    compiler-empty) printf '\n' ;;
    compiler-invalid) printf 'go1.27.3 invalid\n' ;;
    *) printf 'go1.27.3\n' ;;
  esac
  exit 0
fi
[ "$PWD" = "$SCAN_MODULE" ] || exit 31
[ "${GOTOOLCHAIN:-}" = go1.27.3 ] || exit 32
[ "$#" -eq 2 ] && [ "$1" = install ] && [ "$2" = github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.3.8 ] || exit 33
if [ "$SCAN_CASE" = compiler-build-failure ]; then exit 17; fi
cat > "$GOBIN/osv-scanner" <<'SCANNER'
#!/usr/bin/env bash
set -eu
[ "$PWD" = "$SCAN_MODULE" ] || exit 34
printf '%s\n' "$@" > "$SCAN_ARGS"
if [ "$SCAN_CASE" = scan-failure ]; then exit 23; fi
SCANNER
chmod +x "$GOBIN/osv-scanner"
GO
chmod +x "$tmp/bin/go"
printf '%s\n' scan source --recursive . --experimental-exclude 'space directory' > "$tmp/expected"
for scenario in valid compiler-failure compiler-empty compiler-invalid compiler-build-failure scan-failure; do
  rm -f "$tmp/args"
  set +e
  (cd "$tmp/module" && SCAN_CASE="$scenario" SCAN_MODULE="$tmp/module" SCAN_ARGS="$tmp/args" PATH="$tmp/bin:$PATH" \
    bash "$root/scripts/scan-go-source.sh" scan source --recursive . --experimental-exclude 'space directory') > "$tmp/out" 2> "$tmp/err"
  status=$?
  set -e
  case "$scenario" in
    valid) [ "$status" -eq 0 ]; cmp "$tmp/expected" "$tmp/args" ;;
    scan-failure) [ "$status" -eq 23 ]; cmp "$tmp/expected" "$tmp/args" ;;
    compiler-build-failure) [ "$status" -eq 17 ]; [ ! -e "$tmp/args" ] ;;
    *) [ "$status" -ne 0 ]; [ ! -e "$tmp/args" ] ;;
  esac
done
printf '%s\n' 'PASS: real configured entrypoint, exact target arguments and six compiler/scanner controls'
