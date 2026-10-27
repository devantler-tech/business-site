#!/usr/bin/env bash
# Orchestration controls use fake processes; the separate CI job builds real upstream.
set -euo pipefail
export LC_ALL=C
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
scratch="$(cd "$scratch" && pwd -P)"
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/recipe/scripts" "$scratch/bin" "$scratch/upstream"
mkdir "$scratch/go-distribution"
cp "$root/scripts/testdata/go1.26.10/LICENSE" "$root/scripts/testdata/go1.26.10/PATENTS" "$scratch/go-distribution/"
printf 'go1.26.10\n' >"$scratch/go-distribution/VERSION"
cp "$root/scripts/build-zitadel-linux.sh" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-login-icons.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-text-fonts.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-copied-assets.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-console-notices.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-source-bundle.go" "$scratch/recipe/scripts/"
cp "$root/scripts/zitadel-console-assets.go" "$scratch/recipe/scripts/"
mkdir -p "$scratch/recipe/tools/zitadel-pnpm-source"
cp "$root/tools/zitadel-pnpm-source/"{index.mjs,package.json,package-lock.json} "$scratch/recipe/tools/zitadel-pnpm-source/"
mkdir -p "$scratch/recipe/server/cmd"
cp -R "$root/server/cmd/zitadel-module-sources" "$scratch/recipe/server/cmd/"
cp -R "$root/server/cmd/zitadel-bsr-sources" "$scratch/recipe/server/cmd/"
cp "$root/server/go.mod" "$root/server/go.sum" "$scratch/recipe/server/"
mkdir "$scratch/recipe/third_party"
cp -R "$root/third_party/zitadel-login-icons" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-text-fonts" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-copied-assets" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-console-notices" "$scratch/recipe/third_party/"
cp -R "$root/third_party/zitadel-go-module-notices" "$scratch/recipe/third_party/"
build="$scratch/recipe/scripts/build-zitadel-linux.sh"
# These are process doubles, not alternative production build entrypoints.
cat >"$scratch/recipe/scripts/prepare-zitadel-source.sh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
out="$5"
mkdir -p "$out/source/console/src" "$out/source/console/scripts" "$out/source/apps/api" "$out/source/proto" "$out/source/internal/api/ui/login/static/resources/fonts/ailerons"
printf 'native installer fixture\n' >"$out/source/console/scripts/install-proto-plugins.sh"
for f in go.mod go.sum package.json pnpm-lock.yaml pnpm-workspace.yaml console/package.json proto/buf.lock; do
  printf 'frozen input\n' >"$out/source/$f"
done
printf '{"targets":{"generate-install":{"options":{"commands":["native generator"]}}}}\n' >"$out/source/apps/api/project.json"
jq -n --arg pin "${TEST_PIN:-8afa5dd37a8e723efb9eace8d09fd5c1cccef694}" --arg profile "${TEST_METADATA_PROFILE:-machine-id-no-jsonpath-v1}" \
  --arg dependency "${TEST_DEPENDENCY_PROFILE:-go-jose-4.1.6-v1}" \
  '{schemaVersion:1,upstreamCommit:$pin,imageAdmission:"unknown",licenseClosure:"unknown",metadataProfile:$profile,dependencyProfile:$dependency} |
    if $dependency == "missing" then del(.dependencyProfile) else . end' >"$out/source-preparation.json"
STUB
cat >"$scratch/bin/uname" <<'STUB'
#!/usr/bin/env bash
case "$1" in -s) echo "${TEST_OS:-Linux}";; -m) echo x86_64;; esac
STUB
cat >"$scratch/bin/node" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1" = --version ]]; then echo "${TEST_NODE:-v22.23.3}"; exit; fi
[[ "$1" == */tools/zitadel-pnpm-source/index.mjs ]]
if [[ "$2" = inspect ]]; then
  [[ "$#" -eq 4 && "$3" = --build && -s "$4/pnpm-selected-projects.json" && ! -e "$4/generate.log" ]]
  [[ "${TEST_FAIL:-}" != pnpm-early-check ]] || exit 42
  packages=1
  [[ "${TEST_FAIL:-}" != pnpm-early-incomplete ]] || packages=0
  jq -n --argjson packages "$packages" '{schemaVersion:1,profile:"native-installation-selection-v1",packages:$packages,optionalSkips:0,selectedPNPMSourcesCaptured:false,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
elif [[ "$2" = capture ]]; then
  [[ "$3" = --build && "$5" = --source-sha256 && "$7" = --output && "$9" = --scratch ]]
  jq -e 'length == 4' "$4/pnpm-selected-projects.json" >/dev/null
  [[ "${TEST_FAIL:-}" != pnpm-capture ]] || { echo '{"plausible":"partial"}'; exit 42; }
  mkdir "$8"
  packages=1
  if [[ "${TEST_FAIL:-}" = pnpm-incomplete ]]; then packages=0; fi
  jq -n --arg source "$6" --argjson packages "$packages" \
    '{schemaVersion:1,manifestSha256:("e"*64),sourceManifestSha256:$source,packages:$packages,archives:1,files:14,bytes:123,selectedPNPMSourcesCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}' >"$8/receipt.json"
  cat "$8/receipt.json"
  if [[ "${TEST_FAIL:-}" = pnpm-tool-changed ]]; then echo changed >>"$1"; fi
