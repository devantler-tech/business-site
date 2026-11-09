#!/usr/bin/env bash
# Scan the frozen identity executable with the pinned govulncheck, print the
# report into the job log and fail on every reachable advisory except a named,
# time-boxed exception below. An expired exception fails like any other finding.
#
# Usage: scan-zitadel-binary.sh --scanner <govulncheck> --binary <executable>
#          --report <text report> --findings <json findings> [--today YYYY-MM-DD]
set -euo pipefail

fail() {
  printf 'scan-zitadel-binary: %s\n' "$1" >&2
  exit "${2:-1}"
}

# Each entry is "advisory ID|last day the exception applies (UTC)|reason".
# Keep entries narrow and short-lived; never add a wildcard or a module-wide entry.
exceptions=(
  'GO-2026-6909|2026-12-09|crewjam/saml v0.6.0 fixes both defects, but the Go vulnerability database still lists no fixed version (business-site#516). The SAML service-provider parsers only run once an administrator adds a SAML identity provider, and this identity build configures none.'
)

scanner='' binary='' report='' findings='' today=''
while (($#)); do
  case "$1" in
    --scanner | --binary | --report | --findings | --today)
      (($# >= 2)) || fail "$1 needs a value" 2
      case "$1" in
        --scanner) scanner="$2" ;;
        --binary) binary="$2" ;;
        --report) report="$2" ;;
        --findings) findings="$2" ;;
        --today) today="$2" ;;
      esac
      shift 2
      ;;
    *) fail "unknown argument: $1" 2 ;;
  esac
done
[[ -n "$scanner" && -n "$binary" && -n "$report" && -n "$findings" ]] || fail 'missing a required argument' 2
[[ -n "$today" ]] || today="$(date -u +%F)"
[[ "$today" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || fail "invalid date: $today" 2

for entry in "${exceptions[@]}"; do
  IFS='|' read -r id expires reason <<<"$entry"
  [[ "$id" =~ ^GO-[0-9]{4}-[0-9]{4,}$ && "$expires" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ && -n "$reason" ]] ||
    fail "malformed exception entry: $entry" 2
done

status=0
"$scanner" -mode=binary -db=https://vuln.go.dev "$binary" >"$report" 2>&1 || status=$?
echo '--- govulncheck report ---'
cat "$report"
echo '--- end of govulncheck report ---'
case "$status" in
  0) exit 0 ;;
  3) ;;
  *) fail "govulncheck failed with exit status $status" "$status" ;;
esac

# Exit status 3 means reachable vulnerable symbols. Identify them exactly.
json_status=0
"$scanner" -mode=binary -db=https://vuln.go.dev -format=json "$binary" >"$findings" || json_status=$?
((json_status == 0)) || fail "govulncheck JSON scan failed with exit status $json_status" "$json_status"
jq -e -s 'any(.[]; has("config"))' "$findings" >/dev/null || fail 'govulncheck JSON output has no config record'
called="$(jq -r -s '[.[] | select(has("finding")) | .finding | select((.trace[0].function // "") != "") | .osv] | unique | .[]' "$findings")" ||
  fail 'cannot read govulncheck JSON findings'
[[ -n "$called" ]] || fail 'govulncheck reported reachable vulnerabilities, but its JSON output names none'

unexcepted=0
while IFS= read -r osv; do
  matched=''
  for entry in "${exceptions[@]}"; do
    IFS='|' read -r id expires reason <<<"$entry"
    [[ "$id" == "$osv" ]] || continue
    matched=1
    if [[ "$today" > "$expires" ]]; then
      printf '::error::%s exception expired on %s; re-check the advisory and upgrade or renew with fresh evidence\n' "$osv" "$expires"
      unexcepted=1
    else
      printf '::warning::%s is excepted until %s: %s\n' "$osv" "$expires" "$reason"
    fi
  done
  if [[ -z "$matched" ]]; then
    printf '::error::%s is reachable in the identity executable and has no exception\n' "$osv"
    unexcepted=1
  fi
done <<<"$called"
((unexcepted == 0)) || fail 'reachable vulnerabilities without a valid exception' 3
echo 'scan-zitadel-binary: every reachable advisory is covered by an unexpired exception'
