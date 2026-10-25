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
cp "$root/scripts/zitadel-source-bundle.go" "$scratch/recipe/scripts/"
mkdir -p "$scratch/recipe/server/cmd"
cp -R "$root/server/cmd/zitadel-module-sources" "$scratch/recipe/server/cmd/"
cp -R "$root/server/cmd/zitadel-bsr-sources" "$scratch/recipe/server/cmd/"
cp "$root/server/go.mod" "$root/server/go.sum" "$scratch/recipe/server/"
mkdir "$scratch/recipe/third_party"
cp -R "$root/third_party/zitadel-login-icons" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-console-notices" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-go-module-notices" "$scratch/recipe/third_party/"
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
jq -n --arg pin "${TEST_PIN:-8afa5dd37a8e723efb9eace8d09fd5c1cccef694}" --arg profile "${TEST_METADATA_PROFILE:-machine-id-no-jsonpath-v1}" \
  '{schemaVersion:1,upstreamCommit:$pin,imageAdmission:"unknown",licenseClosure:"unknown",metadataProfile:$profile}' >"$out/source-preparation.json"
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
if [[ "$1 $2" = 'exec node' ]]; then
  [[ "${TEST_FAIL:-}" != bsr-resolution ]] || exit 42
  realpath ../node_modules/.pnpm/@bufbuild+buf@1.72.0/node_modules/@bufbuild/buf-linux-x64/bin/buf
  exit