elif [[ "$2" = restore ]]; then
  [[ "$#" -eq 10 && "$3" = --capsule && "$5" = --sha256 && "$7" = --scratch && "$9" = --store ]]
  [[ "${TEST_FAIL:-}" != retained-pnpm ]] || exit 42
  mkdir "${10}"
  printf 'restored native CAFS\n' >"${10}/native-restore-control"
  jq '. + {nativeStoreRestored:true}' "$4/receipt.json"
elif [[ "$2" = verify ]]; then
  [[ "$3" = --capsule && "$5" = --sha256 && "$7" = --scratch ]]
  [[ "${TEST_FAIL:-}" != pnpm-readback ]] || exit 42
  cat "$4/receipt.json"
else exit 42
fi
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
if [[ "$*" = '--filter @zitadel/zitadel --filter @zitadel/console... list --depth -1 --json' ]]; then
  jq -n --arg root "$PWD" '[{name:"@zitadel/zitadel",path:$root},{name:"@zitadel/console",path:($root+"/console")},{name:"client",path:($root+"/packages/zitadel-client")},{name:"proto",path:($root+"/packages/zitadel-proto")}]'
  [[ "${TEST_FAIL:-}" != pnpm-selection ]] || exit 42
  exit
fi
printf '%s\n' "$*" >>"$TEST_CALLS"
[[ "$NX_NO_CLOUD" = true && "$NX_SKIP_NX_CACHE" = true && "$NX_DAEMON" = false ]]
[[ "$COREPACK_ENABLE_PROJECT_SPEC" = 0 && "$npm_config_manage_package_manager_versions" = false ]]
if [[ "$1" = install ]]; then
  build_root="${GOCACHE%/go-cache}"
  if [[ "${npm_config_offline:-false}" = true ]]; then
    [[ "$GOPATH" = "$build_root/go-input-native/go-path" && -d "$GOPATH" && "$GOMODCACHE" = "$build_root/go-input-native/go-modules" ]]
    [[ "$GOPROXY" = "$TEST_RETAINED_PROXY_URL" && "$GOVCS" = '*:off' && "$2" = --offline ]]
    [[ -s "$npm_config_store_dir/v10/native-restore-control" ]]
    set -- "$1" "${@:3}"
  else
    [[ "$GOPATH" = "$build_root/go-path" && -d "$GOPATH" && "$GOPROXY" = https://proxy.golang.org ]]
  fi
  [[ "$npm_config_userconfig" = "$build_root/npmrc" && -f "$npm_config_userconfig" && ! -s "$npm_config_userconfig" ]]
  [[ "$npm_config_cache" = "$build_root/npm-cache" && -d "$npm_config_cache" ]]
  [[ "$npm_config_store_dir" = "$build_root/pnpm-store" && -d "$npm_config_store_dir" ]]
  [[ "$npm_config_globalconfig" = "$build_root/npm-globalrc" && -f "$npm_config_globalconfig" && ! -s "$npm_config_globalconfig" ]]
  [[ "$npm_config_engine_strict" = false && "$npm_config_registry" = https://registry.npmjs.org/ ]]
  [[ "$npm_config_side_effects_cache" = false && " $* " = *' --side-effects-cache=false '* ]]
  [[ "${BUF_CACHE_DIR:-}" = "$build_root/buf-cache" && -d "$BUF_CACHE_DIR" ]]
  [[ "${XDG_CONFIG_HOME:-}" = "$build_root/bsr-config" && -d "$XDG_CONFIG_HOME" && ! -v BUF_TOKEN ]]
  [[ ! -v NPM_CONFIG_REGISTRY && ! -v NPM_CONFIG_PROXY ]]
  [[ "$GOSUMDB" = sum.golang.org ]]
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
  printf 'API OpenAPI 2.28.0 fixture\n' >.artifacts/bin/linux/amd64/protoc-gen-openapiv2
  if [[ -n "${BUF_CACHE_DIR:-}" ]]; then mkdir -p "$BUF_CACHE_DIR"; echo ready >"$BUF_CACHE_DIR/cache-ready"; fi
else
  grep -q '@zitadel/api:generate' "$TEST_CALLS"
  if [[ "${TEST_FAIL:-}" = generator-checkpoint-changed ]]; then echo changed >>"${GOCACHE%/go-cache}/generator-checkpoint.json"; fi
  if [[ -n "${CONSOLE_ORIGINALS_TOOL:-}" ]]; then
    [[ "$TMPDIR" = "${GOCACHE%/go-cache}/console-download-tmp" ]]
    [[ "$(command -v curl)" = "${GOCACHE%/go-cache}/console-download-adapter/curl" ]]
    native_tmp="$(mktemp -d "$TMPDIR/zitadel-console-proto-plugins.XXXXXX")"
    curl -fsSL https://github.com/grpc/grpc-web/releases/download/1.5.0/protoc-gen-grpc-web-1.5.0-linux-x86_64 -o "$native_tmp/protoc-gen-grpc-web"
    curl -fsSL https://github.com/protocolbuffers/protobuf-javascript/releases/download/v4.0.2/protobuf-javascript-4.0.2-linux-x86_64.zip -o "$native_tmp/p.zip"
    if [[ "${TEST_FAIL:-}" != console-request-incomplete ]]; then
      curl -fsSL https://github.com/grpc-ecosystem/grpc-gateway/releases/download/v2.22.0/protoc-gen-openapiv2-v2.22.0-linux-x86_64 -o "$native_tmp/protoc-gen-openapiv2"
    fi
    cp "$CONSOLE_ORIGINALS_CAPSULE/protoc-gen-grpc-web" .artifacts/bin/linux/amd64/protoc-gen-grpc-web
    cp "$CONSOLE_ORIGINALS_CAPSULE/protoc-gen-openapiv2" .artifacts/bin/linux/amd64/protoc-gen-openapiv2
    if [[ "${TEST_FAIL:-}" = console-tool-changed ]]; then echo changed >>"$CONSOLE_ORIGINALS_TOOL"; fi
    if [[ "${TEST_FAIL:-}" = console-launcher-changed ]]; then echo '# changed launcher' >>"$(command -v curl)"; fi
  fi
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
    if [[ "$3" == *.artifacts/bin/linux/amd64/protoc-gen-openapiv2 ]]; then
      printf 'fixture: go1.26.10\n\tmod\tgithub.com/grpc-ecosystem/grpc-gateway/v2\t%s\n' "${TEST_API_OPENAPI_VERSION:-v2.28.0}"
      exit
    fi
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
    if [[ "$GOMODCACHE" == */go-input-native/go-modules ]]; then test -f "$GOMODCACHE/service-consumed"; fi
    if [[ "${TEST_FAIL:-}" = module-integrity && -s "${GOCACHE%/go-cache}/service/zitadel" ]]; then echo 'plausible module verification followed by failure'; exit 42; fi;;
  'mod download')
    [[ "$#" -eq 4 && "$3" = -json && "$4" = example.com/fixture@v1.0.0 && "$GOMODCACHE" == */go-input-native/go-modules ]]
    [[ "${TEST_FAIL:-}" != retained-go-consumption ]] || { echo '{}'; exit 42; }
    if [[ "${TEST_FAIL:-}" = retained-go-consumption-partial ]]; then echo '{}'; exit; fi
    printf 'native service consumption\n' >"$GOMODCACHE/service-consumed"
    jq -n --arg sum "${TEST_NATIVE_SUM:-h1:fixture}" '{Path:"example.com/fixture",Version:"v1.0.0",Sum:$sum,GoModSum:"h1:mod"}';;
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
            '{manifestSha256:("a"*64),preparedProfileSha256:$profile,files:9599,records:28,bytes:261683913,recipeSha256:("b"*64)}';;
        --capture-build)
          [[ "$#" -eq 6 && ! -s "$TEST_CALLS" && ! -e "$4/service" ]]
          mkdir -p "$6/records/recipes" "$6/source/console/scripts"
          cp "$5/scripts/zitadel-console-assets.go" "$6/records/recipes/zitadel-console-assets.go"
          cp "$5/scripts/"{zitadel-login-icons.go,zitadel-text-fonts.go,zitadel-copied-assets.go} "$6/records/recipes/"
          mkdir -p "$6/records/third_party/zitadel-text-fonts"
          cp "$5/third_party/zitadel-text-fonts/"{catalogue.json,Apache-2.0.txt} "$6/records/third_party/zitadel-text-fonts/"
          mkdir -p "$6/records/third_party/zitadel-copied-assets"
          cp "$5/third_party/zitadel-copied-assets/"{catalogue.json,Line-Awesome-LICENSE.md,Material-Icons-LICENSE,Flag-Icons-LICENSE,original-line-awesome.css,original-line-awesome.min.css,ZITADEL-LICENSE,ZITADEL-LICENSING.md,ZITADEL-CONTRIBUTING.md} "$6/records/third_party/zitadel-copied-assets/"
          mkdir -p "$6/records/recipes/zitadel-pnpm-source"
          cp "$5/tools/zitadel-pnpm-source/"{index.mjs,package.json,package-lock.json} "$6/records/recipes/zitadel-pnpm-source/"
          cp "$4/prepared/source/console/scripts/install-proto-plugins.sh" "$6/source/console/scripts/install-proto-plugins.sh"
          jq -n --arg recipe "$(shasum -a 256 "$5/scripts/build-zitadel-linux.sh" | awk '{print $1}')" \
            '{manifestSha256:("a"*64),preparedProfileSha256:"0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d",files:9599,records:28,bytes:261683913,recipeSha256:$recipe}';;
        --pack)
          [[ "$#" -eq 6 && "$5" = "$(printf '%064d' 0 | tr 0 a)" ]]
          echo 'source archive fixture' >"$6"
          jq -n --arg archive "$(shasum -a 256 "$6" | awk '{print $1}')" '{archiveSha256:$archive,manifestSha256:("a"*64)}';;
        --verify)
          [[ "$#" -eq 5 && -d "$4" ]]
          if [[ "${TEST_FAIL:-}" = source-readback ]]; then echo '{"files":[]}'; exit 42; fi
          if [[ "$4" == */retained-fixture ]]; then
            printf '{"phase":"%s"}\n' "${TEST_INPUT_PHASE:-retained-build-inputs}"
          else echo '{"fixture":"complete source readback"}'; fi;;
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
      [[ "${13:-}" = --modules && -s "${14:-}/catalogue.json" ]] || { echo 'selected Go module root retention not required' >&2; exit 42; }
      [[ ( "$#" -eq 14 && "$GOMODCACHE" = "$5/go-modules" ) || ( "$#" -eq 15 && "${15:-}" = --retained-go-cache && "$GOMODCACHE" = "$5/go-input-native/go-modules" ) ]] || { echo 'notice reader cache differs from native compiler' >&2; exit 42; }
      if [[ "$3" = --snapshot ]]; then
        [[ ! -e "$5/service" && ! -e "$5/build-receipt.pending" ]]
        if [[ "${TEST_FAIL:-}" = notice-snapshot ]]; then echo 'plausible partial snapshot'; exit 42; fi
      else
        [[ "$3" = --inventory && -s "$5/console-notices.before.json" && -s "$5/service/zitadel" && -s "$5/build-receipt.pending" && -s "$5/generated.sha256" ]]
        shasum -a 256 "$5/build-receipt.pending" | awk '{print $1}' >"$5/fixture-console-receipt.sha256"
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
    elif [[ "$*" == *' --copied-assets '* ]]; then
      [[ "$*" == *'zitadel-login-icons.go '* && "$*" == *'zitadel-text-fonts.go '* && "$*" == *'zitadel-copied-assets.go --copied-assets '* && "$*" == *' --recipe '* && "$*" == *' --notices '* ]]
      args=("$@"); copied_build=''
      for ((i=0;i<${#args[@]}-1;i++));do if [[ "${args[$i]}" = --build ]];then copied_build="${args[$((i+1))]}";fi;done
      [[ -n "$copied_build" && -s "$copied_build/packages.jsonl" ]]
      if [[ "$*" == *' --snapshot '* ]];then
        [[ ! -e "$copied_build/service" ]]
        if [[ "${TEST_FAIL:-}" = copied-snapshot ]];then echo 'plausible partial copied snapshot';exit 42;fi
      else
        [[ "$*" == *' --inventory '* && -s "$copied_build/copied-assets.before.json" && -s "$copied_build/build-receipt.pending" && -s "$copied_build/text-font-notices-inventory.json" ]]
        shasum -a 256 "$copied_build/build-receipt.pending" | awk '{print $1}' >"$copied_build/fixture-copied-assets-receipt.sha256"
        if [[ "${TEST_FAIL:-}" = copied-capture ]];then echo 'plausible partial copied capture';exit 42;fi
        printf 'complete original icon and flag terms fixture\n' >"$copied_build/copied-asset-notices.txt"
      fi
      echo '{"fixture":"copied asset orchestration only"}'
    elif [[ "$*" == *' --text-fonts '* ]]; then
      [[ "$*" == *'zitadel-login-icons.go '* && "$*" == *'zitadel-text-fonts.go --text-fonts '* && "$*" == *' --recipe '* && "$*" == *' --notices '* ]]
      args=("$@"); text_build=''
      for ((i=0;i<${#args[@]}-1;i++)); do if [[ "${args[$i]}" = --build ]]; then text_build="${args[$((i+1))]}"; fi; done
      [[ -n "$text_build" && -s "$text_build/packages.jsonl" ]]
      if [[ "$*" == *' --snapshot '* ]]; then
        [[ ! -e "$text_build/service" ]]
        if [[ "${TEST_FAIL:-}" = text-snapshot ]]; then echo 'plausible partial snapshot'; exit 42; fi
      else
        [[ "$*" == *' --inventory '* && -s "$text_build/text-fonts.before.json" && -s "$text_build/build-receipt.pending" ]]
        shasum -a 256 "$text_build/build-receipt.pending" | awk '{print $1}' >"$text_build/fixture-text-fonts-receipt.sha256"
        if [[ "${TEST_FAIL:-}" = text-capture ]]; then echo 'plausible partial inventory'; exit 42; fi
        printf 'complete original text-font terms fixture\n' >"$text_build/text-font-notices.txt"
      fi
      echo '{"fixture":"text-font orchestration only"}'
    else
      [[ "$*" == *'zitadel-login-icons.go '* && "$*" == *'zitadel-text-fonts.go --inventory --build '* && "$*" == *' --notices '* ]]
      [[ -s "$6/build-receipt.pending" && -s "$6/generated.sha256" ]]
      shasum -a 256 "$6/build-receipt.pending" | awk '{print $1}' >"$6/fixture-icons-receipt.sha256"
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
    if [[ "$1" == */console-originals-tool ]]; then
      cp "$TEST_CONSOLE_TOOL_STUB" "$1"; chmod 755 "$1"; exit
    fi
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
export TEST_CONSOLE_TOOL_STUB="$scratch/console-tool-stub"
cat >"$TEST_CONSOLE_TOOL_STUB" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
operation="$1"; shift
capsule='' seal='' requests=''
while [[ "$#" -gt 0 && "$1" != -- ]]; do
  case "$1" in
    --capsule) capsule="$2";; --sha256) seal="$2";; --requests) requests="$2";;
    --curl|--temp-root) :;; *) exit 42;;
  esac
  shift 2
done
case "$operation" in
  capture)
    [[ "${TEST_FAIL:-}" != console-capture ]] || exit 42
    mkdir -m 700 "$capsule"
    for name in protoc-gen-grpc-web protobuf-javascript.zip protoc-gen-openapiv2; do printf 'original %s\n' "$name" >"$capsule/$name"; done
    jq -n '{manifestSha256:("d"*64),originalConsoleAssetsCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}';;
  verify)
    [[ -d "$capsule" && "$seal" = "$(printf '%064d' 0 | tr 0 d)" && "${TEST_FAIL:-}" != console-verify ]] || exit 42
    jq -n '{manifestSha256:("d"*64),originalConsoleAssetsCaptured:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}';;
  native-request)
    shift; [[ "$#" = 4 && "$1" = -fsSL && "$3" = -o ]]
    printf 'served\n' >"$4"
    printf '%s\n' "$2" >>"$requests/observed";;
  consumption)
    [[ "$(wc -l <"$requests/observed" | tr -d ' ')" = 3 ]] || exit 42
    jq -n '{schemaVersion:1,manifestSha256:("d"*64),requests:3,requestsSha256:("e"*64),originalConsoleDownloadsConsumed:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}';;
  *) exit 42;;
