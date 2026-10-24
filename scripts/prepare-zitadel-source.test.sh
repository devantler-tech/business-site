#!/usr/bin/env bash
# Exercise source/output behavior against real immutable upstream objects.
set -euo pipefail
export LC_ALL=C
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
prepare="$root/scripts/prepare-zitadel-source.sh"
scratch="$(mktemp -d)"
scratch="$(cd "$scratch" && pwd -P)"
trap 'rm -rf -- "$scratch"' EXIT
pin=8afa5dd37a8e723efb9eace8d09fd5c1cccef694
# Assert a nonzero process result, retaining its output for failure diagnosis.
reject() {
  local status=0
  "$@" >"$scratch/result" 2>&1 || status=$?
  if [[ "$status" -eq 0 ]]; then
    echo 'Expected refusal, got success.' >&2
    exit 1
  fi
}

# Missing opt-in is inert even when no source or output exists.
default_status=0
bash "$prepare" >"$scratch/default" 2>"$scratch/default-error" || default_status=$?
if [[ "$default_status" -ne 0 ]]; then
  echo "Expected inert preparation command to succeed; got $default_status." >&2
  exit 1
fi
test ! -s "$scratch/default"
reject bash "$prepare" --prepare
reject bash "$prepare" --unknown

if [[ "$#" -eq 1 ]]; then
  git clone --shared --no-checkout -- "$1" "$scratch/upstream" >"$scratch/clone.log" 2>&1
else
  test "$#" -eq 0
  git clone --filter=blob:none --no-checkout --depth=1 --branch v4.20.0 \
    https://github.com/zitadel/zitadel.git "$scratch/upstream" >"$scratch/clone.log" 2>&1
fi
# Hydrate immutable objects through the independent archive, not another full copy.
# A sparse working tree also proves that preparation never reads checkout bytes.
git -C "$scratch/upstream" read-tree "$pin"
mkdir "$scratch/pristine"
git -C "$scratch/upstream" --no-replace-objects archive "$pin" | tar -x -C "$scratch/pristine"
mkdir -p "$scratch/upstream/console/src"
git -C "$scratch/upstream" show "$pin:console/src/styles.scss" >"$scratch/upstream/console/src/styles.scss"

# Dirty/untracked input bytes must neither leak into output nor be overwritten.
printf '\nLOCAL-ONLY-FICTIONAL-MARKER\n' >>"$scratch/upstream/console/src/styles.scss"
printf 'FICTIONAL-PRIVATE-INPUT\n' >"$scratch/upstream/local-only.txt"
shasum -a 256 "$scratch/upstream/console/src/styles.scss" >"$scratch/input-before"
bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/prepared source"
shasum -a 256 "$scratch/upstream/console/src/styles.scss" >"$scratch/input-after"
cmp "$scratch/input-before" "$scratch/input-after"
test -f "$scratch/upstream/local-only.txt"
test ! -e "$scratch/prepared source/source/local-only.txt"
test ! -d "$scratch/prepared source/source/.git"

# Exercise the real prepared machine-ID function through its native package.
# These tests add no production source or dependencies to the prepared output.
cp "$root/scripts/testdata/zitadel-metadata-profile/metadata_profile_test.go" \
  "$scratch/prepared source/source/internal/id/customer_metadata_profile_test.go"
(cd "$scratch/prepared source/source" && GOTOOLCHAIN=go1.26.10 GOMAXPROCS=2 GOFLAGS=-p=2 \
  go test -mod=readonly -count=1 ./internal/id -run '^TestCustomerMetadataProfile')
rm -- "$scratch/prepared source/source/internal/id/customer_metadata_profile_test.go"

for path in \
  console/src/assets/ailerons.otf \
  console/src/styles/ailerons.scss \
  internal/api/ui/login/static/resources/fonts/ailerons/ailerons.otf \
  internal/api/ui/login/static/resources/themes/scss/styles/typography/faces/ailerons_font_faces.scss; do
  test ! -e "$scratch/prepared source/source/$path"
  # Remove only the independently declared expected differences from the baseline.
  rm -- "$scratch/pristine/$path"
done
grep -Fvx "@import './styles/ailerons.scss';" "$scratch/pristine/console/src/styles.scss" >"$scratch/expected-console"
mv "$scratch/expected-console" "$scratch/pristine/console/src/styles.scss"
grep -Fvx "@import 'faces/ailerons_font_faces';" "$scratch/pristine/internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss" >"$scratch/expected-login"
mv "$scratch/expected-login" "$scratch/pristine/internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss"
# Pin the only newly declared changed file independently of the preparer.
metadata=internal/id/sonyflake.go
test "$(shasum -a 256 "$scratch/prepared source/source/$metadata" | awk '{print $1}')" = \
  74ad3d761a3101ec720429c664e55cf3083ad17903b762139717406114c62adc
