#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
for spec in '1 svelte' '2 svelte' '3 svelte'; do
  read -r trial sample <<< "$spec"
  (
    cd "$sample"
    LC_ALL=C /usr/bin/time -p npm exec --yes --package=node@24.19.0 --package=npm@11.19.1 -- npm run build > "build-$trial.log" 2> "time-$trial.txt"
  )
  tail -n 3 "$sample/time-$trial.txt"
done