esac
STUB
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
    jq -n --arg api "$api" --arg console "$console" --arg source "$7" --arg recipe "$(shasum -a 256 "$5" | awk '{print $1}')" '{sourceManifestSha256:$source,recipeSha256:$recipe,tools:[{phase:"api",version:"1.67.0",sha256:$api},{phase:"console",version:"1.72.0",sha256:$console}]}' >"$9/manifest.json";;
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
if [[ "$1" = observe-generators ]]; then
  [[ "$#" -eq 5 && "$2" = --build && -e "$3/generate.log" && ! -e "$3/console.log" && "$4" = --source-sha256 ]]
  [[ "${TEST_FAIL:-}" != generator-observation ]] || exit 42
  printf 'native generator checkpoint\n'
  exit
fi
if [[ "$1" = verify-generator-checkpoint ]]; then
  [[ "$#" -eq 7 && "$2" = --checkpoint && -s "$3" && "$4" = --sha256 && "$6" = --source-sha256 ]]
  [[ "$5" = "$(shasum -a 256 "$3" | awk '{print $1}')" ]]
  [[ "${TEST_FAIL:-}" != generator-readback ]] || exit 42
  tools=14
  [[ "${TEST_FAIL:-}" != generator-incomplete ]] || tools=13
  jq -n --arg seal "$5" --arg source "$7" --argjson tools "$tools" '{schemaVersion:1,sourceManifestSha256:$source,checkpointSha256:$seal,tools:$tools,modules:123,generatorBuildMetadataValidated:true,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
  exit