fi
if [[ "$1 $2 $3" = 'exec buf --version' ]]; then echo "${TEST_CONSOLE_BUF_VERSION:-1.72.0}"; exit; fi
printf '%s\n' "$*" >>"$TEST_CALLS"
[[ "$NX_NO_CLOUD" = true && "$NX_SKIP_NX_CACHE" = true && "$NX_DAEMON" = false ]]
[[ "$COREPACK_ENABLE_PROJECT_SPEC" = 0 && "$npm_config_manage_package_manager_versions" = false ]]
if [[ "$1" = install ]]; then
  build_root="${GOCACHE%/go-cache}"
  [[ "$GOPATH" = "$build_root/go-path" && -d "$GOPATH" ]]
  [[ "$npm_config_userconfig" = "$build_root/npmrc" && -f "$npm_config_userconfig" && ! -s "$npm_config_userconfig" ]]
  [[ "$npm_config_cache" = "$build_root/npm-cache" && -d "$npm_config_cache" ]]
  [[ "$npm_config_store_dir" = "$build_root/pnpm-store" && -d "$npm_config_store_dir" ]]
  [[ "$npm_config_globalconfig" = "$build_root/npm-globalrc" && -f "$npm_config_globalconfig" && ! -s "$npm_config_globalconfig" ]]
  [[ "$npm_config_engine_strict" = false && "$npm_config_registry" = https://registry.npmjs.org/ ]]
  [[ "$npm_config_side_effects_cache" = false && " $* " = *' --side-effects-cache=false '* ]]
  [[ "${BUF_CACHE_DIR:-}" = "$build_root/buf-cache" && -d "$BUF_CACHE_DIR" ]]
  [[ "${XDG_CONFIG_HOME:-}" = "$build_root/bsr-config" && -d "$XDG_CONFIG_HOME" && ! -v BUF_TOKEN ]]
  [[ ! -v NPM_CONFIG_REGISTRY && ! -v NPM_CONFIG_PROXY ]]
  [[ "$GOSUMDB" = sum.golang.org && "$GOPROXY" = https://proxy.golang.org ]]
  [[ -z "$GONOSUMDB" && -z "$GONOPROXY" && -z "$GOPRIVATE" && -z "$GOINSECURE" ]]
  [[ "$2" = --store-dir && "$3" = "$npm_config_store_dir" ]]
  mkdir -p node_modules/.pnpm/@bufbuild+buf@1.72.0/node_modules/@bufbuild/buf-linux-x64/bin
  cp "$TEST_BUF_STUB" node_modules/.pnpm/@bufbuild+buf@1.72.0/node_modules/@bufbuild/buf-linux-x64/bin/buf
  if [[ "${TEST_LOCK:-}" = changed ]]; then echo changed >>pnpm-lock.yaml; fi
  if [[ "${TEST_FAIL:-}" = prepared-recipe ]]; then
    printf '\n# changed after prepared admission\n' >>"$TEST_RECIPE"
  fi
elif [[ "$*" = *'@zitadel/api:generate '* ]]; then
  jq -e '.targets["generate-install"].options.parallel == false' apps/api/project.json >/dev/null
  if [[ "${TEST_FAIL:-}" = generate ]]; then echo 'Successfully ran target generate'; exit 42; fi
  for f in internal/api/assets/authz.go internal/api/assets/router.go internal/api/ui/login/statik/statik.go internal/notification/statik/statik.go internal/statik/statik.go pkg/grpc/user/user.pb.go openapi/v2/zitadel/user.swagger.json internal/api/ui/login/static/resources/themes/zitadel/css/zitadel.css; do
    mkdir -p "$(dirname "$f")"; echo generated >"$f"
  done
  [[ "${TEST_MISSING:-}" != statik ]] || rm internal/statik/statik.go
  mkdir -p .artifacts/bin/linux/amd64
  cp "$TEST_BUF_STUB" .artifacts/bin/linux/amd64/buf
  if [[ -n "${BUF_CACHE_DIR:-}" ]]; then mkdir -p "$BUF_CACHE_DIR"; echo ready >"$BUF_CACHE_DIR/cache-ready"; fi
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
  if [[ "${TEST_FAIL:-}" = bsr-tool-changed ]]; then echo changed >>.artifacts/bin/linux/amd64/buf; fi
  if [[ "${TEST_FAIL:-}" = bsr-copy-changed ]]; then echo '# changed copy' >>"${BUF_CACHE_DIR%/buf-cache}/bsr-tools/api"; fi
fi
STUB
cat >"$scratch/bin/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1 $2" in
  'env GOVERSION') echo "${TEST_GO:-go1.26.10}";;
  'env GOROOT') echo "$TEST_GOROOT";;
  'version -m')
    if [[ "$3" = "$TEST_BSR_VERIFIER" ]]; then
      printf 'fixture: %s\n\tpath\t%s\n' "${TEST_BSR_VERIFIER_GO:-go1.27.3}" "${TEST_BSR_VERIFIER_PATH:-github.com/devantler-tech/business-site/server/cmd/zitadel-bsr-sources}"
      if [[ "${TEST_FAIL:-}" = bsr-verifier-metadata ]]; then exit 42; fi
      exit
    fi
    if [[ "$3" = "$TEST_MODULE_VERIFIER" ]]; then
      if [[ "${TEST_FAIL:-}" = verifier-metadata ]]; then exit 42; fi
      printf 'fixture: %s\n\tpath\t%s\n' "${TEST_VERIFIER_GO:-go1.27.3}" "${TEST_VERIFIER_PATH:-github.com/devantler-tech/business-site/server/cmd/zitadel-module-sources}"
      exit
    fi
    printf 'fixture: go1.26.10\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=amd64\n\tbuild\tCGO_ENABLED=0\n'
    if [[ "${TEST_UNADMITTED:-}" = binary ]]; then printf '\tdep\tgithub.com/jarcoal/jpath\tv0.0.0-20140328210829-f76b8b2dbf52\th1:fixture\n'; fi
    if [[ "${TEST_UNADMITTED:-}" = binary-replacement ]]; then printf '\t=>\tgithub.com/jarcoal/jpath\tv0.0.0-20140328210829-f76b8b2dbf52\th1:fixture\n'; fi
    if [[ "${TEST_FAIL:-}" = binary-metadata ]]; then exit 42; fi;;
  'mod verify')
    if [[ "${TEST_FAIL:-}" = module-integrity && -s "${GOCACHE%/go-cache}/service/zitadel" ]]; then echo 'plausible module verification followed by failure'; exit 42; fi;;
  'run '*)
    if [[ "$2" == */zitadel-source-bundle.go ]]; then
      if [[ "${TEST_FAIL:-}" = source-capture ]]; then echo '{"manifestSha256":"plausible partial capture"}'; exit 42; fi
      case "$3" in
        --restore-build)
          [[ "$#" -eq 6 && "$4" = "$TEST_PREPARED_BUILD" && "$5" = "$(printf '%064d' 0 | tr 0 a)" && ! -e "$6" ]]
          mkdir -p "$6/prepared"
          cp -R "$TEST_PREPARED_BUILD/prepared/source" "$6/prepared/source"
          cp "$TEST_PREPARED_BUILD/prepared/source-preparation.json" "$6/prepared/source-preparation.json"
          for record in api-project.original.json api-project.serial.json inputs.sha256; do cp "$TEST_PREPARED_BUILD/$record" "$6/$record"; done
          jq -n --arg profile "${TEST_REPLAY_PROFILE:-0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d}" \
            '{manifestSha256:("a"*64),preparedProfileSha256:$profile,files:9599,records:10,bytes:261683913,recipeSha256:("b"*64)}';;
        --capture-build)
          [[ "$#" -eq 6 && ! -s "$TEST_CALLS" && ! -e "$4/service" ]]
          mkdir "$6"
          jq -n --arg recipe "$(shasum -a 256 "$5/scripts/build-zitadel-linux.sh" | awk '{print $1}')" \
            '{manifestSha256:("a"*64),preparedProfileSha256:"0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d",files:9599,records:10,bytes:261683913,recipeSha256:$recipe}';;
        --pack)
          [[ "$#" -eq 6 && "$5" = "$(printf '%064d' 0 | tr 0 a)" ]]
          echo 'source archive fixture' >"$6"
          jq -n --arg archive "$(shasum -a 256 "$6" | awk '{print $1}')" '{archiveSha256:$archive,manifestSha256:("a"*64)}';;
        --verify)
          [[ "$#" -eq 5 && -d "$4" ]]
          if [[ "${TEST_FAIL:-}" = source-readback ]]; then echo '{"files":[]}'; exit 42; fi
          echo '{"fixture":"complete source readback"}';;
        --unpack)
          [[ "$#" -eq 7 && -s "$4" && ! -e "$7" ]]
          mkdir -p "$7/source"
          cp -R "${GOCACHE%/go-cache}/prepared/source/proto" "$7/source/proto"
          echo '{"fixture":"complete source unpack"}';;
        *) exit 42;;
      esac
    elif [[ "$2" == */zitadel-console-notices.go ]]; then
      [[ "$4" = --build && "$6" = --recipe && -s "$7" && -s "$5/packages.jsonl" ]]
      [[ "$8" = --supplement && -s "$9/catalogue.json" ]]
      [[ "${10:-}" = --toolchain && "${11:-}" = "$TEST_GOROOT" ]] || { echo 'selected compiler distribution not passed' >&2; exit 42; }
      [[ "${12:-}" = --source-license ]] || { echo 'prepared root licence capture not required' >&2; exit 42; }
      [[ "${13:-}" = --modules && "$#" -eq 14 && -s "${14:-}/catalogue.json" ]] || { echo 'selected Go module root retention not required' >&2; exit 42; }
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
        printf 'verbatim source root licence fixture\n' >"$5/zitadel-root-license.txt"
        if [[ "${TEST_FAIL:-}" = source-license ]]; then echo 'plausible partial root licence retention'; exit 42; fi
        printf 'verbatim selected module fixture\n' >"$5/go-module-notices.txt"
        if [[ "${TEST_FAIL:-}" = modules ]]; then echo 'plausible partial module retention'; exit 42; fi
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
    fi
    if [[ "${TEST_UNADMITTED:-}" = graph ]]; then echo '{"ImportPath":"github.com/jarcoal/jpath","Module":{"Path":"github.com/jarcoal/jpath"}}'; fi
    if [[ "${TEST_UNADMITTED:-}" = graph-replacement ]]; then echo '{"ImportPath":"example.invalid/selector","Module":{"Path":"example.invalid/selector","Replace":{"Path":"github.com/jarcoal/jpath"}}}'; fi
    if [[ "${TEST_FAIL:-}" = package-graph ]]; then exit 42; fi;;
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
# Host acquisition settings are deliberately hostile. The native build must
# select its declared public sources and retain checksum verification instead.
export GOSUMDB=off GOPROXY=https://unrelated.invalid GONOSUMDB='*' GONOPROXY='*' GOPRIVATE='*' GOINSECURE='*'
export NPM_CONFIG_REGISTRY=https://unrelated.invalid NPM_CONFIG_PROXY=https://unrelated.invalid
export TEST_MODULE_VERIFIER="$scratch/module-source-verifier"
export TEST_BSR_VERIFIER="$scratch/bsr-source-verifier" TEST_BUF_STUB="$scratch/native-buf"
cat >"$TEST_BUF_STUB" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
role=console
[[ "$0" != */.artifacts/* && "$0" != */bsr-tools/api ]] || role=api
if [[ "$1" = --version ]]; then
  if [[ "$role" = api ]]; then echo "${TEST_API_BUF_VERSION:-1.67.0}"; else echo "${TEST_CONSOLE_BUF_VERSION:-1.72.0}"; fi
