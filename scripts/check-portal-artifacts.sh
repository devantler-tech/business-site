#!/usr/bin/env bash
# Static publication must contain public pages, never the latent server module.
set -euo pipefail
root="${1:-dist}"
[[ -f "$root/index.html" && -f "$root/da/index.html" ]] || {
  printf '%s\n' 'Portal artifact boundary: missing EN/DA build' >&2
  exit 1
}
inventory="$(mktemp)"
trap 'rm -f "$inventory"' EXIT
find "$root" -mindepth 1 -print0 > "$inventory"
while IFS= read -r -d '' file; do
  path="${file#"$root"/}"
  [[ ! -L "$file" ]] || { printf '%s\n' 'Portal artifact boundary: symlink refused' >&2; exit 1; }
  case "$path" in
    server|server/*|portal|portal/*|*/server/*|*/portal/*|*.go|*.sql|go.mod|*/go.mod|go.sum|*/go.sum|.env|*/.env|.env.*|*/.env.*)
      printf '%s\n' 'Portal artifact boundary: private source or unreleased route refused' >&2
      exit 1 ;;
  esac
  [[ -f "$file" ]] || continue
  [[ -r "$file" ]] || { printf '%s\n' 'Portal artifact boundary: unreadable file' >&2; exit 1; }
  rc=0
  grep -aEq 'portal_login_attempts|portal_invitations|portal_memberships|PORTAL_TEST_DATABASE_URL|portal_fixture_owner|fixture-only' "$file" || rc=$?
  case "$rc" in
    0) printf '%s\n' 'Portal artifact boundary: private identity content refused' >&2; exit 1 ;;
    1) ;;
    *) printf '%s\n' 'Portal artifact boundary: content read failed' >&2; exit 1 ;;
  esac
done < "$inventory"
printf '%s\n' 'Portal artifact boundary: PASS'
