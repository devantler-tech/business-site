#!/usr/bin/env bash
# The sole public application is Svelte; the backend is packaged separately in Go.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_NAMESPACE
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1
cleanup_failed_build() {
  local build_status=$?
  if [[ "$build_status" -ne 0 ]]; then rm -rf -- dist; fi
}
trap cleanup_failed_build EXIT
bash scripts/check-source-history.sh
node web/public-selection.ts
node scripts/check-editorial-assets.mjs
bash scripts/build-journal-reference.sh
build_nonce="$(node --input-type=module -e 'import {randomUUID} from "node:crypto"; console.log(randomUUID())')"
SVELTE_PUBLIC_BUILD_NONCE="$build_nonce" npm --prefix web run build
for check in output projects-output journal-output reading-output feed-output discovery-output; do
  node "web/tests/check-$check.mjs" enabled
done
node web/tests/check-company-output.mjs
node scripts/check-business-site.mjs web/build
bash scripts/check-portal-artifacts.sh web/build
node scripts/public-build.mjs "$build_nonce"
bash scripts/check-portal-artifacts.sh dist
printf '%s\n' 'Verified Svelte public artifact: dist'