elif [[ "$1 $2 $3 $4" = 'registry module commit info' ]]; then
  jq -n --arg commit "${5##*:}" '{commit:$commit,create_time:"native fixture"}'
  [[ "${TEST_FAIL:-}" != bsr-metadata ]] || exit 42
elif [[ "$1" = export ]]; then
  while [[ "$1" != --output ]]; do shift; done; shift
  mkdir -p "$1"; echo 'syntax = "proto3";' >"$1/source.proto"
  [[ "${TEST_FAIL:-}" != bsr-export ]] || exit 42
elif [[ "$1" = build ]]; then
  [[ -s "$BUF_CACHE_DIR/cache-ready" ]] || exit 42
  while [[ "$1" != --output && "$1" != -o ]]; do shift; done; shift
  if [[ "${TEST_NETWORK_DENIED:-0}" = 1 && "${TEST_FAIL:-}" = bsr-descriptor ]]; then
    echo changed >"$1"
  else printf 'descriptor %s\n' "$role" >"$1"; fi
else echo 'unexpected native schema command' >&2; exit 42
fi
STUB
cat >"$scratch/bin/curl" <<'STUB'
#!/usr/bin/env bash
[[ "${TEST_NETWORK_DENIED:-0}" != 1 || "${TEST_FAIL:-}" = network-leak ]]
STUB
cat >"$scratch/bin/sudo" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1 $2 $3 $4 $5" = '-n unshare --net -- setpriv' ]]
[[ "${TEST_FAIL:-}" != network-isolation ]] || exit 42
shift 5
[[ "$1" = --no-new-privs && "$2" = --reuid && "$3" != 0 && "$4" = --regid && "$6 $7" = '--clear-groups --' ]]
shift 7
[[ "$1" = env ]]; shift
while [[ "$1" = -u ]]; do unset "$2"; shift 2; done
[[ "$1 $2" = 'bash -c' && "$3" = *NoNewPrivs* && "$4" = bash ]]
# This is an orchestration double; only Linux CI can prove the kernel state.
[[ "${TEST_FAIL:-}" != no-new-privileges ]] || exit 125
printf 'verified\n' >"$5"
shift 5
export TEST_NETWORK_DENIED=1
exec "$@"
STUB
for tool in unshare setpriv; do printf '#!/usr/bin/env bash\nexit 42\n' >"$scratch/bin/$tool"; done
chmod 755 "$TEST_BUF_STUB" "$scratch/bin/curl" "$scratch/bin/sudo" "$scratch/bin/unshare" "$scratch/bin/setpriv"
cat >"$TEST_BSR_VERIFIER" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "$1" in
  capture)
    [[ "$2" = --build && -s "$3/service/zitadel" && -s "$3/bsr-tools/api.version" && -s "$3/bsr-tools/console.version" ]]
    [[ "$4" = --recipe && "$6" = --source-sha256 && "$8" = --output && "$#" = 9 ]]
    [[ "${TEST_FAIL:-}" != bsr-capture ]] || { echo '{"files":170}'; exit 42; }
    mkdir "$9"
    api="$(shasum -a 256 "$3/bsr-tools/api" | awk '{print $1}')"
    console="$(shasum -a 256 "$3/bsr-tools/console" | awk '{print $1}')"
    [[ "${TEST_FAIL:-}" != bsr-tool-binding ]] || api="$(printf '%064d' 0)"
    jq -n --arg api "$api" --arg console "$console" '{tools:[{phase:"api",version:"1.67.0",sha256:$api},{phase:"console",version:"1.72.0",sha256:$console}]}' >"$9/manifest.json";;
  verify)
    [[ "$2" = --package && -s "$3/manifest.json" && "$4" = --sha256 && "$#" = 5 ]]
    [[ "${TEST_FAIL:-}" != bsr-readback ]] || { echo '{"files":170}'; exit 42; };;
  restore)
    [[ "$2" = --package && -s "$3/manifest.json" && "$4" = --sha256 && "$6" = --output && "$#" = 7 ]]
    mkdir -p "$7/v3/modules/b5/buf.build/googleapis/googleapis/75b4300737fb4efca0831636be94e517/files/google/api"
    echo original >"$7/v3/modules/b5/buf.build/googleapis/googleapis/75b4300737fb4efca0831636be94e517/files/google/api/annotations.proto"
    echo ready >"$7/cache-ready";;
  check-cache)
    [[ "$2" = --package && "$4" = --sha256 && "$6" = --cache && "$#" = 7 ]]
    [[ "${TEST_FAIL:-}" != bsr-cache ]] || exit 42
    grep -qx original "$7/v3/modules/b5/buf.build/googleapis/googleapis/75b4300737fb4efca0831636be94e517/files/google/api/annotations.proto" || exit 42;;
  *) exit 42;;
