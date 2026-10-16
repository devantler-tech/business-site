#!/usr/bin/env bash
# Build the published site and verify its visitor journeys.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
# The preflight and renderer must read the same unmodified source history.
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_NAMESPACE
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1
bash scripts/check-source-history.sh
node --test scripts/theme.test.mjs
node --test scripts/visitor-privacy.test.mjs
node --test scripts/journal-navigation.test.mjs
node --test scripts/public-products.test.mjs
node --test scripts/public-tools.test.mjs
node --test scripts/public-profile.test.mjs
node --test scripts/package-profile-contract.test.mjs
node --test scripts/reader-text.test.mjs
node --test scripts/featured-rollout.test.mjs
node --test scripts/fresh-publication.test.mjs
node --test scripts/client-rollout.test.mjs
node --test scripts/company-rollout.test.mjs
node --test scripts/company-address.test.mjs
node --test scripts/journal-rollout.test.mjs
node --test scripts/journal-style.test.mjs
node --test scripts/editorial-assets.test.mjs
node --test scripts/compress-editorial-assets.test.mjs
node scripts/check-editorial-assets.mjs
bash scripts/refresh-public-stars.test.sh
astro build
bash scripts/journal-history.test.sh dist
node --test scripts/company-disclosure.test.mjs
node scripts/check-offer-rollout.mjs dist
node scripts/check-business-site.mjs dist
node scripts/check-customer-scope.mjs dist
node scripts/customer-scope.test.mjs dist
node scripts/check-client-facing.mjs dist
node scripts/check-featured-portfolio.mjs dist
node scripts/check-journal-presentation.mjs dist
bash scripts/retired-public-output.test.sh dist
bash scripts/check-portal-artifacts.test.sh
bash scripts/check-portal-artifacts.sh dist
