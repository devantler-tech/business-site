#!/usr/bin/env bash
# Complete, dated public-product snapshot for monorepo#3931. Never replace it on a partial read.
set -euo pipefail
site_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
manifest="$site_dir/src/data/public-products.json"
destination="$site_dir/src/data/github-stars.json"
scratch=$(mktemp -d "$site_dir/src/data/.github-stars.XXXXXX")
finished=false
cleanup() {
  status=$?
  rm -f -- "$scratch/repos.json" "$scratch/snapshot.json"
  rmdir -- "$scratch"
  if ! $finished && [ "$status" -eq 0 ]; then status=2; fi
  exit "$status"
}
trap cleanup EXIT
gh api --paginate --slurp 'orgs/devantler-tech/repos?type=public&per_page=100' > "$scratch/repos.json"
jq -e --slurpfile catalogue "$manifest" --arg observedAt "$(date -u +%Y-%m-%d)" --arg fetchedAt "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  ($catalogue[0] | map(.repository)) as $names |
  if ($names | length) == 0 or ($names | unique | length) != ($names | length) then error("Invalid public product catalogue") else . end |
  [ .[][] | select(.name as $name | $names | index($name)) ] as $repos |
  if ($repos | length) != ($names | length) or ($repos | map(.name) | unique | length) != ($names | length) or
    any($repos[]; .private != false or (.archived | type) != "boolean" or (.fork | type) != "boolean" or .owner.login != "devantler-tech" or (.stargazers_count | type) != "number" or .stargazers_count < 0 or (.stargazers_count | floor) != .stargazers_count)
  then error("Incomplete or invalid public GitHub read; existing star snapshot retained")
  else { observedAt: $observedAt, fetchedAt: $fetchedAt, repositories: ($repos | sort_by(.name) | map({key: .name, value: .stargazers_count}) | from_entries), metadata: ($repos | sort_by(.name) | map({key: .name, value: {private, archived, fork}}) | from_entries) } end
' "$scratch/repos.json" > "$scratch/snapshot.json"
mv -- "$scratch/snapshot.json" "$destination"
printf 'Refreshed public product stars: %s\n' "$destination"
finished=true
