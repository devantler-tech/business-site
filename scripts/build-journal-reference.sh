#!/usr/bin/env bash
# Build an input-bound reading reference only for the isolated Svelte candidate.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
bash scripts/check-source-history.sh
node src/data/build-content.mjs
