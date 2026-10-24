#!/usr/bin/env bash
# Orchestration controls use fake processes; the separate CI job builds real upstream.
set -euo pipefail
export LC_ALL=C
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/recipe/scripts" "$scratch/bin" "$scratch/upstream"
mkdir "$scratch/go-distribution"
cp "$root/scripts/testdata/go1.26.10/LICENSE" "$root/scripts/testdata/go1.26.10/PATENTS" "$scratch/go-distribution/"
printf 'go1.26.10\n' >"$scratch/go-distribution/VERSION"
cp "$root/scripts/build-zitadel-linux.sh" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-login-icons.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-console-notices.go" "$scratch/recipe/scripts/"
mkdir "$scratch/recipe/third_party"
cp -R "$root/third_party/zitadel-login-icons" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-console-notices" "$scratch/recipe/third_party/"
build="$scratch/recipe/scripts/build-zitadel-linux.sh"
# These are process doubles, not alternative production build entrypoints.
cat >"$scratch/recipe/scripts/prepare-zitadel-source.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
out="$5"
mkdir -p "$out/source/console/src" "$out/source/apps/api" "$out/source/proto" "$out/source/internal/api/ui/login/static/resources/fonts/ailerons"
for f in go.mod go.sum package.json pnpm-lock.yaml pnpm-workspace.yaml console/package.json proto/buf.lock; do
  printf 'frozen input\n' >"$out/source/$f"
done
printf '{"targets":{"generate-install":{"options":{"commands":["native generator"]}}}}\n' >"$out/source/apps/api/project.json"
jq -n --arg pin "${TEST_PIN:-8afa5dd37a8e723efb9eace8d09fd5c1cccef694}" \
  '{schemaVersion:1,upstreamCommit:$pin,imageAdmission:"unknown",licenseClosure:"unknown"}' >"$out/source-preparation.json"
STUB
cat >"$scratch/bin/uname" <<'STUB'
#!/usr/bin/env bash
case "$1" in -s) echo "${TEST_OS:-Linux}";; -m) echo x86_64;; esac
STUB
cat >"$scratch/bin/node" <<'STUB'
#!/usr/bin/env bash
echo "${TEST_NODE:-v22.23.3}"
STUB
cat >"$scratch/bin/pnpm" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" = --version ]]; then echo "${TEST_PNPM:-10.30.3}"; exit; fi
printf '%s\n' "$*" >>"$TEST_CALLS"
[[ "$NX_NO_CLOUD" = true && "$NX_SKIP_NX_CACHE" = true && "$NX_DAEMON" = false ]]
[[ "$COREPACK_ENABLE_PROJECT_SPEC" = 0 && "$npm_config_manage_package_manager_versions" = false ]]
if [[ "$1" = install ]]; then
  if [[ "${TEST_LOCK:-}" = changed ]]; then echo changed >>pnpm-lock.yaml; fi
elif [[ "$*" = *'@zitadel/api:generate '* ]]; then
  jq -e '.targets["generate-install"].options.parallel == false' apps/api/project.json >/dev/null
  if [[ "${TEST_FAIL:-}" = generate ]]; then echo 'Successfully ran target generate'; exit 42; fi
  for f in internal/api/assets/authz.go internal/api/assets/router.go internal/api/ui/login/statik/statik.go internal/notification/statik/statik.go internal/statik/statik.go pkg/grpc/user/user.pb.go openapi/v2/zitadel/user.swagger.json internal/api/ui/login/static/resources/themes/zitadel/css/zitadel.css; do
    mkdir -p "$(dirname "$f")"; echo generated >"$f"
  done
  [[ "${TEST_MISSING:-}" != statik ]] || rm internal/statik/statik.go
