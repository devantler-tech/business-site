#!/usr/bin/env bash
# Build, never deploy: public Astro output + its exact Go runtime and notices.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mode="${1:---release}"
[[ "$#" -le 1 && ( "$mode" == --release || "$mode" == --development ) ]] || { printf '%s\n' 'Use --release for clean source or --development for a labelled local package.' >&2; exit 1; }
revision="$(git rev-parse HEAD)"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || exit 1
if [[ "$mode" == --release && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf '%s\n' 'Release packages require a clean checkout; commit the source or use --development.' >&2; exit 1
fi
[[ "$(node --version)" == v24.* && "$(npm --version)" == 11.* ]] || { printf '%s\n' 'Use Node 24 and npm 11 before packaging.' >&2; exit 1; }
[[ "$(go version)" == "go version go1.27.2 "* ]] || { printf '%s\n' 'Use the exact Go 1.27.2 toolchain before packaging.' >&2; exit 1; }
export GOTOOLCHAIN=local LC_ALL=C
env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_OFFER_COPY -u FEATURE_FEATURED_PORTFOLIO -u FEATURE_COMPANY_IDENTITY npm run build
bash scripts/check-portal-artifacts.sh dist
if [[ "$mode" == --release && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf '%s\n' 'The public build changed release source; investigate before packaging.' >&2; exit 1
fi
mkdir -p runtime-dist
package="$(mktemp -d runtime-dist/package.XXXXXX)"
mkdir -p "$package/public" "$package/notices"
cp -R dist/. "$package/public/"
digest="$(cd server && go run ./cmd/business-site --check-public --public "../$package/public")"
[[ "$digest" =~ ^[0-9a-f]{64}$ ]] || { printf '%s\n' 'Public artifact validation did not return a digest.' >&2; exit 1; }
(
  cd server
  CGO_ENABLED=0 go build -trimpath -ldflags "-X main.revision=$revision -X main.publicDigest=$digest -X main.buildMode=${mode#--}" -o "../$package/business-site" ./cmd/business-site
  # Every actually linked third-party module, not test-only dependencies.
  go list -deps -f '{{if and .Module (not .Module.Main)}}{{.Module.Path}}{{"\t"}}{{.Module.Version}}{{"\t"}}{{.Module.Dir}}{{end}}' ./cmd/business-site | sort -u | while IFS=$'\t' read -r module version directory; do
    [[ -n "$module" ]] || continue
    notice_dir="../$package/notices/${module//\//_}@$version"
    mkdir -p "$notice_dir"
    count=0
    listing="../$package/.notices-inventory"
    find "$directory" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' -o -iname 'PATENTS*' \) -print0 > "$listing"
    while IFS= read -r -d '' notice; do
      cp "$notice" "$notice_dir/"; count=$((count + 1))
    done < "$listing"
    rm -- "$listing"
    [[ "$count" -gt 0 ]] || { printf '%s\n' 'A linked module lacks packaged upstream license text; investigate before distribution.' >&2; exit 1; }
  done
)
cp "$(go env GOROOT)/LICENSE" "$package/notices/Go-LICENSE"
cp "$(go env GOROOT)/PATENTS" "$package/notices/Go-PATENTS"
cp server/DEPENDENCIES.md "$package/notices/DEPENDENCIES.md"
cp server/README.md "$package/README.md"
"$package/business-site" --version > "$package/build-receipt.json"
# ZIP consumers need not retain executable modes. Preserve the complete package
# inside a tar archive before any artifact service handles it.
tar -czf runtime-dist/business-site-runtime.tar.gz -C runtime-dist "${package#runtime-dist/}"
printf 'RUNTIME_PACKAGE=%s\n' "$package"
