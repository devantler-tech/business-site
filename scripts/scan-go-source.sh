#!/usr/bin/env bash
# Build the unchanged scanner with the current module's selected Go compiler.
set -euo pipefail
compiler="$(go env GOVERSION)"
if [[ ! "$compiler" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf '%s\n' 'source scanner: no stable module-selected Go compiler' >&2
  exit 1
fi
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# Versioned installation uses no main module. Carry the selected compiler
# explicitly, then run the real binary so go run cannot collapse its exit code.
GOBIN="$work" GOTOOLCHAIN="$compiler" go install github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.3.8
status=0
"$work/osv-scanner" "$@" || status=$?
exit "$status"
