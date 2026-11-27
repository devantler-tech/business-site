#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

case "${1:-}" in
  -h|--help|-V|--version) exec lychee "$@" ;;
esac
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  exec lychee "$@"
fi
if [[ ! "$GITHUB_TOKEN" =~ ^[a-zA-Z0-9_.-]+$ ]]; then
  echo 'GitHub reference checker received an invalid credential' >&2
  exit 1
fi

# Keep credentials on the exact public records. The ordinary Lychee pass still
# checks every other link, including GitHub URLs with fragments or query strings.
readonly github_record='^https://api\.github\.com/repos/(devantler-tech/(business-site|monorepo)/(issues|pulls)/[1-9][0-9]*|justinas/nosurf/security-advisories/GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$'
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
lychee "$@" --dump --include "$github_record" >"$work/discovered"
sort -u "$work/discovered" >"$work/records"

checked=0
while IFS= read -r record || [[ -n "$record" ]]; do
  if [[ ! "$record" =~ $github_record ]]; then
    echo 'GitHub reference checker refused an unexpected URL' >&2
    exit 1
  fi
  status="$(printf 'header = "Authorization: Bearer %s"\n' "$GITHUB_TOKEN" |
    env -u GITHUB_TOKEN curl --disable --config - --proto '=https' --max-redirs 0 \
      --silent --show-error --connect-timeout 5 --max-time 15 \
      --output /dev/null --write-out '%{http_code}' -- "$record")"
  if [[ "$status" != 200 ]]; then
    printf 'GitHub reference check failed (%s): %s\n' "$status" "$record" >&2
    exit 1
  fi
  ((checked += 1))
done <"$work/records"
printf 'Verified %d GitHub API references.\n' "$checked"

lychee "$@" --exclude "$github_record"