cp "$scratch/prepared source/source/$metadata" "$scratch/pristine/$metadata"
diff -qr "$scratch/pristine" "$scratch/prepared source/source"
rm -rf -- "$scratch/pristine"
jq -e --arg pin "$pin" '
  .schemaVersion == 1 and .upstreamCommit == $pin and
  .imageAdmission == "unknown" and .licenseClosure == "unknown" and
  (.removedFiles | length) == 4 and
  .metadataProfile == "machine-id-no-jsonpath-v1" and
  .changedFiles == ["console/src/styles.scss","internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss","internal/id/sonyflake.go"]
' "$scratch/prepared source/source-preparation.json" >/dev/null

# Never overwrite a completed output or a pre-existing directory/symlink.
shasum -a 256 "$scratch/prepared source/source-preparation.json" >"$scratch/receipt-before"
reject bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/prepared source"
shasum -a 256 "$scratch/prepared source/source-preparation.json" >"$scratch/receipt-after"
cmp "$scratch/receipt-before" "$scratch/receipt-after"
rm -rf -- "$scratch/prepared source"
mkdir "$scratch/existing"
printf 'keep\n' >"$scratch/existing/sentinel"
reject bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/existing"
test "$(cat "$scratch/existing/sentinel")" = keep
ln -s "$scratch/existing" "$scratch/output-link"
reject bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/output-link"
reject bash "$prepare" --prepare --source "$scratch/absent" --output "$scratch/missing-source-output"
test ! -e "$scratch/missing-source-output"
git init -q "$scratch/other"
reject bash "$prepare" --prepare --source "$scratch/other" --output "$scratch/other-output"
test ! -e "$scratch/other-output"

# A dependency emitting a valid but incomplete archive before failure is not success.
real_git="$(command -v git)"
mkdir "$scratch/bin" "$scratch/partial"
printf 'plausible partial source\n' >"$scratch/partial/README.md"
tar -cf "$scratch/partial.tar" -C "$scratch/partial" README.md
cat >"$scratch/bin/git" <<'WRAPPER'
#!/usr/bin/env bash
if [[ " $* " == *" archive "* ]]; then cat "$SOURCE_TEST_PARTIAL"; exit "${SOURCE_TEST_ARCHIVE_STATUS:-42}"; fi
exec "$SOURCE_TEST_GIT" "$@"
WRAPPER
chmod 755 "$scratch/bin/git"
reject env PATH="$scratch/bin:$PATH" SOURCE_TEST_GIT="$real_git" SOURCE_TEST_PARTIAL="$scratch/partial.tar" \
  bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/partial-output"
test ! -e "$scratch/partial-output"
reject env PATH="$scratch/bin:$PATH" SOURCE_TEST_GIT="$real_git" SOURCE_TEST_PARTIAL="$scratch/partial.tar" SOURCE_TEST_ARCHIVE_STATUS=0 \
  bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/truncated-output"
test ! -e "$scratch/truncated-output"

# A plausible partial metadata transform is never a prepared source receipt,
# whether its producer reports failure or incorrectly reports success.
real_awk="$(command -v awk)"
mkdir "$scratch/metadata-bin"
cat >"$scratch/metadata-bin/awk" <<'WRAPPER'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ "$arg" == */internal/id/sonyflake.go ]]; then
    printf 'package id\n'
    exit "${SOURCE_TEST_METADATA_STATUS:-42}"
  fi
done
exec "$SOURCE_TEST_AWK" "$@"
WRAPPER
chmod 755 "$scratch/metadata-bin/awk"
for status in 42 0; do
  reject env PATH="$scratch/metadata-bin:$PATH" SOURCE_TEST_AWK="$real_awk" SOURCE_TEST_METADATA_STATUS="$status" \
    bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/partial-metadata-$status"
  test ! -e "$scratch/partial-metadata-$status"
done

# A failed receipt-input read cannot be hidden by a successful JSON writer.
real_shasum="$(command -v shasum)"
mkdir "$scratch/hash-bin"
cat >"$scratch/hash-bin/shasum" <<'WRAPPER'
#!/usr/bin/env bash
if [[ "${3:-}" == -c ]]; then exec "$SOURCE_TEST_SHASUM" "$@"; fi
exit "${SOURCE_TEST_HASH_STATUS:-42}"
WRAPPER
chmod 755 "$scratch/hash-bin/shasum"
reject env PATH="$scratch/hash-bin:$PATH" SOURCE_TEST_SHASUM="$real_shasum" \
  bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/failed-hash-output"
