#!/usr/bin/env bash
# Build the published site and verify its visitor journeys.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
node --test scripts/theme.test.mjs
node --test scripts/public-products.test.mjs
bash scripts/refresh-public-stars.test.sh
astro build
node scripts/check-business-site.mjs dist
