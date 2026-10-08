#!/usr/bin/env bash
# Exercise the real refresh boundary without network access or changing the checked-in snapshot.
set -euo pipefail
site_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d)
finished=false
cleanup() {
  status=$?
  rm -f -- "$scratch/bin/gh" "$scratch/scripts/refresh-public-stars.sh" "$scratch/src/data/public-products.json" "$scratch/src/data/github-stars.json" "$scratch/read.json" "$scratch/result.json" "$scratch/result.json.read" "$scratch/failure.log"
  rmdir -- "$scratch/bin" "$scratch/scripts" "$scratch/src/data" "$scratch/src" "$scratch"
  if ! $finished && [ "$status" -eq 0 ]; then status=2; fi
  exit "$status"
}
trap cleanup EXIT
mkdir -p "$scratch/bin" "$scratch/scripts" "$scratch/src/data"
cp "$site_dir/scripts/refresh-public-stars.sh" "$scratch/scripts/"
cp "$site_dir/src/data/public-products.json" "$scratch/src/data/"
cp "$site_dir/scripts/fixtures/github-public-stars.sh" "$scratch/bin/gh"
chmod +x "$scratch/bin/gh"
export PATH="$scratch/bin:$PATH" STAR_FIXTURE="$scratch/read.json"
jq '[map({name: .repository, private: false, archived: false, owner: {login: "devantler-tech"}, stargazers_count: 0})]' "$site_dir/src/data/public-products.json" > "$scratch/read.json"
bash "$scratch/scripts/refresh-public-stars.sh" > /dev/null
jq -e --slurpfile manifest "$site_dir/src/data/public-products.json" '(.repositories | length) == ($manifest[0] | length) and .repositories.ksail == 0 and (.observedAt | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$"))' "$scratch/src/data/github-stars.json" > /dev/null
cp "$scratch/src/data/github-stars.json" "$scratch/result.json"
for mode in failed missing private archived invalid duplicate; do
  case "$mode" in
    failed) export STAR_READ_FAIL=1 ;;
    missing) jq '.[0] |= .[1:]' "$scratch/read.json" > "$scratch/result.json.read" ;;
    private) jq '.[0][0].private = true' "$scratch/read.json" > "$scratch/result.json.read" ;;
    archived) jq '.[0][0].archived = true' "$scratch/read.json" > "$scratch/result.json.read" ;;
    invalid) jq '.[0][0].stargazers_count = null' "$scratch/read.json" > "$scratch/result.json.read" ;;
    duplicate) jq '.[0] += [.[0][0]]' "$scratch/read.json" > "$scratch/result.json.read" ;;
  esac
  if [ "$mode" != failed ]; then export STAR_FIXTURE="$scratch/result.json.read"; fi
  if bash "$scratch/scripts/refresh-public-stars.sh" > "$scratch/failure.log" 2>&1; then
    printf 'FAIL: %s GitHub response was accepted\n' "$mode" >&2; exit 1
  fi
  cmp "$scratch/result.json" "$scratch/src/data/github-stars.json"
  if [ "$mode" != failed ]; then
    grep -q 'Incomplete or invalid public GitHub read' "$scratch/failure.log"
    rm -f -- "$scratch/result.json.read"
  fi
  export STAR_READ_FAIL=0 STAR_FIXTURE="$scratch/read.json"
done
printf 'Public star refresh preserves the snapshot on failed and incomplete reads.\n'
finished=true