fi
if [[ "$1" = capture-generator-sources || "$1" = verify-generator-sources ]]; then
  if [[ "$1" = capture-generator-sources ]]; then
    [[ "$#" -eq 9 && "$2" = --build && -s "$3/service/zitadel" && "$4" = --checkpoint-sha256 && "$6" = --source-sha256 && "$8" = --output ]]
    [[ "$5" = "$(shasum -a 256 "$3/generator-checkpoint.json" | awk '{print $1}')" ]]
    [[ "${TEST_FAIL:-}" != generator-source-capture ]] || { echo '{"modules":123}'; exit 42; }
    mkdir "$9"
    printf '%s' "$5" >"$9/checkpoint-seal"
    checkpoint="$5"; source="$7"
  else
    [[ "$#" -eq 7 && "$2" = --package && -d "$3" && "$4" = --sha256 && "$6" = --source-sha256 ]]
    [[ "$5" = "$(printf '%064d' 0 | tr 0 e)" ]]
    [[ "${TEST_FAIL:-}" != generator-source-readback ]] || exit 42
    checkpoint="$(cat "$3/checkpoint-seal")"; source="$7"
  fi
  proved=true
  [[ "${TEST_FAIL:-}" != generator-source-incomplete ]] || proved=false
  jq -n --arg checkpoint "$checkpoint" --arg source "$source" --argjson proved "$proved" '{schemaVersion:1,manifestSha256:("e"*64),sourceManifestSha256:$source,checkpointSha256:$checkpoint,modules:123,proofFiles:2,files:373,bytes:123,selectedGeneratorGoSourcesCaptured:$proved,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
  exit