esac
[[ "${TEST_FAIL:-}" != bsr-incomplete ]] || { echo '{"modules":2}'; exit; }
jq -n '{schemaVersion:1,manifestSha256:("c"*64),modules:3,files:170,cacheFiles:113,exportFiles:49,bytes:123,selectedBSRSourcesCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
STUB
chmod 755 "$TEST_BSR_VERIFIER"
cat >"$TEST_MODULE_VERIFIER" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" = --capture ]]; then
  [[ "$#" -eq 7 && "$2" = --build && -s "$3/service/zitadel" && "$4" = --catalogue && -s "$5" && "$6" = --output ]]
  if [[ "${TEST_FAIL:-}" = module-source-capture ]]; then echo '{"manifestSha256":"plausible partial capture"}'; exit 42; fi
  mkdir "$7"
else
  [[ "$#" -eq 4 && "$1" = --verify && -d "$2" && "$3" = --sha256 && "$4" = "$(printf '%064d' 0 | tr 0 b)" ]]
  if [[ "${TEST_FAIL:-}" = module-source-readback ]]; then echo '{"modules":231}'; exit 42; fi
fi
if [[ "${TEST_FAIL:-}" = module-source-incomplete ]]; then echo '{"modules":230}'; exit; fi
jq -n '{schemaVersion:1,manifestSha256:("b"*64),modules:231,files:466,bytes:123,selectedServiceGoSourcesCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
STUB
chmod 755 "$TEST_MODULE_VERIFIER"
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
reject failed-source-license TEST_FAIL=source-license
reject failed-module-retention TEST_FAIL=modules
reject changed-module-cache TEST_FAIL=module-integrity
reject unsupported-metadata-profile TEST_METADATA_PROFILE=other
reject unadmitted-metadata-graph TEST_UNADMITTED=graph
reject unadmitted-metadata-binary TEST_UNADMITTED=binary
reject unadmitted-metadata-replacement-graph TEST_UNADMITTED=graph-replacement
reject unadmitted-metadata-replacement-binary TEST_UNADMITTED=binary-replacement
reject partial-package-graph TEST_FAIL=package-graph
reject partial-binary-metadata TEST_FAIL=binary-metadata
test "$failures" -eq 0
: >"$TEST_CALLS"
env BUF_TOKEN=unrelated-fixture-token BUF_CACHE_DIR="$scratch/ambient-buf-cache" XDG_CONFIG_HOME="$scratch/ambient-buf-config" \
  bash "$build" --build --source "$scratch/upstream" --output "$scratch/complete"
