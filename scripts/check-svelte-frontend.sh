#!/usr/bin/env bash
set -euo pipefail
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo"
npm --prefix web audit
npm --prefix web test
npm --prefix web run check
FEATURE_CUSTOMER_SCOPE=true bash scripts/build-journal-reference.sh
node web/tests/check-reading-reference.mjs
FEATURE_SVELTE_FRONTEND=true npm --prefix web run build
node scripts/check-customer-scope.mjs web/build
node scripts/customer-scope.test.mjs web/build
node web/tests/check-output.mjs enabled
node web/tests/check-projects-output.mjs enabled
node web/tests/check-journal-output.mjs enabled
node web/tests/check-reading-output.mjs enabled
node web/tests/check-snapshot-builds.mjs
FEATURE_SVELTE_FRONTEND=false npm --prefix web run build
node web/tests/check-output.mjs disabled
node web/tests/check-projects-output.mjs disabled
node web/tests/check-journal-output.mjs disabled
node web/tests/check-reading-output.mjs disabled
env -u FEATURE_SVELTE_FRONTEND npm --prefix web run build
node web/tests/check-output.mjs disabled
node web/tests/check-projects-output.mjs disabled
node web/tests/check-journal-output.mjs disabled
node web/tests/check-reading-output.mjs disabled
