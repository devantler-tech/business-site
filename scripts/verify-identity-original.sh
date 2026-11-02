#!/usr/bin/env bash
# Shared data-only readback for native CI and the trusted manual publisher.
set -euo pipefail
export LC_ALL=C GOTOOLCHAIN=local
[[ "${IDENTITY_RELEASE_MODE:-}" == dry-run || "${IDENTITY_RELEASE_MODE:-}" == publish ]] || exit 1
work=${IDENTITY_RELEASE_WORK:-}
[[ "$work" == "${RUNNER_TEMP:?}"/identity-forge.* && "$work" != *$'\n'* && -d "$work/restored/native" ]] || exit 1
native="$work/restored/native"
handoff="$native/zitadel-handoff/handoff.json"
base="$(jq -er '.files[]|select(.path=="zitadel-base-source/base-source.tar")|.sha256' "$handoff")"
base_manifest="$(jq -er .manifestSha256 "$native/zitadel-base-source/evidence/capture.json")"
"$RUNNER_TEMP/identity-release" unpack-base-source --input "$native/zitadel-base-source/base-source.tar" --archive-sha256 "$base" --sha256 "$base_manifest" --output "$work/base" >"$work/base-restore.json"
"$RUNNER_TEMP/identity-release" verify-base-auth --input "$work/base" --sha256 "$base_manifest" --repository "$GITHUB_WORKSPACE" --output "$work/base-auth" >"$work/base-auth.json"
archive="$(jq -er '.files[]|select(.path=="zitadel-offline-replay/inputs.tar")|.sha256' "$handoff")"
inputs="$(jq -er .inputManifestSha256 "$handoff")"
manifest="$(jq -er .sourceManifestSha256 "$handoff")"
go run scripts/zitadel-source-bundle.go --unpack "$native/zitadel-offline-replay/inputs.tar" "$archive" "$inputs" "$work/inputs" >"$work/inputs-restore.json"
cmp "$work/inputs/source/build-receipt.json" "$native/zitadel-build/build-receipt.json"
go run scripts/zitadel-source-bundle.go --restore-build "$work/inputs/source/source-snapshot" "$manifest" "$work/source" >"$work/source-restore.json"
go run scripts/zitadel-source-bundle.go --restore-notice-originals "$work/inputs/source/selected-notice-originals" "$work/inputs/source/source-snapshot" "$manifest" "$work/notices" >"$work/notice-restore.json"
go run scripts/zitadel-source-bundle.go --verify-build-records "$work/inputs/source/source-snapshot" "$manifest" "$GITHUB_WORKSPACE" "$work/notices" >"$work/build-records.json"
go run scripts/zitadel-image-package.go scripts/zitadel-image-smoke.go scripts/zitadel-service-smoke.go scripts/zitadel-browser.go --verify-saved --native "$native" --notices "$work/notices" --output "$work/saved"
# An explicit dry-run owns no later publisher; remove only the exact freshly
# loaded ID which the saved-image verifier just admitted.
if [[ "${IDENTITY_RELEASE_MODE:?}" == dry-run ]]; then
  image="$(jq -er '.localImageId|select(test("^sha256:[0-9a-f]{64}$"))' "$work/saved/saved-image.json")"
  docker image rm "$image"
fi
