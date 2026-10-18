#!/usr/bin/env bash
# The one public build entrypoint for local builds, publication and packaging.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_NAMESPACE
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1
bash scripts/check-source-history.sh
frontend="$(node web/public-selection.ts)"
case "$frontend" in
  astro) bash scripts/build-astro-site.sh ;;
  svelte)
    # This checkout owns its generated output. A failed selection must not leave
    # the reference artifact looking like successfully selected publication.
    cleanup_failed_selection() {
      local build_status=$?
      if [[ "$build_status" -ne 0 ]]; then rm -rf -- dist; fi
    }
    trap cleanup_failed_selection EXIT
    bash scripts/build-journal-reference.sh
    npm --prefix web run build
    for check in output projects-output journal-output reading-output feed-output discovery-output; do
      node "web/tests/check-$check.mjs" enabled
    done
    node web/tests/check-company-output.mjs
    bash scripts/check-portal-artifacts.sh web/build
    node scripts/public-build.mjs
    bash scripts/check-portal-artifacts.sh dist
    printf '%s\n' 'Selected verified Svelte public artifact: dist'
    ;;
  *) printf '%s\n' 'Unknown public frontend selection.' >&2; exit 1 ;;
esac
