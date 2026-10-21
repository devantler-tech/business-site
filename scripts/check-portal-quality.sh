#!/usr/bin/env bash
# Optional full-module lint: admit exact tools before accepting a clean verdict.
set -euo pipefail
[[ "$#" -eq 0 ]] || { printf '%s\n' 'The complete quality check takes no arguments.' >&2; exit 1; }
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../server"
export GOTOOLCHAIN=local
if ! go_version="$(go version 2>/dev/null)" || [[ "$go_version" != 'go version go1.27.3 '* ]]; then
  printf '%s\n' 'Use the exact Go 1.27.3 toolchain for the complete quality check.' >&2
  exit 1
fi
if ! lint_version="$(golangci-lint version 2>/dev/null)" || [[ "$lint_version" != 'golangci-lint has version 2.14.0 built with go1.27.3 from '* ]]; then
  printf '%s\n' 'Use golangci-lint 2.14.0 built with Go 1.27.3 for the complete quality check.' >&2
  exit 1
fi
golangci-lint run --no-config --max-same-issues=0 --max-issues-per-linter=0 --uniq-by-line=false ./...
