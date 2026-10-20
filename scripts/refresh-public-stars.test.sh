#!/usr/bin/env bash
# Exercise the real refresh boundary without network access or changing the checked-in snapshot.
set -euo pipefail
site_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
scratch=$(mktemp -d)
finished=false
cleanup() {
  status=$?
  rm -f -- "$scratch/bin/gh" "$scratch/scripts/refresh-public-stars.sh" "$scratch/src/data/public-products.json" "$scratch/src/data/github-stars.json" "$scratch/read.json" "$scratch/result.json" "$scratch/result.json.read" "$scratch/failure.log" "$scratch/custom.json"
  rmdir -- "$scratch/bin" "$scratch/scripts" "$scratch/src/data" "$scratch/src" "$scratch"
  if ! $finished && [ "$status" -eq 0 ]; then status=2; fi
  exit "$status"
}
trap cleanup EXIT
mkdir -p "$scratch/bin" "$scratch/scripts" "$scratch/src/data"
cp "$site_dir/scripts/refresh-public-stars.sh" "$scratch/scripts/"
cp "$site_dir/src/data/public-products.json" "$scratch/src/data/"
catalogue="$scratch/src/data/public-products.json"
# Exercise the rename path even when no catalogued repository is renamed today: give the last entry a
# different GitHub name in the scratch copy only.
renamed=$(jq 'if any(.[]; has("githubRepository")) then . else .[-1].githubRepository = ("renamed-" + .[-1].repository) end' "$catalogue")
printf '%s\n' "$renamed" > "$catalogue"
jq -e 'any(.[]; has("githubRepository"))' "$catalogue" > /dev/null
cp "$site_dir/scripts/fixtures/github-public-stars.sh" "$scratch/bin/gh"
chmod +x "$scratch/bin/gh"
export PATH="$scratch/bin:$PATH" STAR_FIXTURE="$scratch/read.json"
# A renamed repository answers with sentinel values, so the test can see they land under its catalogue name.
jq '[map({name: (.githubRepository // .repository), private: false, archived: (has("githubRepository")), fork: false, owner: {login: "devantler-tech"}, stargazers_count: (if has("githubRepository") then 17 else 0 end)})]' "$catalogue" > "$scratch/read.json"
bash "$scratch/scripts/refresh-public-stars.sh" > /dev/null
jq -e --slurpfile manifest "$catalogue" '(.repositories | length) == ($manifest[0] | length) and .repositories.ksail == 0 and (.observedAt | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}$"))' "$scratch/src/data/github-stars.json" > /dev/null
# Every count is keyed by its catalogue name, including a repository GitHub now lists under a new name.
jq -e --slurpfile manifest "$catalogue" '(.repositories | keys) == ($manifest[0] | map(.repository) | sort) and (.metadata | keys) == ($manifest[0] | map(.repository) | sort) and
  (. as $snapshot | [$manifest[0][] | .repository as $key | has("githubRepository") as $renamed |
    ($renamed == ($snapshot.repositories[$key] == 17)) and ($renamed == $snapshot.metadata[$key].archived)] | all)' "$scratch/src/data/github-stars.json" > /dev/null
cp "$scratch/src/data/github-stars.json" "$scratch/result.json"
bash "$scratch/scripts/refresh-public-stars.sh" "$scratch/custom.json" > /dev/null
jq -e '.repositories.ksail == 0' "$scratch/custom.json" > /dev/null
cmp "$scratch/result.json" "$scratch/src/data/github-stars.json"
for mode in failed missing private malformed-archived malformed-fork invalid duplicate; do
  case "$mode" in
    failed) export STAR_READ_FAIL=1 ;;
    missing) jq '.[0] |= .[1:]' "$scratch/read.json" > "$scratch/result.json.read" ;;
    private) jq '.[0][0].private = true' "$scratch/read.json" > "$scratch/result.json.read" ;;
    malformed-archived) jq '.[0][0].archived = null' "$scratch/read.json" > "$scratch/result.json.read" ;;
    malformed-fork) jq 'del(.[0][0].fork)' "$scratch/read.json" > "$scratch/result.json.read" ;;
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
for field in archived fork; do
  jq --arg field "$field" '.[0][0][$field] = true' "$scratch/read.json" > "$scratch/result.json.read"
  export STAR_FIXTURE="$scratch/result.json.read"
  bash "$scratch/scripts/refresh-public-stars.sh" > /dev/null
  first_repository=$(jq -r '.[0].repository' "$catalogue")
  jq -e --arg repo "$first_repository" --arg field "$field" '.metadata[$repo][$field] == true and (.fetchedAt | test("^[0-9T:Z-]+$"))' "$scratch/src/data/github-stars.json" > /dev/null
  rm -f -- "$scratch/result.json.read"
done
printf 'Public star refresh preserves the snapshot on failed and incomplete reads.\n'
finished=true
