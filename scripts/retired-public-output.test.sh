#!/usr/bin/env bash
# Exercise the real visitor checker against otherwise-valid built public pages.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
dist="${1:?Pass the built site directory}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for page in index.html da/index.html projects/index.html; do
  cp -R "$dist" "$tmp/site"
  printf '<a href="https://github.com/devantler-tech/reusable-workflows">Retired source</a>\n' >> "$tmp/site/$page"
  if out="$(node "$here/check-business-site.mjs" "$tmp/site" 2>&1)"; then
    printf 'Retired public output: FAIL — %s was accepted\n' "$page" >&2
    exit 1
  elif ! grep -Fq 'Rendered public pages must not link to retired repositories' <<<"$out"; then
    printf 'Retired public output: FAIL — %s failed for an unrelated reason\n%s\n' "$page" "$out" >&2
    exit 1
  fi
  rm -rf "$tmp/site"
done
printf 'Retired public output: PASS — EN/DA home and projects cannot bypass the built-output guard\n'
