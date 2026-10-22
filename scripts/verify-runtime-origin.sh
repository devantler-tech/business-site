#!/usr/bin/env bash
# Reuse the accepted cryptographic verifier, changing only an isolated deployment copy.
set -euo pipefail
root=$PWD
helper="$root/.runtime-publisher/.github/scripts/prepare-prebuilt-webapp.sh"
[[ -f "$helper" && ! -L "$helper" && -d "${RUNNER_TEMP:-}" ]] || exit 1
work="$(mktemp -d "$RUNNER_TEMP/runtime-origin.XXXXXX")"
trap 'rm -rf "$work"' EXIT
cp -R deploy "$work/deploy"
(
  cd "$work"
  REPOSITORY=devantler-tech/business-site APP_NAME=business-site DEPLOY_PATH=deploy \
    PREBUILT_IMAGE_DIGEST="${IMAGE_DIGEST:?}" PREBUILT_PUBLIC_DIGEST="${PUBLIC_DIGEST:?}" \
    PREBUILT_CI_RUN_ID="${ORIGINAL_CI_RUN_ID:?}" PREBUILT_CI_RUN_ATTEMPT="${ORIGINAL_CI_RUN_ATTEMPT:?}" \
    GITHUB_OUTPUT="$work/verified" bash "$helper"
)
printf 'Original source-CI image signature and complete provenance verified; source files unchanged.\n'
