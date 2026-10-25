#!/usr/bin/env bash
# Build, never deploy: selected public output + its exact Go runtime and notices.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
mode="${1:---release}"
variant="${2:---default}"
frontend="${3:---astro}"
[[ "$#" -le 3 && ( "$mode" == --release || "$mode" == --development ) && ( "$variant" == --default || "$variant" == --production-template ) && ( "$frontend" == --astro || "$frontend" == --svelte ) ]] || { printf '%s\n' 'Use --release or --development, --default or --production-template, then optionally --svelte.' >&2; exit 1; }
profile=default
output=runtime-dist
if [[ "$variant" == --production-template ]]; then profile=production-template; output=runtime-production-dist; fi
selected=false
if [[ "$frontend" == --svelte ]]; then
  selected=true
  # Production's reviewed Svelte package feeds the existing image/archive chain.
  # Keep the extra default-profile candidate separate from incumbent comparisons.
  if [[ "$profile" == default ]]; then output=runtime-svelte-default-dist; fi
fi
revision="$(git rev-parse HEAD)"
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || exit 1
if [[ "$mode" == --release && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf '%s\n' 'Release packages require a clean checkout; commit the source or use --development.' >&2; exit 1
fi
node_line="$(cat .node-version)"
[[ "$node_line" =~ ^[1-9][0-9]*$ && "$(node --version)" == "v${node_line}."* && "$(npm --version)" == 11.* ]] || { printf 'Use Node %s and npm 11 before packaging.\n' "$node_line" >&2; exit 1; }
[[ "$(go version)" == "go version go1.27.3 "* ]] || { printf '%s\n' 'Use the exact Go 1.27.3 toolchain before packaging.' >&2; exit 1; }
export GOTOOLCHAIN=local LC_ALL=C
mkdir -p "$output"
inputs=""
cleanup_inputs() {
  if [[ -n "$inputs" ]]; then rm -f -- "$inputs/stars.json"; rmdir -- "$inputs"; fi
}
trap cleanup_inputs EXIT
build_env=(env -u FEATURE_SVELTE_FRONTEND -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_OFFER_COPY -u FEATURE_FEATURED_PORTFOLIO -u FEATURE_COMPANY_IDENTITY -u FEATURE_CUSTOMER_SCOPE -u COMPANY_POSTAL_ADDRESS -u PUBLIC_STARS_SNAPSHOT -u PUBLIC_CONTENT_PROFILE)
build_env+=("FEATURE_SVELTE_FRONTEND=$selected")
build_env+=("PUBLIC_CONTENT_PROFILE=$profile" "FEATURE_COMPANY_IDENTITY=false" "COMPANY_POSTAL_ADDRESS=" "FEATURE_PREVIEW_BANNER=false" "FEATURE_OFFER_COPY=false" "FEATURE_CUSTOMER_SCOPE=false" "FEATURE_CLIENT_PORTFOLIO=true" "FEATURE_FEATURED_PORTFOLIO=true")
if [[ "$profile" == production-template ]]; then
  inputs="$(mktemp -d "$output/public-inputs.XXXXXX")"
  bash scripts/refresh-public-stars.sh "$inputs/stars.json"
  build_env+=("PUBLIC_CONTENT_PROFILE=production-template" "FEATURE_COMPANY_IDENTITY=true" "PUBLIC_STARS_SNAPSHOT=$PWD/$inputs/stars.json")
fi
"${build_env[@]}" npm run build
bash scripts/check-portal-artifacts.sh dist
if [[ "$mode" == --release && -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  printf '%s\n' 'The public build changed release source; investigate before packaging.' >&2; exit 1
fi
package="$(mktemp -d "$output/package.XXXXXX")"
mkdir -p "$package/public" "$package/notices"
cp -R dist/. "$package/public/"
digest="$(cd server && go run ./cmd/business-site --check-public --public "../$package/public")"
[[ "$digest" =~ ^[0-9a-f]{64}$ ]] || { printf '%s\n' 'Public artifact validation did not return a digest.' >&2; exit 1; }
(
  cd server
  CGO_ENABLED=0 go build -trimpath -ldflags "-X main.revision=$revision -X main.publicDigest=$digest -X main.buildMode=${mode#--} -X main.publicProfile=$profile" -o "../$package/business-site" ./cmd/business-site
  bash ../scripts/check-runtime-dependencies.sh "../$package/business-site" DEPENDENCIES.md
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
bash scripts/package-go-notices.sh "$package/notices"
cp server/DEPENDENCIES.md "$package/notices/DEPENDENCIES.md"
cp server/README.md "$package/README.md"
cp server/MIGRATIONS.md "$package/MIGRATIONS.md"
"$package/business-site" --version > "$package/build-receipt.json"
# ZIP consumers need not retain executable modes. Preserve the complete package
# inside a tar archive before any artifact service handles it.
tar -czf "$output/business-site-runtime.tar.gz" -C "$output" "${package#"$output"/}"
printf 'RUNTIME_PACKAGE=%s\n' "$package"