fi
if [[ "$1" = capture-graph-metadata || "$1" = verify-graph-metadata ]]; then
  [[ "$#" -eq 7 ]]
  if [[ "$1" = capture-graph-metadata ]]; then
    [[ "$2" = --build && -s "$3/service/zitadel" && "$4" = --source-sha256 && "$6" = --output ]]
    [[ "${TEST_FAIL:-}" != graph-metadata-capture ]] || { echo '{"modules":123}'; exit 42; }
    mkdir "$7"
    source="$5"
  else
    [[ "$2" = --package && -d "$3" && "$4" = --sha256 && "$6" = --source-sha256 ]]
    [[ "$5" = "$(printf '%064d' 0 | tr 0 f)" ]]
    [[ "${TEST_FAIL:-}" != graph-metadata-readback ]] || exit 42
    source="$7"
  fi
  proved=true
  [[ "${TEST_FAIL:-}" != graph-metadata-incomplete ]] || proved=false
  jq -n --arg source "$source" --argjson proved "$proved" '{schemaVersion:1,manifestSha256:("f"*64),sourceManifestSha256:$source,modules:400,metadataFiles:500,proofFiles:2,files:504,bytes:123,goGraphMetadataCaptured:$proved,dependencyInputsCaptured:false,nativeOfflineReplayProved:false}'
  exit