test ! -e "$scratch/failed-hash-output"
reject env PATH="$scratch/hash-bin:$PATH" SOURCE_TEST_SHASUM="$real_shasum" SOURCE_TEST_HASH_STATUS=0 \
  bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/empty-hash-output"
test ! -e "$scratch/empty-hash-output"

# Either failed final move must release only this invocation's reservation.
real_mv="$(command -v mv)"
mkdir "$scratch/move-bin"
cat >"$scratch/move-bin/mv" <<'WRAPPER'
#!/usr/bin/env bash
if [[ "${2:-}" == "$SOURCE_TEST_FAILED_MOVE" ]]; then exit 42; fi
exec "$SOURCE_TEST_MV" "$@"
WRAPPER
chmod 755 "$scratch/move-bin/mv"
leaked_output=0
for part in source source-preparation.json; do
  destination="$scratch/failed-move-$part"
  reject env PATH="$scratch/move-bin:$PATH" SOURCE_TEST_MV="$real_mv" SOURCE_TEST_FAILED_MOVE="$destination/$part" \
    bash "$prepare" --prepare --source "$scratch/upstream" --output "$destination"
  if [[ -e "$destination" ]]; then
    printf 'Failed final move left reserved output: %s\n' "$part" >&2
    leaked_output=1
  fi
done
test "$leaked_output" -eq 0

# A competing creator wins the reservation and keeps its unrelated output.
real_mkdir="$(command -v mkdir)"
mkdir "$scratch/mkdir-bin"
cat >"$scratch/mkdir-bin/mkdir" <<'WRAPPER'
#!/usr/bin/env bash
if [[ "$#" -eq 4 && "$1" == -m && "$2" == 700 && "$3" == -- && "$4" == "$SOURCE_TEST_OUTPUT" ]]; then
  "$SOURCE_TEST_MKDIR" -- "$4" || exit
  printf 'other creator\n' >"$4/sentinel"
  exit 1
fi
exec "$SOURCE_TEST_MKDIR" "$@"
WRAPPER
chmod 755 "$scratch/mkdir-bin/mkdir"
reject env PATH="$scratch/mkdir-bin:$PATH" SOURCE_TEST_MKDIR="$real_mkdir" SOURCE_TEST_OUTPUT="$scratch/competing-output" \
  bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/competing-output"
test "$(cat "$scratch/competing-output/sentinel")" = 'other creator'

# Output cannot modify the input checkout, even as a new untracked directory.
boundary_failure=0
if bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/upstream/nested-output" >"$scratch/nested-result" 2>&1; then
  echo 'Preparation accepted output inside its input checkout.' >&2
  boundary_failure=1
fi
if ! grep -qF 'output must be outside the input repository' "$scratch/nested-result"; then
  echo 'Nested output lacks an explicit input-preservation refusal.' >&2
  boundary_failure=1
fi

# Real missing Git blobs must not invoke even a local promisor transport.
git init -q "$scratch/blobless"
printf '%s\n' "$pin" | git -C "$scratch/upstream" pack-objects --stdout --revs --filter=blob:none |
  git -C "$scratch/blobless" index-pack --stdin --promisor >/dev/null
cat >"$scratch/promise-upload-pack" <<'WRAPPER'
#!/usr/bin/env bash
printf 'implicit fetch attempted\n' >"$SOURCE_TEST_FETCH_LOG"
exit 42
WRAPPER
chmod 755 "$scratch/promise-upload-pack"
git -C "$scratch/blobless" remote add origin "$scratch/upstream"
git -C "$scratch/blobless" config remote.origin.promisor true
git -C "$scratch/blobless" config remote.origin.partialclonefilter blob:none
git -C "$scratch/blobless" config remote.origin.uploadpack "$scratch/promise-upload-pack"
reject env GIT_NO_LAZY_FETCH=0 GIT_ALLOW_PROTOCOL=file SOURCE_TEST_FETCH_LOG="$scratch/implicit-fetch" \
  bash "$prepare" --prepare --source "$scratch/blobless" --output "$scratch/blobless-output"
test ! -e "$scratch/blobless-output"
if [[ -e "$scratch/implicit-fetch" ]]; then
  echo 'Preparation invoked an implicit Git fetch.' >&2
  boundary_failure=1
fi
test "$boundary_failure" -eq 0

