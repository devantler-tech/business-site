#!/usr/bin/env bash
# Git-backed article dates need complete history from this source repository.
set -euo pipefail
root="${1:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)}"
fail() {
  printf '%s\n' "source history: FAIL — $*" \
    'Use a complete business-site Git checkout. Fetch missing history with git fetch --unshallow before rebuilding.' >&2
  exit 1
}
[[ $# -le 1 && -d "$root" ]] || fail 'expected a source directory'
root="$(cd -- "$root" && pwd -P)"
git_read=(env -u GIT_DIR -u GIT_WORK_TREE -u GIT_COMMON_DIR -u GIT_INDEX_FILE \
  -u GIT_OBJECT_DIRECTORY -u GIT_ALTERNATE_OBJECT_DIRECTORIES -u GIT_PREFIX -u GIT_NAMESPACE \
  GIT_NO_LAZY_FETCH=1 git --no-replace-objects -C "$root")
repo_root="$("${git_read[@]}" rev-parse --show-toplevel 2>/dev/null)" || fail 'source has no readable Git repository'
[[ "$repo_root" == "$root" ]] || fail 'source belongs to another enclosing repository'
"${git_read[@]}" rev-parse --verify 'HEAD^{commit}' >/dev/null 2>&1 || fail 'source HEAD is unavailable'
shallow="$("${git_read[@]}" rev-parse --is-shallow-repository 2>/dev/null)" || fail 'history depth is unknown'
[[ "$shallow" == false ]] || fail 'shallow history cannot establish article modification dates'
# Keep the traversal complete: even plausible output is unusable when Git fails.
"${git_read[@]}" log --format= --name-status -- src/content/docs >/dev/null 2>&1 || fail 'content history traversal is incomplete'
printf '%s\n' 'source history: PASS — complete own-source history for committed article dates'
