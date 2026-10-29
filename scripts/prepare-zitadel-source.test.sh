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
bash "$root/scripts/zitadel-provider-icons.test.sh" "$scratch/upstream"
mkdir "$scratch/pristine"
git -C "$scratch/upstream" --no-replace-objects archive "$pin" | tar -x -C "$scratch/pristine"
mkdir -p "$scratch/upstream/console/src"
git -C "$scratch/upstream" show "$pin:console/src/styles.scss" >"$scratch/upstream/console/src/styles.scss"
for member in go.mod go.sum; do
  git -C "$scratch/upstream" show "$pin:$member" >"$scratch/upstream/$member"
  printf '\nFICTIONAL-PRIVATE-MODULE-MARKER\n' >>"$scratch/upstream/$member"
done

# Dirty/untracked input bytes must neither leak into output nor be overwritten.
printf '\nLOCAL-ONLY-FICTIONAL-MARKER\n' >>"$scratch/upstream/console/src/styles.scss"
printf 'FICTIONAL-PRIVATE-INPUT\n' >"$scratch/upstream/local-only.txt"
shasum -a 256 "$scratch/upstream/console/src/styles.scss" >"$scratch/input-before"
shasum -a 256 "$scratch/upstream/go.mod" "$scratch/upstream/go.sum" >"$scratch/modules-before"
bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/prepared source"
shasum -a 256 "$scratch/upstream/console/src/styles.scss" >"$scratch/input-after"
cmp "$scratch/input-before" "$scratch/input-after"
shasum -a 256 "$scratch/upstream/go.mod" "$scratch/upstream/go.sum" >"$scratch/modules-after"
cmp "$scratch/modules-before" "$scratch/modules-after"
# Independently collected with native Go 1.26.10 for the three selected patches,
# without a toolchain change or unrelated selected dependency changes.
test "$(shasum -a 256 "$scratch/prepared source/source/go.mod" | awk '{print $1}')" = \
  d93f15da9568e8845430997cf6a1f5831decb2c3a526d71ffa8a0d869fe65b50
test "$(shasum -a 256 "$scratch/prepared source/source/go.sum" | awk '{print $1}')" = \
  19e4a6df1f26dd45007ee996eb654923e561dd9aa818315abed3a40577d2f8ba
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

# Compile the unchanged prepared interceptor with the real patched CSRF package.
# Only unrelated instance/cookie-name policies are doubled in this focused probe.
mkdir "$scratch/csrf-probe"
cp "$root/scripts/testdata/zitadel-metadata-profile/login_csrf_test.go" "$scratch/csrf-probe/"
{
  printf 'package csrfprobe\nimport ("net/http"; "strings"; "github.com/gorilla/csrf")\n'
  awk '/^func createCSRFInterceptor\(/ { active=1 } active {print} active && /^}/ {exit}' \
    "$scratch/prepared source/source/internal/api/ui/login/login.go"
} >"$scratch/csrf-probe/interceptor.go"
printf 'module example.invalid/csrfprobe\n\ngo 1.26.0\n\nrequire github.com/gorilla/csrf v1.7.3\n' >"$scratch/csrf-probe/go.mod"
(cd "$scratch/csrf-probe" && GOTOOLCHAIN=go1.26.10 go mod tidy && GOTOOLCHAIN=go1.26.10 go test -race -count=1 .)

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
test "$(shasum -a 256 "$scratch/prepared source/source/internal/api/ui/login/login.go" | awk '{print $1}')" = \
  28da3dd3fd55002ea3239495cba504be6a355e9eed74133327aa564e634e812e