fi
if [[ "$1" = restore-go-inputs ]]; then
  [[ "$#" -eq 17 && "$2" = --service && "$4" = --service-sha256 && "$6" = --generators && "$8" = --generators-sha256 && "${10}" = --metadata && "${12}" = --metadata-sha256 && "${14}" = --source-sha256 && "${16}" = --output ]]
  [[ "$5" = "$(printf '%064d' 0 | tr 0 b)" && "$9" = "$(printf '%064d' 0 | tr 0 e)" && "${13}" = "$(printf '%064d' 0 | tr 0 f)" && "${15}" = "$(printf '%064d' 0 | tr 0 a)" ]]
  [[ "${TEST_FAIL:-}" != retained-go ]] || exit 42
  mkdir -p "${17}/go-modules" "${17}/go-path" "${17}/go-proxy"
  jq -n '{schemaVersion:1,modules:[{Path:"example.com/fixture",Version:"v1.0.0"}]}' >"${17}/manifest.json"
  native_manifest="$(shasum -a 256 "${17}/manifest.json" | awk '{print $1}')"
  proxy="file://${17}/go-proxy" scope=retained-snapshot
  case "${TEST_FAIL:-}" in
    retained-go-remote) proxy=https://unrelated.invalid ;;
    retained-go-fallback) proxy="$proxy,direct" ;;
    retained-go-scope) scope=upstream-latest ;;
  esac
  jq -n --arg source "${15}" --arg manifest "$native_manifest" --arg proxy "$proxy" --arg scope "$scope" --arg fault "${TEST_FAIL:-}" '{schemaVersion:1,manifestSha256:$manifest,sourceManifestSha256:$source,selectedGoInputsRestored:true,modules:340,moduleProxyScope:$scope,dependencyInputsCaptured:false,nativeOfflineReplayProved:false} + (if $fault=="retained-go-remote" or $fault=="retained-go-fallback" then {moduleProxyURL:$proxy} else {} end)'
  exit
fi
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
reject failed-text-font-snapshot TEST_FAIL=text-snapshot
reject failed-text-font-capture TEST_FAIL=text-capture
reject failed-copied-asset-snapshot TEST_FAIL=copied-snapshot
reject failed-copied-asset-capture TEST_FAIL=copied-capture
reject failed-notice-snapshot TEST_FAIL=notice-snapshot
reject failed-notice-capture TEST_FAIL=notice-capture
reject failed-supplement TEST_FAIL=supplement
reject failed-toolchain TEST_FAIL=toolchain
reject failed-source-license TEST_FAIL=source-license
reject failed-module-retention TEST_FAIL=modules
reject changed-module-cache TEST_FAIL=module-integrity
reject unsupported-metadata-profile TEST_METADATA_PROFILE=other
reject missing-dependency-profile TEST_DEPENDENCY_PROFILE=missing
reject unpatched-dependency-profile TEST_DEPENDENCY_PROFILE=go-jose-4.1.4-v1
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
test -s "$scratch/complete/text-font-notices.txt"
test -s "$scratch/complete/text-font-notices-inventory.json"
test -s "$scratch/complete/copied-asset-notices.txt"
test -s "$scratch/complete/copied-asset-notices-inventory.json"
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
for fault in unsealed-source both-source-modes capture-with-prepared substituted-prepared-profile console-input-only console-seal-only console-duplicate-seal; do
  : >"$TEST_CALLS"
  args=(--build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" --output "$scratch/$fault")
  case "$fault" in
    unsealed-source) args=(--build --prepared-source "$TEST_PREPARED_BUILD" --output "$scratch/$fault");;
    both-source-modes) args+=(--source "$scratch/upstream");;
    capture-with-prepared) args+=(--capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER");;
    console-input-only) args+=(--console-inputs "$scratch/unavailable-capsule");;
    console-seal-only) args+=(--console-sha256 "$prepared_sha");;
    console-duplicate-seal) args+=(--console-inputs "$scratch/unavailable-capsule" --console-sha256 "$prepared_sha" --console-sha256 "$prepared_sha");;
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
jq -e '.sourceSnapshot.preparedProfileSha256 == "0ada792a70475609fd59e746b22dab31f664719cabf86decab1d37d1a207b12d" and .sourceSnapshot.files == 9599 and .sourceSnapshot.records == 28 and .sourceSnapshot.dependencyInputsCaptured == false and .sourceSnapshot.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.pnpmSources.selectedPNPMSourcesCaptured == true and .sourceSnapshot.pnpmSources.packages > 0 and .sourceSnapshot.pnpmSources.sourceManifestSha256 == .sourceSnapshot.manifestSha256 and .sourceSnapshot.pnpmSources.dependencyInputsCaptured == false and .sourceSnapshot.pnpmSources.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.generatorBuildMetadata.tools == 14 and .sourceSnapshot.generatorBuildMetadata.modules > 0 and .sourceSnapshot.generatorBuildMetadata.sourceManifestSha256 == .sourceSnapshot.manifestSha256 and .sourceSnapshot.generatorBuildMetadata.generatorBuildMetadataValidated == true and .sourceSnapshot.generatorBuildMetadata.dependencyInputsCaptured == false and .sourceSnapshot.generatorBuildMetadata.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.generatorGoSources.selectedGeneratorGoSourcesCaptured == true and .sourceSnapshot.generatorGoSources.modules == .sourceSnapshot.generatorBuildMetadata.modules and .sourceSnapshot.generatorGoSources.checkpointSha256 == .sourceSnapshot.generatorBuildMetadata.checkpointSha256 and .sourceSnapshot.generatorGoSources.sourceManifestSha256 == .sourceSnapshot.manifestSha256 and .sourceSnapshot.generatorGoSources.dependencyInputsCaptured == false and .sourceSnapshot.generatorGoSources.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.goModules.modules == 231 and .sourceSnapshot.goModules.files == 466 and .sourceSnapshot.goModules.selectedServiceGoSourcesCaptured == true and .sourceSnapshot.goModules.dependencyInputsCaptured == false and .sourceSnapshot.goModules.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e --arg digest "$(shasum -a 256 "$TEST_MODULE_VERIFIER" | awk '{print $1}')" '.sourceSnapshot.goModuleVerifier == {go:"1.27.3",sha256:$digest}' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.sourceSnapshot.bsrSources.modules == 3 and .sourceSnapshot.bsrSources.files == 170 and .sourceSnapshot.bsrSources.selectedBSRSourcesCaptured == true and .sourceSnapshot.bsrSchemaReplay.schemaOfflineReplayProved == true and .sourceSnapshot.bsrSchemaReplay.noNewPrivilegesReplay == true and .sourceSnapshot.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null
jq -e '.consoleDownloads.originalConsoleDownloadsConsumed == true and .consoleDownloads.requests == 3 and .consoleDownloads.sourceManifestSha256 == .sourceSnapshot.manifestSha256 and .consoleDownloads.dependencyInputsCaptured == false and .consoleDownloads.nativeOfflineReplayProved == false' "$scratch/captured/build-receipt.json" >/dev/null || { echo 'native console consumption was not bound to the captured source' >&2; exit 1; }
: >"$TEST_CALLS"
bash "$build" --build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" \
  --console-inputs "$scratch/captured/selected-console-originals" --console-sha256 "$(printf '%064d' 0 | tr 0 d)" \
  --output "$scratch/prepared-console-replay" >"$scratch/prepared-console-replay.log" 2>&1 || { cat "$scratch/prepared-console-replay.log" >&2; exit 1; }