jq -e '.platform == "linux/amd64" and .imageAdmission == "unknown" and .licenseClosure == "unknown" and .reproducible == false' "$scratch/complete/build-receipt.json"
test -s "$scratch/complete/console-notices-inventory.json"
test -s "$scratch/complete/console-third-party-notices.txt"
test -s "$scratch/complete/console-notice-verifier.sha256"
test -s "$scratch/complete/console-notices-supplement.txt"
test -s "$scratch/complete/console-notices-supplement-inventory.json"
test -s "$scratch/complete/go-toolchain-notices.txt"
test -s "$scratch/complete/zitadel-root-license.txt"
test -s "$scratch/complete/go-module-notices.txt"
test "$(jq -r 'has("sourceSnapshot")' "$scratch/complete/build-receipt.json")" = false
test ! -e "$scratch/complete/selected-go-sources"
: >"$TEST_CALLS"
# A prepared replay enters the same install/generate/compile path with literal
# original records. This mock is orchestration proof only; the Go suite verifies
# the actual closed source and the hosted job exercises the native frozen tree.
TEST_PREPARED_BUILD="$(cd "$scratch/complete" && pwd -P)"
export TEST_PREPARED_BUILD
prepared_sha="$(printf '%064d' 0 | tr 0 a)"
if ! bash "$build" --build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" --output "$scratch/prepared-replay" >"$scratch/prepared-replay.log" 2>&1; then
  cat "$scratch/prepared-replay.log" >&2
  echo 'verified prepared source could not enter the existing native pipeline' >&2
  exit 1
