#!/usr/bin/env bash
# Actual frozen provider templates: neutral decoration preserves native bindings.
set -euo pipefail
export LC_ALL=C
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
scratch="$(cd "$scratch" && pwd -P)"
trap 'rm -rf -- "$scratch"' EXIT
pin=8afa5dd37a8e723efb9eace8d09fd5c1cccef694
[[ "$#" -eq 1 ]] || { echo 'Expected immutable upstream repository.' >&2; exit 1; }
upstream="$1"
[[ "$(git -C "$upstream" rev-parse "$pin^{commit}")" = "$pin" ]]
cat >"$scratch/consumers" <<'PATHS'
console/src/app/modules/idp-table/idp-table.component.html
console/src/app/modules/policies/idp-settings/idp-settings.component.html
console/src/app/modules/providers/provider-apple/provider-apple.component.html
console/src/app/modules/providers/provider-azure-ad/provider-azure-ad.component.html
console/src/app/modules/providers/provider-github-es/provider-github-es.component.html
console/src/app/modules/providers/provider-github/provider-github.component.html
console/src/app/modules/providers/provider-gitlab-self-hosted/provider-gitlab-self-hosted.component.html
console/src/app/modules/providers/provider-gitlab/provider-gitlab.component.html
console/src/app/modules/providers/provider-google/provider-google.component.html
console/src/app/modules/providers/provider-oauth/provider-oauth.component.html
console/src/app/modules/providers/provider-saml-sp/provider-saml-sp.component.html
console/src/app/modules/idp-table/idp-table.component.scss
console/src/app/modules/policies/idp-settings/idp-settings.component.scss
console/src/app/modules/providers/providers.scss
internal/api/ui/login/static/templates/login.html
internal/api/ui/login/static/templates/register_option.html
internal/api/ui/login/static/resources/themes/scss/styles/identity_provider/identity_provider_base.scss
internal/api/ui/login/static/resources/themes/scss/styles/vars.scss
PATHS
cp "$scratch/consumers" "$scratch/inputs"
git -C "$upstream" ls-tree -r --name-only "$pin" -- console/src/assets/images/idp internal/api/ui/login/static/resources/images/idp >>"$scratch/inputs"
[[ "$(wc -l <"$scratch/inputs" | tr -d ' ')" -eq 34 ]]
mkdir "$scratch/original"
while IFS= read -r path; do
 mkdir -p "$scratch/original/$(dirname "$path")"
 git -C "$upstream" show "$pin:$path" >"$scratch/original/$path"
 mode=$(git -C "$upstream" ls-tree "$pin" -- "$path" | awk '{print $1}')
 chmod "${mode#100}" "$scratch/original/$path"
