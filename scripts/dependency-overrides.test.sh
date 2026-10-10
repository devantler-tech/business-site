#!/usr/bin/env bash
#
# dependency-overrides.test.sh — an override for a package the site also depends on directly
# follows that dependency instead of repeating its version (business-site#10).
#
# WHY: npm refuses to resolve a tree in which a direct dependency and the override for the
# same package name disagree (EOVERRIDE). An update tool changes the direct dependency only,
# so a repeated version makes every proposed update of that package unresolvable, and the
# update is never offered. The reference form "$<name>" always equals the direct dependency,
# so the override keeps forcing every transitive copy onto the version the site itself uses.
#
# CHECKS
#   1. package.json is readable and holds an overrides object. An unreadable or
#      override-less file fails, so an empty discovery cannot pass.
#   2. Every top-level override whose name is also in dependencies, devDependencies or
#      optionalDependencies is exactly "$<name>".
#
# Fixture cases first prove the check rejects the drift it exists for.
set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(dirname -- "${script_dir}")"

command -v jq >/dev/null || { printf 'dependency-overrides: jq is required\n' >&2; exit 2; }

# Prints one line per violating override name; exits 2 when the file cannot be judged.
violations() {
  local manifest="$1"
  jq -e '.overrides | type == "object"' "${manifest}" >/dev/null 2>&1 || return 2
  jq -r '
    ((.dependencies // {}) + (.devDependencies // {}) + (.optionalDependencies // {})) as $direct
    | .overrides
    | to_entries[]
    | select($direct[.key] != null)
    | select(.value != ("$" + .key))
    | .key
  ' "${manifest}"
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

failures=0
expect() {
  local name="$1" want_status="$2" want_output="$3" manifest="$4" got_status=0 got_output
  got_output="$(violations "${manifest}")" || got_status=$?
  if [ "${got_status}" != "${want_status}" ] || [ "${got_output}" != "${want_output}" ]; then
    printf 'FAIL fixture %s: status %s output [%s], wanted status %s output [%s]\n' \
      "${name}" "${got_status}" "${got_output}" "${want_status}" "${want_output}" >&2
    failures=$((failures + 1))
  fi
}

printf '%s' '{"devDependencies":{"a":"^1.0.0"},"overrides":{"a":"^1.0.0","b":"^2.0.0"}}' >"${tmp}/repeated.json"
expect 'repeated version is rejected' 0 'a' "${tmp}/repeated.json"

printf '%s' '{"dependencies":{"a":"^1.0.0"},"overrides":{"a":"$b"}}' >"${tmp}/other-reference.json"
expect 'reference to another package is rejected' 0 'a' "${tmp}/other-reference.json"

printf '%s' '{"optionalDependencies":{"a":"^1.0.0"},"overrides":{"a":{".":"^1.0.0"}}}' >"${tmp}/nested.json"
expect 'nested override of a direct dependency is rejected' 0 'a' "${tmp}/nested.json"

printf '%s' '{"devDependencies":{"a":"^1.0.0"},"overrides":{"a":"$a","b":"^2.0.0"}}' >"${tmp}/reference.json"
expect 'reference form is accepted' 0 '' "${tmp}/reference.json"

printf '%s' '{"devDependencies":{"a":"^1.0.0"}}' >"${tmp}/no-overrides.json"
expect 'missing overrides cannot pass' 2 '' "${tmp}/no-overrides.json"

printf '%s' '{not json' >"${tmp}/unreadable.json"
expect 'unreadable manifest cannot pass' 2 '' "${tmp}/unreadable.json"

if [ "${failures}" -ne 0 ]; then
  exit 1
fi

status=0
found="$(violations "${repo_root}/package.json")" || status=$?
if [ "${status}" -ne 0 ]; then
  printf 'dependency-overrides: package.json could not be read or has no overrides object\n' >&2
  exit 1
fi
if [ -n "${found}" ]; then
  while IFS= read -r name; do
    printf 'dependency-overrides: override "%s" repeats a version; write "$%s" so it follows the direct dependency\n' \
      "${name}" "${name}" >&2
  done <<<"${found}"
  exit 1
fi

printf 'dependency-overrides: 6 fixture cases and package.json pass\n'
