#!/usr/bin/env bash
# Build the published site and verify its visitor journeys.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
node --test scripts/theme.test.mjs
node --test scripts/journal-navigation.test.mjs
node --test scripts/public-products.test.mjs
node --test scripts/client-rollout.test.mjs
node --test scripts/journal-rollout.test.mjs
node --test scripts/journal-style.test.mjs
node --test scripts/editorial-assets.test.mjs
node --test scripts/compress-editorial-assets.test.mjs
node scripts/check-editorial-assets.mjs
bash scripts/refresh-public-stars.test.sh
astro build
node scripts/check-business-site.mjs dist
node scripts/check-client-facing.mjs dist
node scripts/check-journal-presentation.mjs dist
bash scripts/retired-public-output.test.sh dist
