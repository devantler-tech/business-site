#!/usr/bin/env bash
#
# npm-toolchain.test.sh — every workflow job that installs, audits or builds the site uses
# the npm major that writes package-lock.json (monorepo#3749).
#
# WHY: Dependabot writes the lockfile with npm 11, its default for a version-3 lockfile.
# npm 10 and npm 11 disagree about which optional peer entries a lockfile must hold, so a
# lockfile one major writes fails the other major's `npm ci` sync check, and every
# dependency update of the site went red. package.json declares the npm major in
# devEngines.packageManager, and npm refuses to install, audit or run with another major.
# This test keeps the workflows on a Node line that bundles that major, so no workflow can
# drift back unnoticed — including the publish workflow, which runs only on main.
#
# CHECKS
#   1. package.json declares devEngines.packageManager as npm "^<major>.0.0" with
#      onFail "error".
#   2. Every job in .github/workflows that invokes npm/npx or the audit wrapper
#      (see jobs_query) has exactly one actions/setup-node step before that invocation, with an explicit
#      root .node-version selecting a Node line whose bundled npm is that major. The known jobs must all be
#      found, so an empty discovery cannot pass.
#   3. The build first proves npm 11.4.2 can clean-install the lockfile, then installs
#      with the runner's current npm 11. Both installs are unconditional and propagate
#      failures. Optional peers can differ within the same major.
#   4. CI runs this test from a job that its own inputs gate, and that gate covers every
#      workflow, so a new workflow that uses the toolchain reruns it.
#
# Fixture cases first prove each check rejects the drift it exists for.
set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(CDPATH='' cd -- "${script_dir}/.." && pwd -P)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

fail() {
  printf 'docs npm toolchain: FAIL - %s\n' "$*" >&2
  exit 1
}

command -v yq >/dev/null 2>&1 || fail "yq is required to read the workflows"
command -v jq >/dev/null 2>&1 || fail "jq is required to read package.json"

# The npm major each Node release line bundles, from the Node.js release notes. An unlisted
# line fails closed: confirm the npm major it bundles before adding it here.
bundled_npm_major() {
  case "$1" in
    22) printf '10\n' ;;
    24) printf '11\n' ;;
    *) return 1 ;;
  esac
}

# Jobs that must be discovered, as <workflow file>:<job id>.
required_jobs=(
  ci.yaml:build-docs
  ci.yaml:audit-docs
  ci.yaml:drift-check-active-projects
  audit-docs.yaml:audit-docs
  publish-site.yaml:build
)

# Paths the CI filter gating this test must list, so a change to any input reruns it.
required_filter_paths=(
  .node-version
  package.json
  web/package.json
  web/package-lock.json
  .github/dependabot.yaml
  scripts/npm-toolchain.test.sh
  '.github/workflows/**'
)

# One row per job that uses the toolchain: job id, setup-node step count, node-version and
# node-version-file of the first setup-node step, and whether that step comes before the
# job's first toolchain step. "-" stands for an absent value, because `read` collapses empty
# tab-separated fields. It is a jq program over yq's JSON rendering of the workflow: the
# runner's yq is older than this host's and rejects parts of the same program in yq syntax.
# A job works in docs/ when its own or the workflow's default working directory is docs/, a
# step's working directory is, or a step enters it with cd, pushd or npm --prefix. Any path
# with a docs component counts (./docs, ${{ github.workspace }}/docs), quoted or not. A step
# that runs npm or npx anywhere counts too, except the closed, independently
# checksum-bound native identity SDK profile below. Any website/unknown command
# in that same job still counts. Audit wrapper invocations count
# from the root too. Legacy docs paths remain detectable, but a root working-directory
# default alone must not turn every Bash-only job into a Node job.
jobs_query=""
IFS= read -r -d '' jobs_query <<'JQ' || true
def docs_dir: test("(^|/)docs(/|$)");
def enters_docs:
  test("(^|[\\s;&|(])(cd|pushd)\\s+([^;&|\\n]*[/\"'\\s])?docs([\"'/\\s;&|)]|$)")
  or test("--prefix[ =]([^\\s;&|]*[/\"'])?docs([\"'/\\s;&|)]|$)");
