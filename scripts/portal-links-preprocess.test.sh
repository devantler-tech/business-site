#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
[[ "$#" -eq 0 || ( "$#" -eq 1 && "$1" == --native ) ]] || { echo 'Use no arguments or --native.' >&2; exit 1; }
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
cd "$root/server"
go test ./internal/identity -run '^TestPortalLink' -count=1 -timeout=60s
for template in portal contact requests triage credits; do
  bash "$root/scripts/portal-links-preprocess.sh" "$root/server/internal/identity/web/$template.html" >"$scratch/$template.html"
  [[ -s "$scratch/$template.html" ]]
  grep -q 'method-aware link contract:' "$scratch/$template.html"
  grep -q 'data-route-checked-' "$scratch/$template.html"
  if grep -q '{{' "$scratch/$template.html"; then echo 'portal-links: unrendered template' >&2; exit 1; fi
done
# Byte identity also preserves every normal public/document target for Lychee.
bash "$root/scripts/portal-links-preprocess.sh" "$root/server/README.md" >"$scratch/README.md"
cmp "$root/server/README.md" "$scratch/README.md"
bash "$root/scripts/portal-links-preprocess.sh" "$root/README.md" >"$scratch/root-README.md"
cmp "$root/README.md" "$scratch/root-README.md"
# Observe selector inheritance without requiring an older compiler in source CI.
# The shim checks the selector, then delegates to the actual admitted compiler.
mkdir "$scratch/bin"
compiler="$(command -v go)"
printf "#!/usr/bin/env bash\n[[ \"\$GOTOOLCHAIN\" == go1.27.2 ]] || exit 41\nexec \"%s\" \"\$@\"\n" "$compiler" >"$scratch/bin/go"
chmod 755 "$scratch/bin/go"
PATH="$scratch/bin:$PATH" GOTOOLCHAIN=go1.27.2 bash "$root/scripts/portal-links-preprocess.sh" "$root/server/README.md" >"$scratch/selected-README.md"
cmp "$root/server/README.md" "$scratch/selected-README.md"
for selector in auto go1.27.2+auto go1.26.3; do
  if GOTOOLCHAIN="$selector" bash "$root/scripts/portal-links-preprocess.sh" "$root/server/README.md" >"$scratch/unsafe-out" 2>"$scratch/unsafe-error"; then
    echo 'portal-links: an unbound compiler selector was accepted' >&2; exit 1
  fi
  [[ ! -s "$scratch/unsafe-out" ]]
  grep -q 'without auto selection' "$scratch/unsafe-error"
done
if bash "$root/scripts/portal-links-preprocess.sh" "$scratch/README.md" >"$scratch/out" 2>"$scratch/error"; then
  echo 'portal-links: outside-repository input was accepted' >&2; exit 1
fi
[[ ! -s "$scratch/out" ]]
if [[ "$#" -eq 1 ]]; then
  # Only this generated directory is inside the repository. Ordinary targets
  # remain visible to the actual checker, including a deliberately broken one.
  controls="$(mktemp -d "$root/server/.portal-link-controls.XXXXXX")"
  trap 'rm -rf -- "$scratch" "$controls"' EXIT
  lychee --no-progress --max-retries 0 --timeout 10 --preprocess "$root/scripts/portal-links-preprocess.sh" "$root/server/internal/identity/web/portal.html" "$root/server/internal/identity/web/contact.html" "$root/server/internal/identity/web/requests.html" "$root/server/internal/identity/web/triage.html" "$root/server/internal/identity/web/credits.html"
  printf '<html><body><a href="https://devantler.tech/portal-link-contract-missing-01a132ba/">Broken link control</a></body></html>\n' >"$controls/broken.html"
  if lychee --no-progress --max-retries 0 --timeout 10 --preprocess "$root/scripts/portal-links-preprocess.sh" "$controls/broken.html" >"$scratch/broken-receipt" 2>&1; then
    echo 'portal-links: real Lychee accepted a broken ordinary link' >&2; exit 1
  fi
  grep -q '\[404\]' "$scratch/broken-receipt"
  printf '<html><body><a href="https://devantler.tech/">Home</a></body></html>\n' >"$scratch/outside.html"
  if lychee --no-progress --max-retries 0 --timeout 10 --preprocess "$root/scripts/portal-links-preprocess.sh" "$scratch/outside.html" >"$scratch/adapter-receipt" 2>&1; then
    echo 'portal-links: real Lychee accepted a failed adapter' >&2; exit 1
  fi
  grep -q 'preprocess' "$scratch/adapter-receipt"
  printf 'Actual Lychee retains broken-link and preprocessing failure rejection\n'
fi
printf 'Portal rendered-link cases, malformed targets and complete passthrough passed\n'