jq -e --slurpfile original "$scratch/captured/build-receipt.json" '.consoleDownloads == $original[0].consoleDownloads and .sourceInput.manifestSha256 == .consoleDownloads.sourceManifestSha256' "$scratch/prepared-console-replay/build-receipt.json" >/dev/null
: >"$TEST_CALLS"
if bash "$build" --build --prepared-source "$TEST_PREPARED_BUILD" --prepared-sha256 "$prepared_sha" \
  --console-inputs "$scratch/captured/selected-console-originals" --console-sha256 "$(printf '%064d' 0 | tr 0 d)" \
  --output "$scratch/captured/selected-console-originals/nested-build" >"$scratch/nested-console-output.log" 2>&1; then
  echo 'build output was allowed inside the sealed console input' >&2; exit 1
fi
test ! -e "$scratch/captured/selected-console-originals/nested-build"
for failure in graph-metadata-capture graph-metadata-readback graph-metadata-incomplete pnpm-early-check pnpm-early-incomplete generator-checkpoint-changed generator-observation generator-readback generator-incomplete generator-source-capture generator-source-readback generator-source-incomplete pnpm-selection pnpm-capture pnpm-incomplete pnpm-readback pnpm-tool-changed console-capture console-verify console-request-incomplete console-tool-changed console-launcher-changed bsr-copy-changed source-capture source-readback module-source-capture module-source-readback module-source-incomplete bsr-tool-changed bsr-tool-binding bsr-resolution bsr-metadata bsr-export bsr-capture bsr-readback bsr-incomplete bsr-cache bsr-descriptor network-isolation network-leak no-new-privileges; do
  : >"$TEST_CALLS"
  if env TEST_FAIL="$failure" bash "$build" --build --capture-source --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --source "$scratch/upstream" --output "$scratch/$failure" >"$scratch/$failure.log" 2>&1; then echo "failed source capture/readback admitted: $failure" >&2; exit 1; fi
  test ! -e "$scratch/$failure/build-receipt.json"
  if [[ "$failure" = pnpm-early-* ]]; then test ! -e "$scratch/$failure/generate.log"; fi
  if [[ "$failure" = pnpm-tool-changed ]]; then cp "$root/tools/zitadel-pnpm-source/index.mjs" "$scratch/recipe/tools/zitadel-pnpm-source/index.mjs"; fi
done
capture_reject wrong-api-openapi-version "$TEST_MODULE_VERIFIER" TEST_API_OPENAPI_VERSION=v2.22.0
if bash "$build" --build --source "$scratch/upstream" --output "$scratch/complete"; then exit 1; fi
printf 'Linux build orchestration controls passed (process doubles, not upstream-build proof).\n'

# The retained mode must join every independent capsule to the original source
# and enter the same pipeline with fresh restored stores. This remains process
# orchestration proof; only the real Linux job can prove a network-denied build.
retained="$scratch/retained-fixture"
mkdir -p "$retained/source/source-snapshot"
cp -R "$scratch/complete/prepared" "$retained/source/source-snapshot/"
for record in api-project.original.json api-project.serial.json inputs.sha256; do
  cp "$scratch/complete/$record" "$retained/source/source-snapshot/$record"
done
for component in selected-go-sources selected-generator-go-sources go-graph-metadata selected-pnpm-sources selected-bsr-sources selected-console-originals; do
  cp -R "$scratch/captured/$component" "$retained/source/"
done
mkdir -p "$retained/source/selected-go-sources/records"
cp "$scratch/complete/prepared/source/go.mod" "$retained/source/selected-go-sources/records/go.mod"
cp "$scratch/complete/prepared/source/go.sum" "$retained/source/selected-go-sources/records/go.sum"
jq -n '{modules:[{effective:{Path:"example.com/fixture",Version:"v1.0.0",Sum:"h1:fixture",GoModSum:"h1:mod"}}]}' >"$retained/source/selected-go-sources/manifest.json"
cp "$scratch/captured/build-receipt.json" "$retained/source/build-receipt.json"
TEST_PREPARED_BUILD="$retained/source/source-snapshot"
export TEST_PREPARED_BUILD
retained_output="$scratch/retained-replay unsafe,|%#"
export TEST_RETAINED_PROXY_URL="file://$scratch/retained-replay%20unsafe%2C%7C%25%23/go-input-native/go-proxy"
: >"$TEST_CALLS"
if ! bash "$build" --build --retained-inputs "$retained" --retained-sha256 "$prepared_sha" --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --output "$retained_output" >"$scratch/retained-replay.log" 2>&1; then
  cat "$scratch/retained-replay.log" >&2
  echo 'retained inputs cannot enter the existing native pipeline' >&2
  exit 1