# Committed regular-file permissions survive any caller umask, including execute masks.
review_failures=0
for mask in 0133 0077; do
  destination="$scratch/umask-$mask"
  if bash -c 'umask "$1"; exec bash "$2" --prepare --source "$3" --output "$4"' \
    -- "$mask" "$prepare" "$scratch/upstream" "$destination" >"$scratch/umask-$mask.log" 2>&1; then
    if [[ -n "$(find "$destination/source" -type f ! -perm 0644 ! -perm 0755 -print)" ]]; then
      echo "Preparation changed committed permissions under umask $mask." >&2
      review_failures=$((review_failures + 1))
    fi
    (cd "$destination/source" && find . -type f -perm 0755 | sed 's@^./@@' | sort) >"$scratch/actual-executables"
    git -C "$scratch/upstream" ls-tree -r "$pin" | awk '$1 == "100755" {print substr($0,index($0,"\t")+1)}' | sort >"$scratch/expected-executables"
    if ! cmp "$scratch/expected-executables" "$scratch/actual-executables"; then
      echo "Preparation changed executable paths under umask $mask." >&2
      review_failures=$((review_failures + 1))
    fi
  else
    echo "Preparation failed under umask $mask." >&2
    review_failures=$((review_failures + 1))
  fi
  rm -rf -- "$destination"
done

# Correct bytes with substituted permissions are not accepted as committed source.
real_tar="$(command -v tar)"
mode_path="$(git -C "$scratch/upstream" ls-tree -r "$pin" | awk '$1 == "100755" && !found {print substr($0,index($0,"\t")+1); found=1}')"
test -n "$mode_path"
mkdir "$scratch/mode-bin"
cat >"$scratch/mode-bin/tar" <<'WRAPPER'
#!/usr/bin/env bash
"$SOURCE_TEST_TAR" "$@" || exit
if [[ "$#" -eq 4 && "$1" == -xpf && "$3" == -C ]]; then
  chmod "$SOURCE_TEST_BAD_MODE" "$4/$SOURCE_TEST_MODE_PATH"
fi
WRAPPER
chmod 755 "$scratch/mode-bin/tar"
for mode in 0777 0644; do
  destination="$scratch/substituted-mode-$mode"
  reject env PATH="$scratch/mode-bin:$PATH" SOURCE_TEST_TAR="$real_tar" SOURCE_TEST_BAD_MODE="$mode" SOURCE_TEST_MODE_PATH="$mode_path" \
    bash "$prepare" --prepare --source "$scratch/upstream" --output "$destination"
  test ! -e "$destination"
done

# A different enclosing Git worktree must not select the source bytes being hashed.
for format in sha1 sha256; do
  git init -q --object-format="$format" "$scratch/enclosing-$format"
  if ! bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/enclosing-$format/prepared" >"$scratch/enclosing-$format.log" 2>&1; then
    echo "Preparation failed inside a different $format Git worktree." >&2
    review_failures=$((review_failures + 1))
  fi
  rm -rf -- "$scratch/enclosing-$format/prepared"
done

# Catchable termination immediately after successful mkdir must not strand a reservation.
mkdir "$scratch/signal-bin"
cat >"$scratch/signal-bin/mkdir" <<'WRAPPER'
#!/usr/bin/env bash
if [[ "$#" -eq 4 && "$1" == -m && "$2" == 700 && "$3" == -- && "$4" == "$SOURCE_TEST_OUTPUT" ]]; then
  "$SOURCE_TEST_MKDIR" "$@" || exit
  kill -s "$SOURCE_TEST_SIGNAL" "$PPID"
  exit 0
fi
exec "$SOURCE_TEST_MKDIR" "$@"
WRAPPER
chmod 755 "$scratch/signal-bin/mkdir"
for signal in HUP INT TERM; do
  destination="$scratch/interrupted-$signal"
  signal_status=0
  env PATH="$scratch/signal-bin:$PATH" SOURCE_TEST_MKDIR="$real_mkdir" SOURCE_TEST_OUTPUT="$destination" SOURCE_TEST_SIGNAL="$signal" \
    bash "$prepare" --prepare --source "$scratch/upstream" --output "$destination" >"$scratch/signal-$signal.log" 2>&1 || signal_status=$?
  if [[ "$signal_status" -eq 0 || -e "$destination" ]]; then
    echo "Signal $signal did not refuse and clean up the incomplete reservation." >&2
    review_failures=$((review_failures + 1))
  fi
  rm -rf -- "$destination"
done
test "$review_failures" -eq 0
printf '%s\n' 'Frozen ZITADEL source preparation and refusal controls passed.'