def runs_npm: test("(^|[\\s;&|(])(npm|npx|node)(\\s|$)");
def runs_audit: test("(^|[/\\s;&|(])audit-dependencies(\\.test)?\\.sh([\\s;&|)]|$)");
def mandatory: .if == null and (.["continue-on-error"] == null or .["continue-on-error"] == false);
# This exact bootstrap, not a job/step name or marker, admits the SDK's own
# older toolchain. It cannot admit a different download, skipped checksum, root
# npm command, conditional setup or setup that runs after the SDK command.
def native_sdk_setup:
  ((.run // "") | rtrimstr("\n")) == ([
    "tools=\"$RUNNER_TEMP/zitadel-tools\"",
    "mkdir \"$tools\"",
    "curl --fail --location --silent --show-error --max-time 120 https://nodejs.org/dist/v22.23.3/node-v22.23.3-linux-x64.tar.xz -o \"$tools/node.tar.xz\"",
    "(cd \"$tools\" && printf '%s\\n' 'df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de  node.tar.xz' | shasum -a 256 -c -)",
    "tar -xJf \"$tools/node.tar.xz\" -C \"$tools\"",
    "export PATH=\"$tools/node-v22.23.3-linux-x64/bin:$PATH\"",
    "corepack enable",
    "corepack prepare pnpm@10.30.3 --activate",
    "echo \"$tools/node-v22.23.3-linux-x64/bin\" >> \"$GITHUB_PATH\""
  ] | join("\n")) and mandatory;
def without_native_sdk_commands:
  split("\n") | map(select((gsub("^\\s+|\\s+$"; "") |
    . == "npm ci --prefix tools/zitadel-pnpm-source --ignore-scripts" or
    . == "node --test tools/zitadel-pnpm-source/index.test.mjs" or
    . == "node tools/zitadel-pnpm-source/index.mjs restore --capsule \"$RUNNER_TEMP/zitadel-pnpm-sources-roundtrip\" --sha256 \"$seal\" --scratch \"$RUNNER_TEMP/zitadel-pnpm-restore-work\" --store \"$RUNNER_TEMP/zitadel-pnpm-restored-store\" > \"$RUNNER_TEMP/zitadel-build/pnpm-source-roundtrip.json\"") | not)) | join("\n");
