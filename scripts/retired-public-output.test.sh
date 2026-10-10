#!/usr/bin/env bash
# Exercise the real visitor checker against otherwise-valid built public pages.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
dist="${1:?Pass the built site directory}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for page in index.html da/index.html projects/index.html; do
  for href in \
    'https://github.com/devantler-tech/reusable-workflows' \
    '//github.com/devantler-tech/reusable-workflows' \
    'http://github.com/devantler-tech/reusable-workflows/tree/main' \
    'https://github.com/DEVANTLER-TECH/REUSABLE-WORKFLOWS' \
    'https://github.com/devantler-tech/reusable&#x2D;workflows' \
    'https://github.com/devantler-tech/%72eusable-workflows'; do
  cp -R "$dist" "$tmp/site"
  printf '<a href="%s">Retired source</a>\n' "$href" >> "$tmp/site/$page"
  if out="$(node "$here/check-business-site.mjs" "$tmp/site" 2>&1)"; then
    printf 'Retired public output: FAIL — %s at %s was accepted\n' "$href" "$page" >&2
    exit 1
  elif ! grep -Fq 'Rendered public pages must not link to retired repositories' <<<"$out"; then
    printf 'Retired public output: FAIL — %s failed for an unrelated reason\n%s\n' "$page" "$out" >&2
    exit 1
  fi
  rm -rf "$tmp/site"
  done
done
# A retired repository string inside another URL is not that repository's link.
for href in \
  'https://example.test/https://github.com/devantler-tech/reusable-workflows' \
  'https://github.com/devantler-tech/reusable-workflows-example' \
  'https://github.com/devantler-tech/reusable-workflows.example.test'; do
  cp -R "$dist" "$tmp/site"
  printf '<a href="%s">Unrelated destination</a>\n' "$href" >> "$tmp/site/index.html"
  node "$here/check-business-site.mjs" "$tmp/site" >/dev/null
  rm -rf "$tmp/site"
done
printf 'Retired public output: PASS — 18 actual EN/DA/Projects rejection controls and 3 unrelated destinations\n'
