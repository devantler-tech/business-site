#!/usr/bin/env bash
# Exercise the actual framework configuration and generated visitor artifacts.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
unset FEATURE_COMPANY_IDENTITY FEATURE_OFFER_COPY FEATURE_CUSTOMER_SCOPE FEATURE_VAT_PRICES PUBLIC_CONTENT_PROFILE COMPANY_POSTAL_ADDRESS
export FEATURE_SVELTE_FRONTEND=true
for mode in false published preview independent template unset; do
  unset FEATURE_COMPANY_IDENTITY FEATURE_OFFER_COPY FEATURE_CUSTOMER_SCOPE FEATURE_VAT_PRICES PUBLIC_CONTENT_PROFILE COMPANY_POSTAL_ADDRESS
  case "$mode" in
    false) export FEATURE_COMPANY_IDENTITY=false FEATURE_OFFER_COPY=false FEATURE_CUSTOMER_SCOPE=false FEATURE_VAT_PRICES=false ;;
    published) export FEATURE_COMPANY_IDENTITY=true FEATURE_OFFER_COPY=false FEATURE_CUSTOMER_SCOPE=true FEATURE_VAT_PRICES=false ;;
    preview|template) export FEATURE_COMPANY_IDENTITY=true FEATURE_OFFER_COPY=true FEATURE_CUSTOMER_SCOPE=true FEATURE_VAT_PRICES=true ;;
    independent) export FEATURE_COMPANY_IDENTITY=false FEATURE_OFFER_COPY=true FEATURE_CUSTOMER_SCOPE=false FEATURE_VAT_PRICES=false ;;
  esac
  export COMPANY_POSTAL_ADDRESS=$'Example & <b>company-address-fixture-187</b> 1\n1234 Exampletown'
  if [[ "$mode" == template ]]; then
    export PUBLIC_CONTENT_PROFILE=production-template
  fi
  if ! bash scripts/build-journal-reference.sh > "$scratch/astro-$mode.log" 2>&1; then
    tail -80 "$scratch/astro-$mode.log" >&2; exit 1
  fi
  if ! npm --prefix web run build > "$scratch/svelte-$mode.log" 2>&1; then
    tail -80 "$scratch/svelte-$mode.log" >&2; exit 1
  fi
  node web/tests/check-company-output.mjs
  if [[ "$mode" == published ]]; then node web/tests/company-output-controls.mjs; fi
  node scripts/check-customer-scope.mjs web/build "${FEATURE_CUSTOMER_SCOPE:-false}"
  node scripts/check-offer-rollout.mjs web/build
  printf 'Actual Astro/Svelte company configuration: %s PASS\n' "$mode"
done

# Native framework validation must reject ambiguous switches before output.
unset COMPANY_POSTAL_ADDRESS PUBLIC_CONTENT_PROFILE FEATURE_COMPANY_IDENTITY FEATURE_OFFER_COPY FEATURE_CUSTOMER_SCOPE FEATURE_VAT_PRICES
for flag in FEATURE_COMPANY_IDENTITY FEATURE_OFFER_COPY FEATURE_CUSTOMER_SCOPE FEATURE_VAT_PRICES; do
  if env "$flag=ambiguous" npm --prefix web run build > "$scratch/rejected.log" 2>&1; then
    printf 'Invalid business setting was accepted: %s\n' "$flag" >&2; exit 1
  fi
  grep -q 'true or false' "$scratch/rejected.log"
done
export FEATURE_COMPANY_IDENTITY=true
for address in '' 'company-address-fixture-187' $'company-address-fixture-187\t1\n1234 Exampletown'; do
  if COMPANY_POSTAL_ADDRESS="$address" npm --prefix web run build > "$scratch/rejected.log" 2>&1; then
    printf 'Incomplete company address was accepted\n' >&2; exit 1
  fi
  grep -q COMPANY_POSTAL_ADDRESS "$scratch/rejected.log"
  if grep -q company-address-fixture-187 "$scratch/rejected.log"; then
    printf 'Address leaked into diagnostics\n' >&2; exit 1
  fi
done
if PUBLIC_CONTENT_PROFILE=invalid npm --prefix web run build > "$scratch/rejected.log" 2>&1; then
  printf 'Unknown content profile was accepted\n' >&2; exit 1
fi
grep -q 'PUBLIC_CONTENT_PROFILE must be' "$scratch/rejected.log"
# Disabled frontend must not require the private disclosure input.
FEATURE_SVELTE_FRONTEND=false npm --prefix web run build > "$scratch/disabled.log" 2>&1
node web/tests/check-output.mjs disabled
printf 'Native invalid-config, private-diagnostic and frontend-off controls: PASS\n'
