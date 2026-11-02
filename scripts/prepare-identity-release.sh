#!/usr/bin/env bash
# The trusted manual caller verifies retained data; never execute restored source.
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
[[ "${IDENTITY_RELEASE_MODE:-}" == dry-run || "${IDENTITY_RELEASE_MODE:-}" == publish ]] || exit 1
bash scripts/admit-identity-release.sh
# The admission helper writes only a path it allocated itself. Read the exact
# fixed output line rather than sourcing an artifact-selected environment file.
work="$(sed -n 's/^IDENTITY_RELEASE_WORK=//p' "$GITHUB_ENV")"
[[ "$work" == "$RUNNER_TEMP"/identity-forge.* && "$work" != *$'\n'* && -d "$work/restored/native" ]] || exit 1
IDENTITY_RELEASE_WORK="$work" bash scripts/verify-identity-original.sh
