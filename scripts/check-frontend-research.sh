#!/usr/bin/env bash
set -euo pipefail
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo"
for sample in svelte; do
  npm --prefix "research/frontend-4019/$sample" ci --ignore-scripts
  if [[ "$sample" == svelte ]]; then
    node --test research/frontend-4019/svelte/merge-security.test.ts
  fi
  npm --prefix "research/frontend-4019/$sample" audit
  npm --prefix "research/frontend-4019/$sample" run build
done
node research/frontend-4019/probe-production.mjs
