#!/usr/bin/env bash
set -euo pipefail

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$root"
output="$(mktemp "${TMPDIR:-/tmp}/go-contact.XXXXXX")"
trap 'rm -f -- "$output"' EXIT

# A successful test process with no selected tests is not integration evidence.
# pipefail retains failures even after plausible complete output.
GOTOOLCHAIN=local LC_ALL=C go -C server test -v -race -tags=contactintegration \
  ./internal/identity -run '^TestGoContact' -count=1 -timeout=90s | tee "$output"
for name in TestGoContactAuthority TestGoContactHTTP; do
  if ! grep -Eq "^--- PASS: ${name} \\([0-9.]+s\\)$" "$output"; then
    printf 'Required Go contact journey did not complete: %s\n' "$name" >&2
    exit 1
  fi
done
