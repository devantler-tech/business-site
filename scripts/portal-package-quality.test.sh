#!/usr/bin/env bash
# Exercise package context PLUS the same selected files passed by MegaLinter.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
revive="${PORTAL_REVIVE:-revive}"
[[ "$("$revive" -version)" == 'version 1.16.0' ]]
[[ "$(go version)" == 'go version go1.27.3 '* ]]
[[ "${GOTOOLCHAIN:-}" == local ]]
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
git -C "$root" ls-files -z -- 'server/*.go' >"$scratch/files"
files=()
while IFS= read -r -d '' file; do files+=("${file#server/}"); done <"$scratch/files"
[[ "${#files[@]}" -gt 0 ]]
cd "$root/server"
"$revive" -set_exit_status ./... "${files[@]}"
printf 'PASS: actual package context plus all %s selected Go files\n' "${#files[@]}"
# A genuine missing package comment must remain a failure; grouping is not a
# disabled rule or an exclusion of selected files.
mkdir "$scratch/missing"
printf 'module fixture.invalid/undocumented\n\ngo 1.27.3\n' >"$scratch/missing/go.mod"
printf 'package undocumented\n\nfunc Value() int { return 1 }\n' >"$scratch/missing/missing.go"
cd "$scratch/missing"
status=0
"$revive" -set_exit_status ./... missing.go >"$scratch/result" 2>&1 || status=$?
[[ "$status" == 1 ]]
grep -F 'should have a package comment' "$scratch/result" >/dev/null
echo 'PASS: genuine missing package documentation still fails the actual rule'