fi
cmp "$scratch/complete/api-project.original.json" "$scratch/prepared-replay/api-project.original.json"
cmp "$scratch/complete/api-project.serial.json" "$scratch/prepared-replay/api-project.serial.json"
jq -e --arg seal "$prepared_sha" '.sourceInput.manifestSha256 == $seal and .sourceInput.nativeOfflineReplayProved == false and (has("sourceSnapshot")|not)' "$scratch/prepared-replay/build-receipt.json" >/dev/null
: >"$TEST_CALLS"
# Mutation occurs after prepared-source admission but before the notice snapshot.
# A final receipt must not join the old admitted recipe with the changed builder.
if env TEST_FAIL=prepared-recipe TEST_RECIPE="$build" bash "$build" --build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" --output "$scratch/prepared-recipe-change" >"$scratch/prepared-recipe-change.log" 2>&1; then
  echo 'changed admitted prepared recipe produced a success receipt' >&2
  exit 1
fi
test ! -e "$scratch/prepared-recipe-change/build-receipt.json"
grep -q 'prepared source recipe changed during build' "$scratch/prepared-recipe-change.log"
cp "$root/scripts/build-zitadel-linux.sh" "$build"
for fault in unsealed-source both-source-modes capture-with-prepared substituted-prepared-profile; do
  : >"$TEST_CALLS"
  args=(--build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" --output "$scratch/$fault")
  case "$fault" in
    unsealed-source) args=(--build --prepared-source "$TEST_PREPARED_BUILD" --output "$scratch/$fault");;
    both-source-modes) args+=(--source "$scratch/upstream");;
    capture-with-prepared) args+=(--capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER");;
  esac
  profile=0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d
  [[ "$fault" != substituted-prepared-profile ]] || profile=wrong
  if env TEST_REPLAY_PROFILE="$profile" bash "$build" "${args[@]}" >"$scratch/$fault.log" 2>&1; then
    echo "unsafe prepared source admitted: $fault" >&2; exit 1
  fi
  test ! -e "$scratch/$fault/build-receipt.json"
done
: >"$TEST_CALLS"
# Explicit build-tool admission has independent refusals; none may rely on a
# missing capture opt-in or bypass an older capture/readback failure control.
capture_reject() {
  local label="$1" verifier="$2"; shift 2
  : >"$TEST_CALLS"
  if env "$@" bash "$build" --build --capture-source --module-source-verifier "$verifier" --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/$label" >"$scratch/$label.log" 2>&1; then
    echo "incorrectly admitted module verifier: $label" >&2; exit 1
  fi
  test ! -e "$scratch/$label/build-receipt.json"
}
ln -s "$TEST_MODULE_VERIFIER" "$scratch/linked-verifier"
cp "$TEST_MODULE_VERIFIER" "$scratch/nonexecutable-verifier"
chmod 644 "$scratch/nonexecutable-verifier"
capture_reject linked-verifier "$scratch/linked-verifier"
capture_reject missing-verifier "$scratch/absent-verifier"
capture_reject nonexecutable-verifier "$scratch/nonexecutable-verifier"
capture_reject relative-verifier ./module-source-verifier
capture_reject wrong-verifier-compiler "$TEST_MODULE_VERIFIER" TEST_VERIFIER_GO=go1.26.10
capture_reject wrong-verifier-command "$TEST_MODULE_VERIFIER" TEST_VERIFIER_PATH=example.invalid/command
capture_reject partial-verifier-metadata "$TEST_MODULE_VERIFIER" TEST_FAIL=verifier-metadata
# These carry a valid module verifier so each schema-tool admission is exercised.
bsr_reject() {
  local label="$1" verifier="$2"; shift 2
  : >"$TEST_CALLS"
  if env "$@" bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$verifier" --source "$scratch/upstream" --output "$scratch/$label" >"$scratch/$label.log" 2>&1; then
    echo "incorrectly admitted schema verifier: $label" >&2; exit 1
  fi
  test ! -e "$scratch/$label/build-receipt.json"
}
ln -s "$TEST_BSR_VERIFIER" "$scratch/linked-bsr-verifier"
cp "$TEST_BSR_VERIFIER" "$scratch/nonexecutable-bsr-verifier"
chmod 644 "$scratch/nonexecutable-bsr-verifier"
bsr_reject linked-bsr-verifier "$scratch/linked-bsr-verifier"
bsr_reject missing-bsr-verifier "$scratch/absent-bsr-verifier"
bsr_reject nonexecutable-bsr-verifier "$scratch/nonexecutable-bsr-verifier"
bsr_reject relative-bsr-verifier ./bsr-source-verifier
bsr_reject wrong-bsr-verifier-compiler "$TEST_BSR_VERIFIER" TEST_BSR_VERIFIER_GO=go1.26.10
bsr_reject wrong-bsr-verifier-command "$TEST_BSR_VERIFIER" TEST_BSR_VERIFIER_PATH=example.invalid/command
bsr_reject partial-bsr-verifier-metadata "$TEST_BSR_VERIFIER" TEST_FAIL=bsr-verifier-metadata
bsr_reject wrong-api-schema-version "$TEST_BSR_VERIFIER" TEST_API_BUF_VERSION=1.72.0
bsr_reject wrong-console-schema-version "$TEST_BSR_VERIFIER" TEST_CONSOLE_BUF_VERSION=1.67.0
if bash "$build" --build --capture-source --source "$scratch/upstream" --output "$scratch/no-verifier" >"$scratch/no-verifier.log" 2>&1; then exit 1; fi
if bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --source "$scratch/upstream" --output "$scratch/no-bsr-verifier" >"$scratch/no-bsr-verifier.log" 2>&1; then exit 1; fi
if bash "$build" --build --module-source-verifier "$TEST_MODULE_VERIFIER" --source "$scratch/upstream" --output "$scratch/no-capture" >"$scratch/no-capture.log" 2>&1; then exit 1; fi
if bash "$build" --build --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/no-bsr-capture" >"$scratch/no-bsr-capture.log" 2>&1; then exit 1; fi
if bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/duplicate-bsr-verifier" >"$scratch/duplicate-bsr-verifier.log" 2>&1; then exit 1; fi
: >"$TEST_CALLS"
bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/captured"
jq -e '.sourceSnapshot.preparedProfileSha256 == "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d" and .sourceSnapshot.files == 9599 and .sourceSnapshot.records == 10 and .sourceSnapshot.dependencyInputsCaptured == false and .sourceSnapshot.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.goModules.modules == 231 and .sourceSnapshot.goModules.files == 466 and .sourceSnapshot.goModules.selectedServiceGoSourcesCaptured == true and .sourceSnapshot.goModules.dependencyInputsCaptured == false and .sourceSnapshot.goModules.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e --arg digest "$(shasum -a 256 "$TEST_MODULE_VERIFIER" | awk '{print $1}')" '.sourceSnapshot.goModuleVerifier == {go:"1.27.3",sha256:$digest}' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.bsrSources.modules == 3 and .sourceSnapshot.bsrSources.files == 170 and .sourceSnapshot.bsrSources.selectedBSRSourcesCaptured == true and .sourceSnapshot.bsrSchemaReplay.schemaOfflineReplayProved == true and .sourceSnapshot.bsrSchemaReplay.noNewPrivilegesReplay == true and .sourceSnapshot.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
for failure in bsr-copy-changed source-capture source-readback module-source-capture module-source-readback module-source-incomplete bsr-tool-changed bsr-tool-binding bsr-resolution bsr-metadata bsr-export bsr-capture bsr-readback bsr-incomplete bsr-cache bsr-descriptor network-isolation network-leak no-new-privileges; do
  : >"$TEST_CALLS"
  if env TEST_FAIL="$failure" bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/$failure" >"$scratch/$failure.log" 2>&1; then echo "failed source capture/readback admitted: $failure" >&2; exit 1; fi
  test ! -e "$scratch/$failure/build-receipt.json"
done
if bash "$build" --build --source "$scratch/upstream" --output "$scratch/complete"; then exit 1; fi
printf 'Linux build orchestration controls passed (process doubles, not upstream-build proof).\n'
