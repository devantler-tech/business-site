#!/usr/bin/env bash
# Test double for the one read-only GitHub request used by the star refresher.
set -euo pipefail
[ "$*" = 'api --paginate --slurp orgs/devantler-tech/repos?type=public&per_page=100' ] || exit 9
[ "${STAR_READ_FAIL:-0}" = 0 ] || exit 2
cat "$STAR_FIXTURE"
