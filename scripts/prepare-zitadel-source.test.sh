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
  git clone --filter=blob:none --depth=1 --branch v4.20.0 \
    https://github.com/zitadel/zitadel.git "$scratch/upstream" >"$scratch/clone.log" 2>&1
fi
git -C "$scratch/upstream" checkout --detach "$pin" >"$scratch/checkout.log" 2>&1
mkdir "$scratch/pristine"
git -C "$scratch/upstream" --no-replace-objects archive "$pin" | tar -x -C "$scratch/pristine"

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
diff -qr "$scratch/pristine" "$scratch/prepared source/source"
jq -e --arg pin "$pin" '
  .schemaVersion == 1 and .upstreamCommit == $pin and
  .imageAdmission == "unknown" and .licenseClosure == "unknown" and
  (.removedFiles | length) == 4 and (.changedFiles | length) == 2
' "$scratch/prepared source/source-preparation.json" >/dev/null

# Never overwrite a completed output or a pre-existing directory/symlink.
shasum -a 256 "$scratch/prepared source/source-preparation.json" >"$scratch/receipt-before"
reject bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/prepared source"
shasum -a 256 "$scratch/prepared source/source-preparation.json" >"$scratch/receipt-after"
cmp "$scratch/receipt-before" "$scratch/receipt-after"
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
printf '%s\n' 'Frozen ZITADEL source preparation and refusal controls passed.'