else
  grep -q '@zitadel/api:generate' "$TEST_CALLS"
  mkdir -p console/dist/console internal/api/ui/console/static
  printf '<base href="/ui/console/"><script src="./main.js"></script><link rel="stylesheet" href="./styles.css">' >console/dist/console/index.html
  for f in main.js styles.css 3rdpartylicenses.txt; do echo generated >"console/dist/console/$f"; done
  mkdir -p console/dist/console/assets
  echo 'native Go embed excludes dot files' >console/dist/console/assets/.gitignore
  if [[ "${TEST_MISSING:-}" = hidden-reference ]]; then
    echo '<script src="_main.js"></script>' >console/dist/console/index.html
    echo generated >console/dist/console/_main.js
  fi
  [[ "${TEST_MISSING:-}" != script ]] || rm console/dist/console/main.js
  if [[ "${TEST_FONT:-}" = leaked ]]; then echo Ailerons >console/dist/console/styles.css; fi
  if [[ "${TEST_FONT:-}" = file ]]; then echo unadmitted >console/dist/console/ailerons.otf; fi
  if [[ "${TEST_FONT:-}" = symlink ]]; then ln -s styles.css console/dist/console/ailerons.otf; fi
  cp -R console/dist/console/* internal/api/ui/console/static/
fi
STUB
cat >"$scratch/bin/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  'env GOVERSION') echo "${TEST_GO:-go1.26.10}";;
  'env GOROOT') echo "$TEST_GOROOT";;
  'version -m') printf 'fixture: go1.26.10\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=amd64\n\tbuild\tCGO_ENABLED=0\n';;
  'mod verify') :;;
  'run '*)
    if [[ "$2" == */zitadel-console-notices.go ]]; then
      [[ "$4" = --build && "$6" = --recipe && -s "$7" && -s "$5/packages.jsonl" ]]
      [[ "$8" = --supplement && -s "$9/catalogue.json" ]]
      [[ "${10:-}" = --toolchain && "${11:-}" = "$TEST_GOROOT" ]] || { echo 'selected compiler distribution not passed' >&2; exit 42; }
      if [[ "$3" = --snapshot ]]; then
        [[ ! -e "$5/service" && ! -e "$5/build-receipt.pending" ]]
        if [[ "${TEST_FAIL:-}" = notice-snapshot ]]; then echo 'plausible partial snapshot'; exit 42; fi
      else
        [[ "$3" = --inventory && -s "$5/console-notices.before.json" && -s "$5/service/zitadel" && -s "$5/build-receipt.pending" && -s "$5/generated.sha256" ]]
        if [[ "${TEST_FAIL:-}" = notice-capture ]]; then echo 'plausible partial capture'; exit 42; fi
        printf 'raw notice fixture\n' >"$5/console-third-party-notices.txt"
        printf 'verbatim supplement fixture\n' >"$5/console-notices-supplement.txt"
        printf '{"fixture":"supplement orchestration only"}\n' >"$5/console-notices-supplement-inventory.pending"
        if [[ "${TEST_FAIL:-}" = supplement ]]; then echo 'plausible partial supplement'; exit 42; fi
        printf 'verbatim Go distribution fixture\n' >"$5/go-toolchain-notices.txt"
        if [[ "${TEST_FAIL:-}" = toolchain ]]; then echo 'plausible partial toolchain retention'; exit 42; fi
      fi
      echo '{"fixture":"orchestration only"}'
    else
      [[ "$*" == *'zitadel-login-icons.go --inventory --build '* && "$*" == *' --notices '* ]]
      [[ -s "$5/build-receipt.pending" && -s "$5/generated.sha256" ]]
      if [[ "${TEST_FAIL:-}" = inventory ]]; then echo 'plausible partial inventory'; exit 42; fi
      echo '{"fixture":"orchestration only"}'
    fi;;
  'list -deps')
    case "${TEST_MISSING:-}" in
      login-compiled) jq -n --arg dir "$PWD/internal/api/ui/login/statik" '{ImportPath:"github.com/zitadel/zitadel/internal/api/ui/login/statik",Dir:$dir,GoFiles:["generate.go"],IgnoredGoFiles:["statik.go"]}';;
      login-directory) echo '{"ImportPath":"github.com/zitadel/zitadel/internal/api/ui/login/statik","Dir":"wrong","GoFiles":["generate.go","statik.go"]}';;
      login-extra) jq -n --arg dir "$PWD/internal/api/ui/login/statik" '{ImportPath:"github.com/zitadel/zitadel/internal/api/ui/login/statik",Dir:$dir,GoFiles:["generate.go","statik.go","unselected.go"]}';;
      *) jq -n --arg dir "$PWD/internal/api/ui/login/statik" '{ImportPath:"github.com/zitadel/zitadel/internal/api/ui/login/statik",Dir:$dir,GoFiles:["generate.go","statik.go"]}';;
    esac
    if [[ "${TEST_MISSING:-}" = embed ]]; then echo '{}'; else
      echo '{"ImportPath":"github.com/zitadel/zitadel/internal/api/ui/console","EmbedPatterns":["static"],"EmbedFiles":["static/index.html","static/main.js","static/styles.css","static/3rdpartylicenses.txt"]}'
    fi;;
  'build '*)
    while [[ "$1" != -o ]]; do shift; done; shift
    if [[ "${TEST_FAIL:-}" = compile ]]; then echo 'Successfully ran target build'; exit 42; fi
    printf '#!/usr/bin/env bash\necho "zitadel version 4.20.0"\n' >"$1"
    [[ "${TEST_MISSING:-}" != binary ]] || : >"$1"
    chmod 755 "$1";;
  *) echo "unexpected go: $*" >&2; exit 1;;
