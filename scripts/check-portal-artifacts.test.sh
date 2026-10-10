#!/usr/bin/env bash
set -euo pipefail
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/da"
printf '<h1>Public EN</h1>\n' > "$fixture/index.html"
printf '<h1>Public DA</h1>\n' > "$fixture/da/index.html"
bash "$script_dir/check-portal-artifacts.sh" "$fixture" >/dev/null
failed=0
for name in server.go schema.sql go.mod go.sum .env; do
  printf 'private\n' > "$fixture/$name"
  if bash "$script_dir/check-portal-artifacts.sh" "$fixture" >/dev/null 2>&1; then
    printf 'FAIL: accepted private file %s\n' "$name" >&2
    failed=1
  fi
  rm "$fixture/$name"
done
printf 'portal_login_attempts\n' > "$fixture/identity.txt"
if bash "$script_dir/check-portal-artifacts.sh" "$fixture" >/dev/null 2>&1; then
  printf '%s\n' 'FAIL: accepted identity storage content' >&2
  failed=1
fi
rm "$fixture/identity.txt"
mkdir "$fixture/portal"
printf '<h1>Sign in</h1>\n' > "$fixture/portal/index.html"
if bash "$script_dir/check-portal-artifacts.sh" "$fixture" >/dev/null 2>&1; then
  printf '%s\n' 'FAIL: accepted premature portal route' >&2
  failed=1
fi
rm "$fixture/portal/index.html"
rmdir "$fixture/portal"
rm "$fixture/da/index.html"
if bash "$script_dir/check-portal-artifacts.sh" "$fixture" >/dev/null 2>&1; then
  printf '%s\n' 'FAIL: accepted incomplete public build' >&2
  failed=1
fi
[[ "$failed" == 0 ]] || exit 1
printf '%s\n' 'Portal artifact boundary: 8 negative controls and public output PASS'
