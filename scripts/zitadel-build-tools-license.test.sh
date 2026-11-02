#!/usr/bin/env bash
# Exercise the actual CI scope checker against complete and adverse repositories.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
sdk="$root/tools/zitadel-pnpm-source"
if ! jq -e '.private == true and .license == "AGPL-3.0-only"' "$sdk/package.json" >/dev/null; then
  echo 'FAIL: the private SDK does not declare the approved scoped grant' >&2
  exit 1
fi
checker="$root/scripts/check-zitadel-build-tools-license.sh"
bash "$checker" "$root"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
scope=third_party/zitadel-build-tools/scope.json
for scenario in valid broad missing duplicate duplicate-key absolute malformed extra-field changed-terms sdk-public sdk-unlicensed lock-unlicensed scope-link source-link ancestor-link missing-source partial-reader replaced-scope; do
  fixture="$tmp/$scenario"
  mkdir "$fixture"
  while IFS= read -r member; do
    mkdir -p "$fixture/$(dirname "$member")"
    cp "$root/$member" "$fixture/$member"
  done < <(jq -r '.sourcePaths[], .licenseDocuments[]' "$root/$scope")
  case "$scenario" in
    broad) jq '.sourcePaths += ["server/internal/identity/browser.go"]' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    missing) jq '.sourcePaths |= .[1:]' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    duplicate) jq '.sourcePaths += [.sourcePaths[0]]' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    duplicate-key) awk 'NR==2 {print "  \"license\": \"UNLICENSED\","} {print}' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    absolute) jq '.sourcePaths[0] = "/tmp/foreign"' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    malformed) printf '{' > "$fixture/$scope" ;;
    extra-field) jq '.wholeRepositoryGrant = true' "$fixture/$scope" > "$fixture/pending"; mv "$fixture/pending" "$fixture/$scope" ;;
    changed-terms) printf 'different terms\n' > "$fixture/third_party/zitadel-build-tools/COPYING" ;;
    sdk-public) jq '.private = false' "$fixture/tools/zitadel-pnpm-source/package.json" > "$fixture/pending"; mv "$fixture/pending" "$fixture/tools/zitadel-pnpm-source/package.json" ;;
    sdk-unlicensed) jq '.license = "UNLICENSED"' "$fixture/tools/zitadel-pnpm-source/package.json" > "$fixture/pending"; mv "$fixture/pending" "$fixture/tools/zitadel-pnpm-source/package.json" ;;
    lock-unlicensed) jq '.packages[""].license = "UNLICENSED"' "$fixture/tools/zitadel-pnpm-source/package-lock.json" > "$fixture/pending"; mv "$fixture/pending" "$fixture/tools/zitadel-pnpm-source/package-lock.json" ;;
    scope-link) rm "$fixture/$scope"; ln -s "$root/$scope" "$fixture/$scope" ;;
    source-link) rm "$fixture/scripts/zitadel-copied-assets.go"; ln -s "$root/scripts/zitadel-copied-assets.go" "$fixture/scripts/zitadel-copied-assets.go" ;;
    ancestor-link) mv "$fixture/server/cmd/zitadel-bsr-sources" "$fixture/original-bsr"; ln -s "$fixture/original-bsr" "$fixture/server/cmd/zitadel-bsr-sources" ;;
    missing-source) rm "$fixture/server/cmd/zitadel-bsr-sources/main.go" ;;
    partial-reader)
      rm "$fixture/server/cmd/zitadel-bsr-sources/main.go"
      mkdir "$fixture/reader-bin"
      cat > "$fixture/reader-bin/jq" <<'READER'
#!/usr/bin/env bash
if [ "$1" = -r ]; then
  printf 'scripts/build-zitadel-linux.sh\n'
  exit 23
fi
exec "$LICENSE_SCOPE_REAL_JQ" "$@"
READER
      chmod +x "$fixture/reader-bin/jq"
      ;;
    replaced-scope)
      mkdir "$fixture/reader-bin" "$fixture/server/internal" "$fixture/server/internal/identity"
      printf 'not in the approved grant\n' > "$fixture/server/internal/identity/browser.go"
      cat > "$fixture/reader-bin/jq" <<'READER'
#!/usr/bin/env bash
if [ "$1" = --stream ]; then
  scope="$LICENSE_SCOPE_FIXTURE_ROOT/third_party/zitadel-build-tools/scope.json"
  "$LICENSE_SCOPE_REAL_JQ" '.sourcePaths[0] = "server/internal/identity/browser.go"' "$scope" > "$LICENSE_SCOPE_FIXTURE_ROOT/replacement"
  mv "$LICENSE_SCOPE_FIXTURE_ROOT/replacement" "$scope"
fi
exec "$LICENSE_SCOPE_REAL_JQ" "$@"
READER
      chmod +x "$fixture/reader-bin/jq"
      ;;
    valid) ;;
  esac
  reader_path="$PATH"
  if [ "$scenario" = partial-reader ] || [ "$scenario" = replaced-scope ]; then reader_path="$fixture/reader-bin:$PATH"; fi
  if LICENSE_SCOPE_FIXTURE_ROOT="$fixture" LICENSE_SCOPE_REAL_JQ="$(command -v jq)" PATH="$reader_path" bash "$checker" "$fixture" > "$tmp/result" 2> "$tmp/error"; then
    if [ "$scenario" != valid ]; then echo "FAIL: admitted $scenario scope" >&2; exit 1; fi
    jq -e '.license == "AGPL-3.0-only" and .sourceFiles == 37 and .licenseDocuments == 3 and .distributionAdmission == false' "$tmp/result" >/dev/null
  else
    if [ "$scenario" = valid ]; then echo 'FAIL: refused complete scoped grant' >&2; cat "$tmp/error" >&2; exit 1; fi
    test ! -s "$tmp/result" || { echo "FAIL: emitted success output for $scenario" >&2; exit 1; }
  fi
done
echo 'PASS: actual scoped grant and 18 complete/adverse repository controls'