esac
STUB
cat >"$scratch/bin/time" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" = -v && "$2" = -o ]]; printf 'Maximum resident set size (kbytes): 1\n' >"$3"
shift 3; exec "$@"
STUB
cat >"$scratch/bin/readelf" <<'STUB'
#!/usr/bin/env bash
echo 'Machine: Advanced Micro Devices X86-64'
STUB
chmod 755 "$scratch/bin/"*
export PATH="$scratch/bin:$PATH" TEST_CALLS="$scratch/calls"
export TEST_GOROOT="$scratch/go-distribution"
bash "$build"
test ! -e "$TEST_CALLS"
# Record all failures so a missing guard cannot hide behind the first rejection.
failures=0
# Require a malformed-input build to fail without publishing a success receipt.
reject() {
  local label="$1"; shift
  : >"$TEST_CALLS"
  if env "$@" bash "$build" --build --source "$scratch/upstream" --output "$scratch/$label" >"$scratch/$label.log" 2>&1; then
    echo "incorrectly admitted $label" >&2; failures=$((failures + 1))
  fi
  if [[ -e "$scratch/$label/build-receipt.json" ]]; then
    echo "failure published success receipt: $label" >&2
    failures=$((failures + 1))
  fi
}
reject wrong-os TEST_OS=Darwin
reject wrong-go TEST_GO=go1.27.2
reject wrong-node TEST_NODE=v24.19.0
reject wrong-pnpm TEST_PNPM=10.28.2
reject wrong-source TEST_PIN=0000000000000000000000000000000000000000
reject changed-lock TEST_LOCK=changed
reject failed-generation TEST_FAIL=generate
reject incomplete-generation TEST_MISSING=statik
reject missing-console-script TEST_MISSING=script
reject missing-console-embed TEST_MISSING=embed
reject excluded-login-registration TEST_MISSING=login-compiled
reject substituted-login-directory TEST_MISSING=login-directory
reject extra-login-source TEST_MISSING=login-extra
reject unembedded-console-reference TEST_MISSING=hidden-reference
reject leaked-font TEST_FONT=leaked
reject leaked-font-file TEST_FONT=file
reject leaked-font-link TEST_FONT=symlink
reject failed-compiler TEST_FAIL=compile
reject empty-binary TEST_MISSING=binary
reject failed-icon-inventory TEST_FAIL=inventory
reject failed-notice-snapshot TEST_FAIL=notice-snapshot
reject failed-notice-capture TEST_FAIL=notice-capture
reject failed-supplement TEST_FAIL=supplement
reject failed-toolchain TEST_FAIL=toolchain
test "$failures" -eq 0
: >"$TEST_CALLS"
bash "$build" --build --source "$scratch/upstream" --output "$scratch/complete"
jq -e '.platform == "linux/amd64" and .imageAdmission == "unknown" and .licenseClosure == "unknown" and .reproducible == false' "$scratch/complete/build-receipt.json"
test -s "$scratch/complete/console-notices-inventory.json"
test -s "$scratch/complete/console-third-party-notices.txt"
test -s "$scratch/complete/console-notice-verifier.sha256"
test -s "$scratch/complete/console-notices-supplement.txt"
test -s "$scratch/complete/console-notices-supplement-inventory.json"
test -s "$scratch/complete/go-toolchain-notices.txt"
if bash "$build" --build --source "$scratch/upstream" --output "$scratch/complete"; then exit 1; fi
printf 'Linux build orchestration controls passed (process doubles, not upstream-build proof).\n'
