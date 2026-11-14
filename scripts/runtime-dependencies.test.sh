#!/usr/bin/env bash
# Real executable positive control and independent inventory/failed-read refusals.
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
(cd "$root/server" && go build -trimpath -o "$scratch/business-site" ./cmd/business-site)
checker="$root/scripts/check-runtime-dependencies.sh"
inventory="$root/server/DEPENDENCIES.md"
bash "$checker" "$scratch/business-site" "$inventory"
reject() {
  local expected="$1"
  shift
  local status=0
  "$@" >"$scratch/result" 2>&1 || status=$?
  [[ "$status" -eq "$expected" ]] || {
    echo "Expected rejection $expected, got $status." >&2
    cat "$scratch/result" >&2
    exit 1
  }
  if grep -q 'inventory verified' "$scratch/result"; then
    echo 'Failed evidence emitted a clean verdict.' >&2
    exit 1
  fi
}
sed '/^| golang.org\/x\/net /d' "$inventory" >"$scratch/missing.md"
reject 1 bash "$checker" "$scratch/business-site" "$scratch/missing.md"
# Derive the stale row from the inventory itself, so a dependency bump cannot turn this into a no-op.
sed -E '/^[|] golang[.]org[/]x[/]net /s/[|] v[^ |]+ /| v0.0.0-stale /' "$inventory" >"$scratch/stale.md"
if cmp -s "$inventory" "$scratch/stale.md"; then
  echo 'The stale fixture did not change the inventory.' >&2
  exit 1
fi
reject 1 bash "$checker" "$scratch/business-site" "$scratch/stale.md"
cp "$inventory" "$scratch/extra.md"
printf '| go.uber.org/mock | v0.6.0 | test-only module |\n' >>"$scratch/extra.md"
reject 1 bash "$checker" "$scratch/business-site" "$scratch/extra.md"
cp "$inventory" "$scratch/duplicate.md"
sed -n '/^| golang.org\/x\/net /p' "$inventory" >>"$scratch/duplicate.md"
reject 1 bash "$checker" "$scratch/business-site" "$scratch/duplicate.md"
printf '| broken row |\n' >"$scratch/malformed.md"
reject 1 bash "$checker" "$scratch/business-site" "$scratch/malformed.md"
reject 1 bash "$checker" "$scratch/business-site" "$scratch/absent.md"
reject 2 bash "$checker" "$scratch/missing-binary" "$inventory"
go version -m "$scratch/business-site" >"$scratch/metadata"
mkdir "$scratch/bin"
printf '#!/usr/bin/env bash\ncat "%s"\nexit "%s"\n' "\$DEPENDENCY_TEST_METADATA" "\${DEPENDENCY_TEST_STATUS:-0}" >"$scratch/bin/go"
chmod 755 "$scratch/bin/go"
# Complete matching output followed by failure must not become a clean verdict.
reject 2 env PATH="$scratch/bin:$PATH" DEPENDENCY_TEST_METADATA="$scratch/metadata" DEPENDENCY_TEST_STATUS=42 bash "$checker" "$scratch/business-site" "$inventory"
sed '/golang.org\/x\/net/,$d' "$scratch/metadata" >"$scratch/partial"
reject 2 env PATH="$scratch/bin:$PATH" DEPENDENCY_TEST_METADATA="$scratch/partial" DEPENDENCY_TEST_STATUS=42 bash "$checker" "$scratch/business-site" "$inventory"
: >"$scratch/empty"
reject 2 env PATH="$scratch/bin:$PATH" DEPENDENCY_TEST_METADATA="$scratch/empty" bash "$checker" "$scratch/business-site" "$inventory"
sed 's@server/cmd/business-site@server/cmd/other@' "$scratch/metadata" >"$scratch/other"
reject 2 env PATH="$scratch/bin:$PATH" DEPENDENCY_TEST_METADATA="$scratch/other" bash "$checker" "$scratch/business-site" "$inventory"
cp "$scratch/metadata" "$scratch/replacement"
printf '\t=>\t./local-module\t(devel)\n' >>"$scratch/replacement"
reject 2 env PATH="$scratch/bin:$PATH" DEPENDENCY_TEST_METADATA="$scratch/replacement" bash "$checker" "$scratch/business-site" "$inventory"
echo 'Runtime inventory drift and incomplete-metadata refusals passed.'