cp "$scratch/prepared source/source/internal/api/ui/login/login.go" "$scratch/pristine/internal/api/ui/login/login.go"
cp "$scratch/prepared source/source/go.mod" "$scratch/pristine/go.mod"
cp "$scratch/prepared source/source/go.sum" "$scratch/pristine/go.sum"
# Pin all eighteen independently observed neutral consumers; retain an exact
# whole-tree comparison, not a projection of selected expected fields.
cat >"$scratch/neutral-consumers.sha256" <<'PROVIDERS'
5c489734a039df3535d00e7761200621bfca99e25d63955b70a1d1e712f76544  console/src/app/modules/idp-table/idp-table.component.html
015e783ee150d21280d7748ab897c1b709fb5e208777370059716f7b10766188  console/src/app/modules/policies/idp-settings/idp-settings.component.html
bcd033476803838515de5f25057ac4df9749f26049bf4a0c2926acb83ea01b94  console/src/app/modules/providers/provider-apple/provider-apple.component.html
bb307b70f936d780f8584f01c4691efd042d43d95041df294f2af1f48ae94459  console/src/app/modules/providers/provider-azure-ad/provider-azure-ad.component.html
380b3dca85ef437d4446987a5e22020c1a7e1bec1bc874ff7942dd0e204a1efd  console/src/app/modules/providers/provider-github-es/provider-github-es.component.html
2eca45dc2386dc08bdd6af010b3782f3e3f9531260db70f22117aa115e976205  console/src/app/modules/providers/provider-github/provider-github.component.html
99871b68ae002cd48b82de6ef6b7beeaa3f3f5f2dc9b0df66a398149e0929e9f  console/src/app/modules/providers/provider-gitlab-self-hosted/provider-gitlab-self-hosted.component.html
341c4a94d2817aed886fe8268c31f890188fb8468e25d09e6e987a511305449b  console/src/app/modules/providers/provider-gitlab/provider-gitlab.component.html
8fae34c70584f876ab9dad8608830b5f95d430310d1b33bca04e847ea492c7e8  console/src/app/modules/providers/provider-google/provider-google.component.html
eae8329b095b5987d8340a386957fb67d5ec58488e97f221c03cbf8592d71266  console/src/app/modules/providers/provider-oauth/provider-oauth.component.html
0b5410b7aa9b905e0f4c074e85eda06905d51cef28cf3f3ca49c6b2146a3acff  console/src/app/modules/providers/provider-saml-sp/provider-saml-sp.component.html
d749528ee622c0d376afc918d4d205bf9d42b6c33243301e721a997be631d4bf  internal/api/ui/login/static/templates/login.html
160874790c22abfd44bc977b2d978a8fbae73dc0e4adfa52917b4b3854ec123b  internal/api/ui/login/static/templates/register_option.html
3109087ad9a64409e4e30d6eab7ea7b8f48b3c7194042d0171d1ee9f9926efb8  console/src/app/modules/idp-table/idp-table.component.scss
8e5bf3f536e63f9e3fab90a2be0c46e7239190e2cc7ea558a0fcd5f90e0b6217  console/src/app/modules/policies/idp-settings/idp-settings.component.scss
fc44668ff48fa9442f5b592abeac0402da17bd177b5809b8c5e1ea8675b58413  console/src/app/modules/providers/providers.scss
a25666a0c03c21542c84ab72d228c5ce91c6e7b00a712289d44a4f720f613732  internal/api/ui/login/static/resources/themes/scss/styles/identity_provider/identity_provider_base.scss
a15f2692a597e8a8c3848d62073db1d48dce82aea66d6cabfda1d2019825a25c  internal/api/ui/login/static/resources/themes/scss/styles/vars.scss
7adb2e384cb0af6db510f3c1b07a9b91729ea922a004e4b6fed6fbd98352b1fc  console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.html
db4f7c1746b2f3f19a1de401ac5cf658439e59d3f045106ff4aa05dc4eb8a8eb  console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.scss
f340db31d1e1d4db10e1646eb218b2e46fe7e6568e9801b26494f94602c168cd  console/src/app/modules/smtp-provider/known-smtp-providers-settings.ts
3c6be2a65c023c2aa1fa0ab86dc54530e0db7d3d79880099be3772f062db579f  console/src/app/modules/smtp-provider/smtp-provider.component.html
194fd266ef3301dcb35b6ead7d92d5ac9860685d52e1312f693dfa644300eb24  console/src/app/modules/smtp-provider/smtp-provider.scss
235dc03feb62db25a22a0e3c2741e916ed40b659677febd691b362fed21bc2ab  console/src/app/app.component.ts
3c97248b11a743f010ed71a85e18d019c0b71fb528674e664f4090c00e79ca80  console/src/app/modules/providers/provider-oidc/provider-oidc.component.html
5d95c62b1cbc5a3195d01593233e485e71d1a6f15a856bc9f8515593ccab85c9  console/src/app/modules/providers/provider-jwt/provider-jwt.component.html
PROVIDERS
(cd "$scratch/prepared source/source" && shasum -a 256 -c "$scratch/neutral-consumers.sha256") >/dev/null
while read -r _hash path; do
  cp "$scratch/prepared source/source/$path" "$scratch/pristine/$path"
