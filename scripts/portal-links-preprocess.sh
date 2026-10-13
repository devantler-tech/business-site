#!/usr/bin/env bash
# Method-aware real-template validation; ordinary documents remain Lychee input.
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
[[ $# == 1 ]] || { echo 'portal-links: exactly one input is required' >&2; exit 2; }
source_path="$1"
[[ "$source_path" == /* ]] || source_path="$PWD/$source_path"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
cd "$root/server"
go test -c -o "$scratch/check" ./internal/identity
if ! "$scratch/check" -test.run '^TestPortalLinkPreprocessor$' -test.timeout 30s \
  -portal-link-source "$source_path" -portal-link-output "$scratch/rendered" >"$scratch/receipt"; then
  cat "$scratch/receipt" >&2
  exit 1
fi
[[ -f "$scratch/rendered" && -s "$scratch/rendered" ]] || { echo 'portal-links: no complete output' >&2; exit 2; }
cat "$scratch/rendered"
