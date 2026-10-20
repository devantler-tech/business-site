#!/usr/bin/env bash
# Build an input-bound reading reference only for the isolated Svelte candidate.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
node scripts/journal-reading-reference.mjs prepare
npm run build:astro
node scripts/journal-reading-reference.mjs capture