done <"$scratch/neutral-consumers.sha256"
for path in \
  console/src/assets/images/idp/apple-dark.svg\
  console/src/assets/images/idp/apple.svg\
  console/src/assets/images/idp/github-dark.svg\
  console/src/assets/images/idp/github.svg\
  console/src/assets/images/idp/gitlab.svg\
  console/src/assets/images/idp/google.png\
  console/src/assets/images/idp/ms.svg\
  console/src/assets/images/idp/oauth.svg\
  console/src/assets/images/idp/saml-icon.svg\
  internal/api/ui/login/static/resources/images/idp/apple-dark.svg\
  internal/api/ui/login/static/resources/images/idp/apple.svg\
  internal/api/ui/login/static/resources/images/idp/github-white.png\
  internal/api/ui/login/static/resources/images/idp/github.png\
  internal/api/ui/login/static/resources/images/idp/gitlab.png\
  internal/api/ui/login/static/resources/images/idp/google.png\
  internal/api/ui/login/static/resources/images/idp/ms.svg\
  console/src/assets/images/smtp/aws-ses.svg\
  console/src/assets/images/smtp/brevo.svg\
  console/src/assets/images/smtp/google.png\
  console/src/assets/images/smtp/mailchimp.svg\
  console/src/assets/images/smtp/mailgun.svg\
  console/src/assets/images/smtp/mailjet.svg\
  console/src/assets/images/smtp/outlook.svg\
  console/src/assets/images/smtp/postmark.png\
  console/src/assets/images/smtp/sendgrid.png\
  console/src/assets/mdi/openid.svg\
  console/src/assets/mdi/jwt.svg; do
  test ! -e "$scratch/prepared source/source/$path"
  rm -- "$scratch/pristine/$path"
done
diff -qr "$scratch/pristine" "$scratch/prepared source/source"
rm -rf -- "$scratch/pristine"
jq -e --arg pin "$pin" '
  .schemaVersion == 1 and .upstreamCommit == $pin and
  .imageAdmission == "unknown" and .licenseClosure == "unknown" and
  (.removedFiles | length) == 31 and .providerProfile == "native-neutral-provider-icons-v1" and
  .smtpProfile == "native-neutral-smtp-icons-v1" and .mdiProfile == "native-neutral-mdi-icons-v1" and
  .metadataProfile == "machine-id-no-jsonpath-v1" and
  .dependencyProfile == "go-jose-4.1.6-x-net-0.62.0-csrf-1.7.3-v1" and
  .changedFiles == ["console/src/styles.scss","internal/api/ui/login/static/resources/themes/scss/styles/typography/typography.scss","internal/id/sonyflake.go","internal/api/ui/login/login.go","go.mod","go.sum","console/src/app/modules/idp-table/idp-table.component.html","console/src/app/modules/policies/idp-settings/idp-settings.component.html","console/src/app/modules/providers/provider-apple/provider-apple.component.html","console/src/app/modules/providers/provider-azure-ad/provider-azure-ad.component.html","console/src/app/modules/providers/provider-github-es/provider-github-es.component.html","console/src/app/modules/providers/provider-github/provider-github.component.html","console/src/app/modules/providers/provider-gitlab-self-hosted/provider-gitlab-self-hosted.component.html","console/src/app/modules/providers/provider-gitlab/provider-gitlab.component.html","console/src/app/modules/providers/provider-google/provider-google.component.html","console/src/app/modules/providers/provider-oauth/provider-oauth.component.html","console/src/app/modules/providers/provider-saml-sp/provider-saml-sp.component.html","internal/api/ui/login/static/templates/login.html","internal/api/ui/login/static/templates/register_option.html","console/src/app/modules/idp-table/idp-table.component.scss","console/src/app/modules/policies/idp-settings/idp-settings.component.scss","console/src/app/modules/providers/providers.scss","internal/api/ui/login/static/resources/themes/scss/styles/identity_provider/identity_provider_base.scss","internal/api/ui/login/static/resources/themes/scss/styles/vars.scss","console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.html","console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.scss","console/src/app/modules/smtp-provider/known-smtp-providers-settings.ts","console/src/app/modules/smtp-provider/smtp-provider.component.html","console/src/app/modules/smtp-provider/smtp-provider.scss","console/src/app/app.component.ts","console/src/app/modules/providers/provider-oidc/provider-oidc.component.html","console/src/app/modules/providers/provider-jwt/provider-jwt.component.html"]
