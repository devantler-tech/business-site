#!/usr/bin/env bash

set -euo pipefail

attempt=1
max_attempts=2

while [ "${attempt}" -le "${max_attempts}" ]; do
  log_file="$(mktemp)"
  status=0
  npm audit --omit=dev >"${log_file}" 2>&1 || status=$?

  if [ "${status}" -eq 0 ]; then
    cat "${log_file}"
    rm -f "${log_file}"
    exit 0
  fi

  cat "${log_file}" >&2
  endpoint_failed=false
  grep -Fq 'npm error audit endpoint returned an error' "${log_file}" && endpoint_failed=true
  rm -f "${log_file}"

  if [ "${endpoint_failed}" = true ] && [ "${attempt}" -lt "${max_attempts}" ]; then
    printf 'npm audit endpoint failed; retrying once\n' >&2
    attempt=$((attempt + 1))
    continue
  fi

  exit "${status}"
done
