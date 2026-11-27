#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

# Both checkers share one allowlist. MegaLinter handles all other links, and
# the required portal quality job checks these records with a read-only token.
readonly github_record='^https://api\.github\.com/repos/(devantler-tech/(business-site|monorepo)/(issues|pulls)/[1-9][0-9]*|justinas/nosurf/security-advisories/GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$'
case "${1:-}" in
  --github-records) shift ;;
  -h|--help|-V|--version) exec lychee "$@" ;;
  *) exec lychee "$@" --exclude "$github_record" ;;
esac
if [[ ! "${GITHUB_TOKEN:-}" =~ ^[a-zA-Z0-9_.-]+$ || "$GITHUB_TOKEN" == HIDDEN_BY_MEGALINTER ]]; then
  echo 'GitHub reference checker requires a read-only credential' >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
if [[ $# == 0 ]]; then
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
  cd "$root"
  env -u GITHUB_TOKEN node --input-type=module - "$root" >"$work/arguments" <<'NODE'
import { readFileSync } from 'node:fs';
import { load } from 'js-yaml';

const options = load(readFileSync('.mega-linter.yml', 'utf8')).SPELL_LYCHEE_ARGUMENTS;
for (const option of options) {
  process.stdout.write(option.replace('/github/workspace', process.argv[2]) + '\0');
}
NODE
  cd "$root/server"
  git ls-files -z -- '*.md' '*.mdx' '*.markdown' '*.html' '*.htm' '*.rst' '*.txt' \
    '*.json' '*.jsonc' '*.json5' '*.yaml' '*.yml' >"$work/sources"
  [[ -s "$work/sources" ]] || { echo 'GitHub reference checker found no source inputs' >&2; exit 1; }
  while IFS= read -r -d '' input; do set -- "$@" "$input"; done <"$work/arguments"
  while IFS= read -r -d '' input; do set -- "$@" "$input"; done <"$work/sources"
fi
# Lychee can warn and skip a failed input while the dump still exits successfully.
if ! env -u GITHUB_TOKEN lychee "$@" --no-progress --dump >"$work/discovered" 2>"$work/discovery-errors" ||
  [[ -s "$work/discovery-errors" ]]; then
  echo 'GitHub reference discovery did not complete cleanly' >&2
  exit 1
fi
sort -u "$work/discovered" >"$work/records"

checked=0
while IFS= read -r record || [[ -n "$record" ]]; do
  # A dump includes ordinary links even when an include filter is supplied.
  [[ "$record" =~ $github_record ]] || continue
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