' "$scratch/prepared source/source-preparation.json" >/dev/null

# Exercise the actual native pre-install capture boundary, not a substituted
# fixture policy. Both complete-source and preparation-receipt pins must agree.
mkdir "$scratch/native-capture"
cp -R "$scratch/prepared source" "$scratch/native-capture/prepared"
(
  cd "$scratch/native-capture/prepared/source"
  shasum -a 256 go.mod go.sum package.json pnpm-lock.yaml pnpm-workspace.yaml console/package.json proto/buf.lock >"$scratch/native-capture/inputs.sha256"
  cp apps/api/project.json "$scratch/native-capture/api-project.original.json"
  jq '.targets["generate-install"].options.parallel = false' apps/api/project.json >"$scratch/native-capture/api-project.serial.json"
  cp "$scratch/native-capture/api-project.serial.json" apps/api/project.json
)
go run "$root/scripts/zitadel-source-bundle.go" --capture-build "$scratch/native-capture" "$root" "$scratch/native-snapshot" >"$scratch/native-capture.json"
jq -e '.preparedProfileSha256=="dc09cf8494d8b7f44a3ee78957738994fce97f527f9b5660563dea9fc63b5165" and .files==9572 and .bytes==261612982 and .records==58' "$scratch/native-capture.json" >/dev/null
native_seal="$(jq -r '.manifestSha256' "$scratch/native-capture.json")"
go run "$root/scripts/zitadel-source-bundle.go" --capture-notice-originals "$root" "$scratch/native-snapshot" "$native_seal" "$scratch/native-notice-originals" >"$scratch/native-notices.json"
jq -e '.files==43 and .originalInputsReadbackPassed==true' "$scratch/native-notices.json" >/dev/null
cp "$scratch/native-capture/prepared/source-preparation.json" "$scratch/native-preparation-original.json"
jq '.mdiProfile="changed"' "$scratch/native-preparation-original.json" >"$scratch/native-capture/prepared/source-preparation.json"
reject go run "$root/scripts/zitadel-source-bundle.go" --capture-build "$scratch/native-capture" "$root" "$scratch/changed-native-snapshot"
test ! -e "$scratch/changed-native-snapshot"
cp "$scratch/native-preparation-original.json" "$scratch/native-capture/prepared/source-preparation.json"
rm -rf -- "$scratch/native-capture" "$scratch/native-snapshot"

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
  if [[ "$arg" == */internal/api/ui/login/login.go && -n "${SOURCE_TEST_LOGIN_STATUS:-}" ]]; then
    printf 'observed\n' >"$SOURCE_TEST_LOGIN_OBSERVED"
    printf 'package login\n'
    exit "$SOURCE_TEST_LOGIN_STATUS"
  fi
  if [[ "$arg" == */internal/id/sonyflake.go && -n "${SOURCE_TEST_METADATA_STATUS:-}" ]]; then
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
for status in 42 0; do
  reject env PATH="$scratch/metadata-bin:$PATH" SOURCE_TEST_AWK="$real_awk" SOURCE_TEST_LOGIN_STATUS="$status" \
    SOURCE_TEST_LOGIN_OBSERVED="$scratch/login-observed-$status" \
    bash "$prepare" --prepare --source "$scratch/upstream" --output "$scratch/partial-login-$status"
  test "$(cat "$scratch/login-observed-$status")" = observed
  test ! -e "$scratch/partial-login-$status"
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
    for removed in console/src/assets/images/idp/apple-dark.svg console/src/assets/images/idp/apple.svg \
      internal/api/ui/login/static/resources/images/idp/apple-dark.svg internal/api/ui/login/static/resources/images/idp/apple.svg; do
      grep -Fxq "$removed" "$scratch/expected-executables"
      grep -Fxv "$removed" "$scratch/expected-executables" >"$scratch/remaining-executables"
      mv "$scratch/remaining-executables" "$scratch/expected-executables"
    done
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