(((.defaults // {}).run // {})["working-directory"]) as $workflow_default |
(.jobs // {}) | to_entries[] |
(((((.value.defaults // {}).run // {})["working-directory"]) // $workflow_default // "") | docs_dir)
  as $default_docs |
(.value.steps // []) as $steps |
[range(0; $steps | length) | select($steps[.] | native_sdk_setup)] as $native |
[range(0; $steps | length) | select(
  (($steps[.]["working-directory"] // "") | docs_dir) or
  (($steps[.].run // "") | enters_docs) or
  (. as $index | ($steps[$index].run // "") |
    (if ($native | length) == 1 and $native[0] < $index and ($steps[$index] | mandatory)
      then without_native_sdk_commands else . end) | runs_npm) or
  (($steps[.].run // "") | runs_audit) or
  ($default_docs and ($steps[.].run != null))
)] as $docs |
[range(0; $steps | length) | select(($steps[.].uses // "") | test("^actions/setup-node@"))] as $setup |
select($default_docs or ($docs | length > 0) or ($setup | length > 0)) |
[.key, ($setup | length | tostring),
 (if ($setup | length) > 0 then ($steps[$setup[0]].with["node-version"] // "-" | tostring) else "-" end),
 (if ($setup | length) > 0 then ($steps[$setup[0]].with["node-version-file"] // "-" | tostring) else "-" end),
 (if ($setup | length) > 0 and ($docs | length) > 0 then ($setup[0] < $docs[0]) else true end | tostring),
 (if ($setup | length) > 0 then ($steps[$setup[0]] | .if == null and
   (.["continue-on-error"] == null or .["continue-on-error"] == false)) else false end | tostring),
 (if ($native | length) == 1 and ($docs | length) > 0 and ($setup | length) > 0
   then $setup[0] > $native[0] else true end | tostring)]
| @tsv
JQ

violation() {
  printf '%s\n' "$*"
  return 1
}

# check <package.json> <workflows dir>: prints the first violation and returns 1.
check() {
  local package_json="$1" workflows_dir="$2"
  local pm_type name version on_fail declared_major

  pm_type="$(jq -r '.devEngines.packageManager | type' "${package_json}")" ||
    violation "cannot parse ${package_json}" || return 1
  [ "${pm_type}" = "object" ] ||
    violation "package.json declares no devEngines.packageManager entry for npm" || return 1
  name="$(jq -r '.devEngines.packageManager.name // ""' "${package_json}")"
  version="$(jq -r '.devEngines.packageManager.version // ""' "${package_json}")"
  on_fail="$(jq -r '.devEngines.packageManager.onFail // ""' "${package_json}")"
  [ "${name}" = "npm" ] ||
    violation "devEngines.packageManager names '${name}', not npm" || return 1
  [ "${on_fail}" = "error" ] ||
    violation "devEngines.packageManager onFail is '${on_fail}'; it must be 'error' so another npm major stops before it reads the lockfile" ||
    return 1
  [[ "${version}" =~ ^\^([1-9][0-9]*)\.0\.0$ ]] ||
    violation "devEngines.packageManager version '${version}' is not one npm major written as ^<major>.0.0" ||
    return 1
  declared_major="${BASH_REMATCH[1]}"

  local root node_line engine package types
  root="$(dirname "${package_json}")"
  node_line="$(cat "${root}/.node-version" 2>/dev/null)" ||
    violation "cannot read .node-version" || return 1
  [[ "${node_line}" =~ ^[1-9][0-9]*$ ]] ||
    violation ".node-version must declare one numeric Node major" || return 1
  for package in package.json web/package.json; do
    engine="$(jq -er '.engines.node' "${root}/${package}")" ||
      violation "cannot read ${package} engines.node" || return 1
    [ "${engine}" = "^${node_line}" ] ||
      violation "${package} engines.node must match ^${node_line}" || return 1
  done
  types="$(jq -er '.devDependencies["@types/node"]' "${root}/web/package.json")" || return 1
  [[ "${types}" == "${node_line}."* ]] ||
    violation "@types/node must match Node ${node_line}" || return 1
  local lock policy
  lock="$(jq -er '.packages["node_modules/@types/node"].version' "${root}/web/package-lock.json")" ||
    violation "cannot read locked @types/node" || return 1
  [ "${lock}" = "${types}" ] || violation "locked @types/node must match its manifest" || return 1
  policy="$(yq -o=json '.' "${root}/.github/dependabot.yaml" | jq -r '
    [.updates[] | select(."package-ecosystem" == "npm" and .directory == "/web")] as $web |
    ($web | length) == 1 and
    any($web[0].ignore[]?; ."dependency-name" == "@types/node" and
      ."update-types" == ["version-update:semver-major"] and .versions == null)
  ')" || violation "cannot read Node types update policy" || return 1
  [ "${policy}" = true ] || violation "Node types major updates must be coordinated with the runtime" || return 1

  local workflow base json rows job count node_version node_version_file ordered mandatory native_authority line npm_major
  local found=" "
  for workflow in "${workflows_dir}"/*.yaml "${workflows_dir}"/*.yml; do
    [ -f "${workflow}" ] || continue
    base="$(basename "${workflow}")"
    json="$(yq -o=json '.' "${workflow}")" || violation "cannot parse ${base}" || return 1
    rows="$(jq -r "${jobs_query}" <<<"${json}")" || violation "cannot read the jobs of ${base}" || return 1
    while IFS=$'\t' read -r job count node_version node_version_file ordered mandatory native_authority; do
      [ -n "${job}" ] || continue
      found="${found}${base}:${job} "
      [ "${count}" = "1" ] ||
        violation "${base}:${job} works in docs/ but has ${count} actions/setup-node steps; it needs exactly one" ||
        return 1
      [ "${native_authority}" = "true" ] ||
        violation "${base}:${job} website Node setup must follow the native toolchain override" || return 1
      [ "${ordered}" = "true" ] ||
        violation "${base}:${job} sets up Node after its first step in docs/, so that step runs on the runner's default Node" ||
        return 1
      line="${node_line}"
      if [ "${node_version}" != "-" ]; then
        [[ "${node_version}" =~ ^v?([0-9]+)(\.|$) ]] ||
          violation "${base}:${job} node-version '${node_version}' does not name a Node release line" || return 1
        line="${BASH_REMATCH[1]}"
      fi
      npm_major="$(bundled_npm_major "${line}")" ||
        violation "${base}:${job} uses Node ${line}, whose bundled npm major this test does not know yet" ||
        return 1
      [ "${npm_major}" = "${declared_major}" ] ||
        violation "${base}:${job} sets up Node ${node_version}, which bundles npm ${npm_major}; package.json declares npm ${declared_major}" ||
        return 1
      [ "${node_version_file}" = ".node-version" ] && [ "${node_version}" = "-" ] ||
        violation "${base}:${job} must read the root .node-version without an overriding node-version" || return 1
      [ "${mandatory}" = true ] ||
        violation "${base}:${job} Node setup must run and propagate failures" || return 1
    done <<<"${rows}"
  done

  local required
  for required in "${required_jobs[@]}"; do
    [[ "${found}" == *" ${required} "* ]] ||
      violation "${required} was not found as a job that works in docs/" || return 1
  done

  local ci="${workflows_dir}/ci.yaml" gate filter compatibility
  compatibility="$(yq -o=json '.' "${ci}" | jq -r '
    .jobs["build-docs"].steps as $steps |
    [range(0; $steps | length) | select(
      $steps[.]["working-directory"] == "." and
      $steps[.].run == "npx --yes --package=npm@11.4.2 npm ci --ignore-scripts"
    )] as $older |
    [range(0; $steps | length) | select(
      $steps[.]["working-directory"] == "." and $steps[.].run == "npm ci"
    )] as $current |
    ($older | length) == 1 and ($current | length) == 1 and $older[0] < $current[0] and
    all(($older + $current)[]; $steps[.] | .if == null and
      (.["continue-on-error"] == null or .["continue-on-error"] == false))
  ')" || violation "cannot read the lockfile compatibility installs" || return 1
  [ "${compatibility}" = "true" ] ||
    violation "ci.yaml:build-docs must clean-install with npm 11.4.2 before its current npm ci" || return 1
  # This Bash-only contract needs no Node; an application-root default alone must
  # not make its own job require setup-node.
  gate="$(yq -r '
    [.jobs | to_entries[] | select([.value.steps[]? | select(
      .run == "bash scripts/npm-toolchain.test.sh"
    )] | length > 0) | .value.if // ""] | .[0] // ""
  ' "${ci}")" || violation "cannot parse ci.yaml" || return 1
  # The whole condition, exactly: an inverted or narrowed gate would skip the test.
  [[ "${gate}" =~ ^needs\.changes\.outputs\.([a-z0-9-]+)\ ==\ \'true\'$ ]] ||
    violation "ci.yaml has no job that runs bash scripts/npm-toolchain.test.sh behind exactly needs.changes.outputs.<filter> == 'true' (found: '${gate}')" ||
    return 1
  filter="${BASH_REMATCH[1]}"
  # Compared in bash, literally: yq's == treats a `*` in its right-hand string as a glob, so
  # '.github/workflows/**' would match any single workflow entry.
  local entries path
  entries="$(yq -o=json '.jobs.changes.steps[] | select(.id == "filter") | .with.filters | from_yaml' "${ci}" |
    jq -r --arg filter "${filter}" ".[\$filter] // [] | .[]")" ||
    violation "cannot read ci.yaml filter '${filter}'" || return 1
  for path in "${required_filter_paths[@]}"; do
    grep -qxF -- "${path}" <<<"${entries}" ||
      violation "ci.yaml filter '${filter}' does not list ${path}, so a change to it skips this test" ||
      return 1
  done
}

# expect_failure <case> <expected message fragment>: runs check on the fixture copy.
expect_failure() {
  local name="$1" fragment="$2" output status=0
  output="$(check "${fixture}/package.json" "${fixture}/.github/workflows")" || status=$?
  [ "${status}" -ne 0 ] || fail "fixture '${name}' passed; the check must reject it"
  [[ "${output}" == *"${fragment}"* ]] ||
    fail "fixture '${name}' failed for another reason: ${output}"
}

# reset_fixture: a fresh copy of the real package.json and workflows.
reset_fixture() {
  fixture="${tmp_dir}/fixture"
  rm -rf "${fixture}"
  mkdir -p "${fixture}/docs" "${fixture}/.github" "${fixture}/web"
  cp "${repo_root}/package.json" "${fixture}/package.json"
  cp "${repo_root}/web/package.json" "${fixture}/web/package.json"
  cp "${repo_root}/web/package-lock.json" "${fixture}/web/package-lock.json"
  cp "${repo_root}/.node-version" "${fixture}/.node-version"
  cp "${repo_root}/.github/dependabot.yaml" "${fixture}/.github/dependabot.yaml"
  cp -R "${repo_root}/.github/workflows" "${fixture}/.github/workflows"
}

output="$(check "${repo_root}/package.json" "${repo_root}/.github/workflows")" || fail "${output}"

reset_fixture
yq -i '.jobs["build-zitadel-linux"].steps =
  [{"uses":"actions/setup-node@fixture","with":{"node-version-file":".node-version"}}] +
  .jobs["build-zitadel-linux"].steps + [{"run":"npm ci"}]' "${fixture}/.github/workflows/ci.yaml"
expect_failure "native bootstrap overrides an earlier website Node setup" "website Node setup must follow"

# The frozen native SDK has its own Node authority. Recognize only its exact
# commands after the unconditional checksum-verified bootstrap. Website npm and
# unknown/mixed commands in that same job must still require the site toolchain.
for mutation in \
  '.jobs["build-zitadel-linux"].steps += [{"run":"npm ci"}]' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .run += "npm ci\n" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .run=(.run | sub("--ignore-scripts"; "--ignore-scripts; npm ci")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .run=(.run | sub("--ignore-scripts"; "--ignore-scripts \u0024(npm ci)")) else . end)' \
  '.jobs["build-zitadel-linux"].steps += [{"run":"node scripts/unknown.mjs"}]' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .run=(.run | sub("tools/zitadel-pnpm-source"; "tools/unrecognized")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Prepare the frozen identity JavaScript toolchain" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Prepare the frozen identity JavaScript toolchain" then .["continue-on-error"]=true else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Prepare the frozen identity JavaScript toolchain" then .run=(.run | sub("df450af8"; "00000000")) else . end)' \
  '.jobs["build-zitadel-linux"].steps |= map(select(.name != "Prepare the frozen identity JavaScript toolchain"))' \
  '.jobs["build-zitadel-linux"].steps |= map(if .name == "Verify native pnpm source retention" then .if="false" else . end)' \
  '.jobs["build-zitadel-linux"].steps |= sort_by(if .name == "Verify native pnpm source retention" then 0 else 1 end)'; do
  reset_fixture
  yq -o=json '.' "${fixture}/.github/workflows/ci.yaml" | jq "$mutation" >"${fixture}/native-broken.json"
  yq -P '.' "${fixture}/native-broken.json" >"${fixture}/.github/workflows/ci.yaml"
  expect_failure "native SDK must not hide a website/unchecked toolchain command" "ci.yaml:build-zitadel-linux works in docs/ but has 0"
done

# A version-file consumer must fail closed on absent or conflicting authority,
# and type checking must not admit APIs newer than the selected runtime.
reset_fixture
rm -f "${fixture}/.node-version"
expect_failure "missing Node declaration" "cannot read .node-version"

reset_fixture
printf 'latest\n' >"${fixture}/.node-version"
expect_failure "floating Node declaration" "must declare one numeric Node major"

reset_fixture
jq '.engines.node = ">=22.18.0"' "${fixture}/package.json" >"${fixture}/package-broken.json"
mv "${fixture}/package-broken.json" "${fixture}/package.json"
expect_failure "root engine admits another line" "package.json engines.node"

reset_fixture
jq '.engines.node = "^26"' "${fixture}/web/package.json" >"${fixture}/web/package-broken.json"
mv "${fixture}/web/package-broken.json" "${fixture}/web/package.json"
expect_failure "candidate engine drifts" "web/package.json engines.node"

reset_fixture
jq '.devDependencies["@types/node"] = "26.6.5"' "${fixture}/web/package.json" >"${fixture}/web/package-broken.json"
mv "${fixture}/web/package-broken.json" "${fixture}/web/package.json"
expect_failure "types admit newer APIs" "@types/node must match Node"

reset_fixture
jq '.packages["node_modules/@types/node"].version = "26.6.5"' "${fixture}/web/package-lock.json" >"${fixture}/web/lock-broken.json"
mv "${fixture}/web/lock-broken.json" "${fixture}/web/package-lock.json"
expect_failure "lockfile types drift" "locked @types/node must match"

reset_fixture
yq -i 'del(.updates[] | select(.directory == "/web") | .ignore)' "${fixture}/.github/dependabot.yaml"
expect_failure "uncoordinated Node types major update" "Node types major updates must be coordinated"

reset_fixture
yq -i '(.jobs.build.steps[] | select((.uses // "") | test("^actions/setup-node@")) | .with."node-version-file") = "web/.node-version"' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "second Node authority" "must read the root .node-version"

reset_fixture
yq -i '(.jobs.build.steps[] | select((.uses // "") | test("^actions/setup-node@")) | .with."node-version") = "24"' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "inline version overrides declaration" "must read the root .node-version"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@fixture", "with": {"node-version": "24"}},
  {"run": "node --test tests/example.mjs"}]}' "${fixture}/.github/workflows/ci.yaml"
expect_failure "Node-only job also reads declaration" "must read the root .node-version"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"run": "node --test tests/example.mjs"}]}' "${fixture}/.github/workflows/ci.yaml"
expect_failure "Node-only job missing setup" "ci.yaml:extra works in docs/ but has 0"

for control in '.if = "false"' '.["continue-on-error"] = true'; do
  reset_fixture
  yq -o=json '.' "${fixture}/.github/workflows/publish-site.yaml" |
    jq "(.jobs.build.steps[] | select((.uses // \"\") | startswith(\"actions/setup-node@\"))) |= (${control})" >"${fixture}/conditional-node.json"
  yq -P '.' "${fixture}/conditional-node.json" >"${fixture}/.github/workflows/publish-site.yaml"
  expect_failure "Node setup cannot be skipped: ${control}" "Node setup must run and propagate failures"
done

# A root-native application still audits through its wrapper, not a literal npm
# command. Discover that invocation even when a workflow inherits the root.
reset_fixture
yq -i '.defaults.run.working-directory = "." | .jobs.extra = {
  "runs-on": "ubuntu-latest",
  "steps": [
    {"uses": "actions/setup-node@fixture", "with": {"node-version": "22"}},
    {"run": "./scripts/audit-dependencies.sh"}
  ]
}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "root-default audit wrapper" "publish-site.yaml:extra sets up Node 22"

# Root defaults alone must not classify every Bash-only repository check as a
# Node job. The wrapper/npm invocation, not the application's folder name, matters.
reset_fixture
yq -i '.defaults.run.working-directory = "." | .jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [{"run": "bash scripts/editorial-contract.test.sh"}]
}' "${fixture}/.github/workflows/publish-site.yaml"
check "${fixture}/package.json" "${fixture}/.github/workflows" ||
  fail "root-default Bash-only jobs must not require Node"

reset_fixture
yq -i '.jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [
    {"run": "./scripts/audit-dependencies.sh", "working-directory": "."},
    {"uses": "actions/setup-node@fixture", "with": {"node-version": "24"}}
  ]
}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "root audit before Node setup" "publish-site.yaml:extra sets up Node after"

reset_fixture
yq -i '(.jobs.build.steps[] | select((.uses // "") | test("^actions/setup-node@")) | .with."node-version") = "22"' \
  "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "publish on Node 22" "publish-site.yaml:build sets up Node 22, which bundles npm 10"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "cd docs && ./scripts/audit-dependencies.sh"}]}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "new job entering docs/ with cd" "publish-site.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "pushd \"docs\" && ./scripts/audit-dependencies.sh"}]}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "new job entering docs/ with a quoted pushd" "publish-site.yaml:extra sets up Node 22"

reset_fixture
CD_STEP="cd \"\${GITHUB_WORKSPACE}\"/docs && ./scripts/audit-dependencies.sh" yq -i '.jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": strenv(CD_STEP)}]}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "new job entering docs/ through a quoted variable" "publish-site.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "npx astro build"}]}' "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "new job running npx" "publish-site.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.test-docs-npm-toolchain.if = "needs.changes.outputs.docs-npm-toolchain != '"'"'true'"'"'"' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "inverted gate" "behind exactly needs.changes.outputs.<filter> == 'true'"

reset_fixture
WORKSPACE_DOCS="\${{ github.workspace }}/docs" yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "npm ci", "working-directory": strenv(WORKSPACE_DOCS)}]}' \
  "${fixture}/.github/workflows/publish-site.yaml"
expect_failure "new job in docs/ by workspace path" "publish-site.yaml:extra sets up Node 22"

reset_fixture
cat >"${fixture}/.github/workflows/extra.yaml" <<'YAML'
on: push
defaults:
  run:
    working-directory: .
jobs:
  install:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-node@v7
        with:
          node-version: "22"
      - run: npm ci
YAML
expect_failure "workflow-wide docs/ default" "extra.yaml:install sets up Node 22"

reset_fixture
yq -i '(.jobs.audit-docs.steps[] | select((.uses // "") | test("^actions/setup-node@")) | .with."node-version") = "23"' \
  "${fixture}/.github/workflows/audit-docs.yaml"
expect_failure "unknown Node line" "audit-docs.yaml:audit-docs uses Node 23"

reset_fixture
yq -i 'del(.jobs.drift-check-active-projects.steps[] | select((.uses // "") | test("^actions/setup-node@")))' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "job without setup-node" "ci.yaml:drift-check-active-projects works in docs/ but has 0"

reset_fixture
yq -i 'del(.jobs.build-docs.steps[] | select((.uses // "") | test("^actions/setup-node@")))' \
  "${fixture}/.github/workflows/ci.yaml"
yq -i '.jobs.build-docs.steps += [{"uses": "actions/setup-node@v7", "with": {"node-version": "24"}}]' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "setup-node after npm ci" "ci.yaml:build-docs sets up Node after its first step in docs/"

reset_fixture
yq -i 'del(.jobs.build-docs)' "${fixture}/.github/workflows/ci.yaml"
expect_failure "job not discovered" "ci.yaml:build-docs was not found"

reset_fixture
yq -i 'del(.jobs.build-docs.steps[] | select(.run == "npx --yes --package=npm@11.4.2 npm ci --ignore-scripts"))' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "missing older npm clean install" "must clean-install with npm 11.4.2 before its current npm ci"

reset_fixture
yq -i '(.jobs.build-docs.steps[] | select(.run == "npx --yes --package=npm@11.4.2 npm ci --ignore-scripts") | .run) = "npx --yes --package=npm@11.4.2 npm install --ignore-scripts"' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "install hides a lockfile mismatch" "must clean-install with npm 11.4.2 before its current npm ci"

reset_fixture
yq -i '.jobs.build-docs.steps |= sort_by(.run // "")' "${fixture}/.github/workflows/ci.yaml"
expect_failure "compatibility install moved after current install" "must clean-install with npm 11.4.2 before its current npm ci"

# Both installs must run and propagate errors; a conditional or error-tolerant
# install cannot establish that the committed lockfile is clean-installable.
install_guard_failures=0
for install_run in "npx --yes --package=npm@11.4.2 npm ci --ignore-scripts" "npm ci"; do
  for broken_install in '.if = "false"' '.["continue-on-error"] = true' ".[\"continue-on-error\"] = \"\${{ always() }}\""; do
    reset_fixture
    yq -o=json '.' "${fixture}/.github/workflows/ci.yaml" |
      jq --arg run "${install_run}" ".jobs[\"build-docs\"].steps |= map(if .run == \$run then ${broken_install} else . end)" >"${fixture}/ci-broken.json"
    yq -P '.' "${fixture}/ci-broken.json" >"${fixture}/.github/workflows/ci.yaml"
    if ! (expect_failure "${install_run}: ${broken_install}" "must clean-install with npm 11.4.2 before its current npm ci"); then
      install_guard_failures=$((install_guard_failures + 1))
    fi
  done
done
[ "${install_guard_failures}" -eq 0 ] || fail "${install_guard_failures} invalid install controls were accepted"

reset_fixture
yq -i '(.jobs.build-docs.steps[] | select(.run == "npx --yes --package=npm@11.4.2 npm ci --ignore-scripts" or .run == "npm ci")).continue-on-error = false' "${fixture}/.github/workflows/ci.yaml"
check "${fixture}/package.json" "${fixture}/.github/workflows" || fail "explicit continue-on-error: false must preserve mandatory installs"

reset_fixture
jq 'del(.devEngines)' "${repo_root}/package.json" >"${fixture}/package.json"
expect_failure "no devEngines" "declares no devEngines.packageManager"

reset_fixture
jq '.devEngines.packageManager.onFail = "warn"' "${repo_root}/package.json" >"${fixture}/package.json"
expect_failure "onFail warn" "onFail is 'warn'"

reset_fixture
yq -i '(.jobs.changes.steps[] | select(.id == "filter") | .with.filters) |= (from_yaml | .docs-npm-toolchain -= [".github/workflows/**"] | to_yaml)' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "filter misses new workflows" "does not list .github/workflows/**"

printf 'docs npm toolchain: PASS\n'
