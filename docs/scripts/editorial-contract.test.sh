#!/usr/bin/env bash
# Keep the former monorepo editorial tripwires with their source owner.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
readme="$root/docs/README.md"
fail() { printf 'editorial contract: FAIL — %s\n' "$*" >&2; exit 1; }
for standard in '## Blog editorial standard' \
  'Problem → Why it matters → What Devantler Tech built' \
  'Problem → Why now → Current status' \
  'RSS inclusion, social/OG presentation'; do
  grep -Fq "$standard" "$readme" || fail "missing editorial standard: $standard"
done
[[ "$(cat "$root/.github/CODEOWNERS")" == '* @devantler-tech/maintainers' ]] || fail 'team ownership is missing'
[[ "$(cat "$root/CLAUDE.md")" == '@AGENTS.md' ]] || fail 'Claude shim is not canonical'
[[ "$(cat "$root/docs/CLAUDE.md")" == '@AGENTS.md' ]] || fail 'docs shim is not canonical'
printf 'editorial contract: PASS\n'