fi
jq -e '.sourceInput.manifestSha256 == ("a"*64) and .sourceInput.nativeOfflineReplayProved == false and .retainedInputsRestored == true and .imageAdmission == "unknown" and .licenseClosure == "unknown"' "$retained_output/build-receipt.json" >/dev/null
final_receipt="$(shasum -a 256 "$retained_output/build-receipt.json" | awk '{print $1}')"
for phase in console icons text-fonts copied-assets; do
  [[ "$(cat "$retained_output/fixture-$phase-receipt.sha256")" == "$final_receipt" ]] || { echo "final receipt differs from $phase inventory binding" >&2; exit 1; }
done
test -d "$retained_output/go-input-native/go-modules"
test -d "$retained_output/pnpm-store"
for fault in retained-go retained-pnpm retained-go-remote retained-go-fallback retained-go-scope; do
  : >"$TEST_CALLS"
  if env TEST_FAIL="$fault" bash "$build" --build --retained-inputs "$retained" --retained-sha256 "$prepared_sha" --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --output "$scratch/$fault" >"$scratch/$fault.log" 2>&1; then
    echo "failed retained restoration admitted: $fault" >&2; exit 1
  fi
  test ! -e "$scratch/$fault/generate.log"
  test ! -e "$scratch/$fault/build-receipt.json"
done

for fault in retained-go-consumption retained-go-consumption-partial; do
  if env TEST_FAIL="$fault" bash "$build" --build --retained-inputs "$retained" --retained-sha256 "$prepared_sha" --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --output "$scratch/$fault" >"$scratch/$fault.log" 2>&1; then
    echo "incomplete native service consumption admitted: $fault" >&2; exit 1
  fi
  test ! -e "$scratch/$fault/retained-go-service-readback.json"
  test ! -e "$scratch/$fault/build-receipt.json"
done
if env TEST_NATIVE_SUM=h1:changed bash "$build" --build --retained-inputs "$retained" --retained-sha256 "$prepared_sha" --module-source-verifier "$TEST_MODULE_VERIFIER" --bsr-source-verifier "$TEST_BSR_VERIFIER" --output "$scratch/retained-go-consumption-identity" >"$scratch/retained-go-consumption-identity.log" 2>&1; then
  echo 'changed native service checksum admitted' >&2; exit 1
fi
test ! -e "$scratch/retained-go-consumption-identity/build-receipt.json"

# Exercise the actual receipt footer's I/O boundary without pretending to run
# its native build prerequisites. Linux CI separately executes the full entrypoint.
receipt_anchor="go run \"\$root/scripts/zitadel-source-bundle.go\" --verify \"\$output/roundtrip\" \"\$seal\""
[[ "$(grep -cF "$receipt_anchor" "$root/scripts/replay-zitadel-inputs.sh")" = 1 ]]
cat >"$scratch/receipt-footer.sh" <<'PREFIX'
set -euo pipefail
output="$1"
native="$output/native.json"
seal="$(printf 'a%.0s' {1..64})"
archive="$(printf 'b%.0s' {1..64})"
complete=0
PREFIX
awk -v anchor="$receipt_anchor" 'found {print} index($0,anchor)==1 {found=1}' "$root/scripts/replay-zitadel-inputs.sh" >>"$scratch/receipt-footer.sh"
for control in complete jq-partial hash-partial; do
  receipt_output="$scratch/receipt-$control"
  mkdir -p "$receipt_output/native" "$receipt_output/bin"
  for notice in console-notices-inventory.json console-notices-supplement-inventory.json login-icons-inventory.json text-font-notices-inventory.json copied-asset-notices-inventory.json; do
    printf 'fictional inventory\n' >"$receipt_output/native/$notice"
  done
  jq -n '{sourceInput:{manifestSha256:("a"*64)},recipeSha256:("c"*64),binarySha256:("d"*64)}' >"$receipt_output/native.json"
  case "$control" in
    jq-partial)
      printf '#!/usr/bin/env bash\nprintf "{\\\"schemaVersion\\\":1"\nexit 42\n' >"$receipt_output/bin/jq"
      chmod 700 "$receipt_output/bin/jq" ;;
    hash-partial)
      printf '#!/usr/bin/env bash\nprintf "a%%.0s" {1..64}\nprintf "  fictional\\n"\nexit 42\n' >"$receipt_output/bin/shasum"
      chmod 700 "$receipt_output/bin/shasum" ;;
  esac
  status=0
  PATH="$receipt_output/bin:$PATH" bash "$scratch/receipt-footer.sh" "$receipt_output" >"$receipt_output/log" 2>&1 || status=$?
  if [[ "$control" = complete ]]; then
    [[ "$status" = 0 ]]
    jq -e '.nativeOfflineReplayProved==true and .dependencyInputsCaptured==true and (.noticeInventories.textFonts|test("^[0-9a-f]{64}$")) and (.noticeInventories.copiedAssets|test("^[0-9a-f]{64}$"))' "$receipt_output/replay-receipt.json" >/dev/null
  else
    [[ "$status" = 42 && ! -e "$receipt_output/replay-receipt.json" ]]
  fi
done
printf 'Receipt I/O controls: actual footer success and both partial-failure refusals passed.\n'