done <"$scratch/inputs"
(cd "$root/scripts" && GOTOOLCHAIN=local go build -o "$scratch/provider-icons" zitadel-login-icons.go zitadel-text-fonts.go zitadel-copied-assets.go)
cp -Rp "$scratch/original" "$scratch/positive"
printf 'unrelated source must survive\n' >"$scratch/positive/unrelated.txt"
"$scratch/provider-icons" --neutral-provider-icons --prepare --source "$scratch/positive" >"$scratch/receipt.json"
jq -e '.schemaVersion==1 and .profile=="native-neutral-provider-icons-v1" and .changedFiles==18 and .removedFiles==16 and .consoleSlots==27 and .loginSlots==2 and .imageAdmission=="unknown" and .licenseClosure=="unknown"' "$scratch/receipt.json" >/dev/null
cmp "$scratch/positive/unrelated.txt" <(printf 'unrelated source must survive\n')
# Only the decorative provider image nodes may change in Console HTML.
while IFS= read -r path; do
 case "$path" in
  console/*.html)
   sed '/<img .*src="\.\/assets\/images\/idp\//d' "$scratch/original/$path" >"$scratch/original-core"
   sed '/<i class="idp-logo neutral-provider-icon las la-arrow-right" aria-hidden="true"><\/i>/d' "$scratch/positive/$path" >"$scratch/positive-core"
   cmp "$scratch/original-core" "$scratch/positive-core"
   ;;
  internal/*.html)
   sed 's/<span class="logo"><\/span>/<span class="logo lgn-icon-arrow-right-solid" aria-hidden="true"><\/span>/' "$scratch/original/$path" >"$scratch/expected"
   cmp "$scratch/expected" "$scratch/positive/$path"
   ;;
 esac
done <"$scratch/consumers"
[[ "$(find "$scratch/positive/console" -name '*.html' -exec grep -h 'neutral-provider-icon' {} + | wc -l | tr -d ' ')" -eq 27 ]]
while IFS= read -r path; do
 case "$path" in */images/idp/*) [[ ! -e "$scratch/positive/$path" && ! -L "$scratch/positive/$path" ]] ;; esac
done <"$scratch/inputs"
grep -Fq "externalIDPAuthURL \$reqid \$provider.IDPConfigID" "$scratch/positive/internal/api/ui/login/static/templates/login.html"
grep -Fq "externalIDPRegisterURL \$reqid \$provider.IDPConfigID" "$scratch/positive/internal/api/ui/login/static/templates/register_option.html"
# Each genuine original independently refuses missing, substituted, linked or unsafe-mode input.
controls=0
while IFS= read -r path; do
 for change in absent changed linked mode; do
  cp -Rp "$scratch/original" "$scratch/adverse"
  case "$change" in
   absent) rm "$scratch/adverse/$path" ;;
   changed) printf '\nchanged\n' >>"$scratch/adverse/$path" ;;
   linked) rm "$scratch/adverse/$path"; ln -s "$scratch/original/$path" "$scratch/adverse/$path" ;;
   mode) chmod 0666 "$scratch/adverse/$path" ;;
  esac
  if "$scratch/provider-icons" --neutral-provider-icons --prepare --source "$scratch/adverse" >"$scratch/adverse-result" 2>&1; then
   printf 'Admitted %s original: %s\n' "$change" "$path" >&2; exit 1
  fi
  rm -rf "$scratch/adverse"
  controls=$((controls+1))
 done
done <"$scratch/inputs"
printf 'Provider icons: real 18-template behavior and %s original-input refusals passed.\n' "$controls"

# Original SMTP artwork stays in disposable input fixtures, never in this repo.
cat >"$scratch/smtp-inputs" <<'PATHS'
console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.html
console/src/app/modules/policies/notification-smtp-provider/notification-smtp-provider.component.scss
console/src/app/modules/smtp-provider/known-smtp-providers-settings.ts
console/src/app/modules/smtp-provider/smtp-provider.component.html
console/src/app/modules/smtp-provider/smtp-provider.scss
PATHS
git -C "$upstream" ls-tree -r --name-only "$pin" -- console/src/assets/images/smtp >>"$scratch/smtp-inputs"
[[ "$(wc -l <"$scratch/smtp-inputs" | tr -d ' ')" -eq 14 ]]
mkdir "$scratch/smtp-original"
while IFS= read -r path; do
 mkdir -p "$scratch/smtp-original/$(dirname "$path")"
 git -C "$upstream" show "$pin:$path" >"$scratch/smtp-original/$path"
 mode=$(git -C "$upstream" ls-tree "$pin" -- "$path" | awk '{print $1}')
 chmod "${mode#100}" "$scratch/smtp-original/$path"
done <"$scratch/smtp-inputs"
cp -Rp "$scratch/smtp-original" "$scratch/smtp-positive"
"$scratch/provider-icons" --neutral-smtp-icons --prepare --source "$scratch/smtp-positive" >"$scratch/smtp-receipt.json"
jq -e '.schemaVersion==1 and .profile=="native-neutral-smtp-icons-v1" and .changedFiles==5 and .removedFiles==9 and .consoleSlots==2 and .settingsValues==9 and .imageAdmission=="unknown" and .licenseClosure=="unknown"' "$scratch/smtp-receipt.json" >/dev/null

# Removing decoration must leave every native setting, binding and form intact.
while IFS= read -r path; do
 case "$path" in
  */images/smtp/*) [[ ! -e "$scratch/smtp-positive/$path" && ! -L "$scratch/smtp-positive/$path" ]] ;;
  *.html)
   sed '/<img class="smtp-logo" /d' "$scratch/smtp-original/$path" >"$scratch/smtp-original-core"
   sed '/<i class="smtp-logo neutral-smtp-icon las la-envelope" aria-hidden="true"><\/i>/d' "$scratch/smtp-positive/$path" >"$scratch/smtp-positive-core"
   cmp "$scratch/smtp-original-core" "$scratch/smtp-positive-core"
   ;;
  *.ts)
   sed '/^  image:/d' "$scratch/smtp-original/$path" >"$scratch/smtp-original-core"
   cmp "$scratch/smtp-original-core" "$scratch/smtp-positive/$path"
   ;;
 esac
done <"$scratch/smtp-inputs"
smtp_controls=0
while IFS= read -r path; do
 for change in absent changed linked mode; do
  cp -Rp "$scratch/smtp-original" "$scratch/smtp-adverse"
  case "$change" in
   absent) rm "$scratch/smtp-adverse/$path" ;;
   changed) printf '\nchanged\n' >>"$scratch/smtp-adverse/$path" ;;
   linked) rm "$scratch/smtp-adverse/$path"; ln -s "$scratch/smtp-original/$path" "$scratch/smtp-adverse/$path" ;;
   mode) chmod 0666 "$scratch/smtp-adverse/$path" ;;
  esac
  if "$scratch/provider-icons" --neutral-smtp-icons --prepare --source "$scratch/smtp-adverse" >"$scratch/smtp-adverse-result" 2>&1; then
   printf 'Admitted SMTP %s original: %s\n' "$change" "$path" >&2; exit 1
  fi
  rm -rf "$scratch/smtp-adverse"
  smtp_controls=$((smtp_controls+1))
 done
done <"$scratch/smtp-inputs"
printf 'SMTP icons: real five-consumer behavior and %s original-input refusals passed.\n' "$smtp_controls"

# The MDI profile follows the provider profile but preserves its original controls.
cat >"$scratch/mdi-consumers" <<'PATHS'
console/src/app/app.component.ts
console/src/app/modules/idp-table/idp-table.component.html
console/src/app/modules/policies/idp-settings/idp-settings.component.html
console/src/app/modules/providers/provider-oidc/provider-oidc.component.html
console/src/app/modules/providers/provider-jwt/provider-jwt.component.html
PATHS
cp "$scratch/mdi-consumers" "$scratch/mdi-inputs"
git -C "$upstream" ls-tree -r --name-only "$pin" -- console/src/assets/mdi >>"$scratch/mdi-inputs"
[[ "$(wc -l <"$scratch/mdi-inputs" | tr -d ' ')" -eq 32 ]]
cp -Rp "$scratch/positive" "$scratch/mdi-original"
while IFS= read -r path; do
 [[ -f "$scratch/mdi-original/$path" ]] && continue
 mkdir -p "$scratch/mdi-original/$(dirname "$path")"
 git -C "$upstream" show "$pin:$path" >"$scratch/mdi-original/$path"
 chmod 0644 "$scratch/mdi-original/$path"
done <"$scratch/mdi-inputs"
cp -Rp "$scratch/mdi-original" "$scratch/mdi-positive"
"$scratch/provider-icons" --neutral-mdi-icons --prepare --source "$scratch/mdi-positive" >"$scratch/mdi-receipt.json"
jq -e '.profile=="native-neutral-mdi-icons-v1" and .changedFiles==5 and .removedFiles==2 and .retainedFiles==25 and .consoleSlots==6 and .imageAdmission=="unknown" and .licenseClosure=="unknown"' "$scratch/mdi-receipt.json" >/dev/null
while IFS= read -r path; do
 case "$path" in
  */mdi/openid.svg|*/mdi/jwt.svg) [[ ! -e "$scratch/mdi-positive/$path" && ! -L "$scratch/mdi-positive/$path" ]] ;;
  */mdi/*) cmp "$scratch/mdi-original/$path" "$scratch/mdi-positive/$path" ;;
  *.html)
   sed -E '/<mat-icon .*svgIcon="mdi_(openid|jwt)"/d; s/ svgIcon="mdi_jwt"//g' "$scratch/mdi-original/$path" >"$scratch/mdi-original-core"
   sed '/<i .*neutral-mdi-icon/d' "$scratch/mdi-positive/$path" >"$scratch/mdi-positive-core"
   cmp "$scratch/mdi-original-core" "$scratch/mdi-positive-core"
   ;;
  *.ts)
   sed -E "/addSvgIcon\('mdi_(openid|jwt)'/d" "$scratch/mdi-original/$path" >"$scratch/mdi-original-core"
   cmp "$scratch/mdi-original-core" "$scratch/mdi-positive/$path"
   ;;
 esac
done <"$scratch/mdi-inputs"
mdi_controls=0
while IFS= read -r path; do
 for change in absent changed linked mode; do
  cp -Rp "$scratch/mdi-original" "$scratch/mdi-adverse"
  case "$change" in
   absent) rm "$scratch/mdi-adverse/$path" ;;
   changed) printf '\nchanged\n' >>"$scratch/mdi-adverse/$path" ;;
   linked) rm "$scratch/mdi-adverse/$path"; ln -s "$scratch/mdi-original/$path" "$scratch/mdi-adverse/$path" ;;
   mode) chmod 0666 "$scratch/mdi-adverse/$path" ;;
  esac
  if "$scratch/provider-icons" --neutral-mdi-icons --prepare --source "$scratch/mdi-adverse" >"$scratch/mdi-adverse-result" 2>&1; then
   printf 'Admitted MDI %s original: %s\n' "$change" "$path" >&2; exit 1
  fi
  rm -rf "$scratch/mdi-adverse"
  mdi_controls=$((mdi_controls+1))
 done
done <"$scratch/mdi-inputs"
cp -Rp "$scratch/mdi-original" "$scratch/mdi-extra"
printf '<svg/>\n' >"$scratch/mdi-extra/console/src/assets/mdi/extra.svg"
if "$scratch/provider-icons" --neutral-mdi-icons --prepare --source "$scratch/mdi-extra" >/dev/null 2>&1; then
 echo 'Admitted an undeclared MDI source.' >&2; exit 1
fi
printf 'MDI icons: five native consumers, 25 unchanged graphics and %s original-input refusals passed.\n' "$mdi_controls"
