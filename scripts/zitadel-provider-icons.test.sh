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
