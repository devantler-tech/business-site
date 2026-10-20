#!/usr/bin/env bash
# Compare the authored inventory with modules in the actual packaged executable.
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
[[ "$#" -eq 2 ]] || {
  echo 'Supply the compiled business-site binary and dependency inventory.' >&2
  exit 2
}
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
if ! go version -m "$1" >"$scratch/build-info" 2>"$scratch/error"; then
  echo 'Runtime dependency metadata could not be read; rebuild with the declared Go compiler.' >&2
  exit 2
fi
awk '
  $1 == "path" { if ($2 != "github.com/devantler-tech/business-site/server/cmd/business-site") bad=1; path++ }
  $1 == "mod" { if ($2 != "github.com/devantler-tech/business-site/server") bad=1; module++ }
  $1 == "=>" { bad=1 }
  $1 == "dep" {
    if (NF != 4 || $2 !~ /^[a-zA-Z0-9.-]+\// || $3 !~ /^v[0-9]/ || seen[$2]++) bad=1
    print $2 " " $3; count++
  }
  END { if (bad || path != 1 || module != 1 || count == 0) exit 1 }
' "$scratch/build-info" >"$scratch/compiled" || {
  echo 'Runtime dependency metadata is incomplete or unsupported.' >&2
  exit 2
}
awk -F '|' '
  function trim(value) { gsub(/^[[:space:]]+|[[:space:]]+$/, "", value); return value }
  /^\|/ {
    name=trim($2); version=trim($3)
    if (name == "Compiled module" || name ~ /^-+$/) next
    if (NF != 5 || name !~ /^[a-zA-Z0-9.-]+\// || version !~ /^v[0-9][^[:space:]]*$/ || seen[name]++) bad=1
    print name " " version; count++
  }
  END { if (bad || count == 0) exit 1 }
' "$2" >"$scratch/authored" || {
  echo 'Dependency inventory is unreadable or malformed; correct its module table.' >&2
  exit 1
}
sort "$scratch/compiled" >"$scratch/compiled-sorted"
sort "$scratch/authored" >"$scratch/authored-sorted"
if ! diff -u "$scratch/authored-sorted" "$scratch/compiled-sorted"; then
  echo 'Dependency inventory differs from the executable; update server/DEPENDENCIES.md before packaging.' >&2
  exit 1
fi
printf 'Compiled runtime dependency inventory verified (%s modules).\n' "$(wc -l <"$scratch/compiled-sorted" | tr -d ' ')"
