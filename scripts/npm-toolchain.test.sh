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
#      node-version on a Node line whose bundled npm is that major. The known jobs must all be
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
  publish-pages.yaml:build
  publish-site.yaml:build
)

# Paths the CI filter gating this test must list, so a change to any input reruns it.
required_filter_paths=(
  package.json
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
# that runs npm or npx anywhere counts too: npm is the tool whose major matters, so naming it
# does not depend on spotting how a step reaches docs/. Audit wrapper invocations count
# from the root too. Legacy docs paths remain detectable, but a root working-directory
# default alone must not turn every Bash-only job into a Node job.
jobs_query=""
IFS= read -r -d '' jobs_query <<'JQ' || true
def docs_dir: test("(^|/)docs(/|$)");
def enters_docs:
  test("(^|[\\s;&|(])(cd|pushd)\\s+([^;&|\\n]*[/\"'\\s])?docs([\"'/\\s;&|)]|$)")
  or test("--prefix[ =]([^\\s;&|]*[/\"'])?docs([\"'/\\s;&|)]|$)");
def runs_npm: test("(^|[\\s;&|(])(npm|npx)(\\s|$)");
def runs_audit: test("(^|[/\\s;&|(])audit-dependencies(\\.test)?\\.sh([\\s;&|)]|$)");
(((.defaults // {}).run // {})["working-directory"]) as $workflow_default |
(.jobs // {}) | to_entries[] |
(((((.value.defaults // {}).run // {})["working-directory"]) // $workflow_default // "") | docs_dir)
  as $default_docs |
(.value.steps // []) as $steps |
[range(0; $steps | length) | select(
  (($steps[.]["working-directory"] // "") | docs_dir) or
  (($steps[.].run // "") | enters_docs) or
  (($steps[.].run // "") | runs_npm) or
  (($steps[.].run // "") | runs_audit) or
  ($default_docs and ($steps[.].run != null))
)] as $docs |
select($default_docs or ($docs | length > 0)) |
[range(0; $steps | length) | select(($steps[.].uses // "") | test("^actions/setup-node@"))] as $setup |
[.key, ($setup | length | tostring),
 (if ($setup | length) > 0 then ($steps[$setup[0]].with["node-version"] // "-" | tostring) else "-" end),
 (if ($setup | length) > 0 then ($steps[$setup[0]].with["node-version-file"] // "-" | tostring) else "-" end),
 (if ($setup | length) > 0 and ($docs | length) > 0 then ($setup[0] < $docs[0]) else true end | tostring)]
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

  local workflow base json rows job count node_version node_version_file ordered line npm_major
  local found=" "
  for workflow in "${workflows_dir}"/*.yaml "${workflows_dir}"/*.yml; do
    [ -f "${workflow}" ] || continue
    base="$(basename "${workflow}")"
    json="$(yq -o=json '.' "${workflow}")" || violation "cannot parse ${base}" || return 1
    rows="$(jq -r "${jobs_query}" <<<"${json}")" || violation "cannot read the jobs of ${base}" || return 1
    while IFS=$'\t' read -r job count node_version node_version_file ordered; do
      [ -n "${job}" ] || continue
      found="${found}${base}:${job} "
      [ "${count}" = "1" ] ||
        violation "${base}:${job} works in docs/ but has ${count} actions/setup-node steps; it needs exactly one" ||
        return 1
      [ "${ordered}" = "true" ] ||
        violation "${base}:${job} sets up Node after its first step in docs/, so that step runs on the runner's default Node" ||
        return 1
      [ "${node_version_file}" = "-" ] && [ "${node_version}" != "-" ] ||
        violation "${base}:${job} must pin node-version explicitly, so the npm major it installs with is reviewable" ||
        return 1
      [[ "${node_version}" =~ ^v?([0-9]+)(\.|$) ]] ||
        violation "${base}:${job} node-version '${node_version}' does not name a Node release line" || return 1
      line="${BASH_REMATCH[1]}"
      npm_major="$(bundled_npm_major "${line}")" ||
        violation "${base}:${job} uses Node ${line}, whose bundled npm major this test does not know yet" ||
        return 1
      [ "${npm_major}" = "${declared_major}" ] ||
        violation "${base}:${job} sets up Node ${node_version}, which bundles npm ${npm_major}; package.json declares npm ${declared_major}" ||
        return 1
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
  mkdir -p "${fixture}/docs" "${fixture}/.github"
  cp "${repo_root}/package.json" "${fixture}/package.json"
  cp -R "${repo_root}/.github/workflows" "${fixture}/.github/workflows"
}

output="$(check "${repo_root}/package.json" "${repo_root}/.github/workflows")" || fail "${output}"

# A root-native application still audits through its wrapper, not a literal npm
# command. Discover that invocation even when a workflow inherits the root.
reset_fixture
yq -i '.defaults.run.working-directory = "." | .jobs.extra = {
  "runs-on": "ubuntu-latest",
  "steps": [
    {"uses": "actions/setup-node@fixture", "with": {"node-version": "22"}},
    {"run": "./scripts/audit-dependencies.sh"}
  ]
}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "root-default audit wrapper" "publish-pages.yaml:extra sets up Node 22"

# Root defaults alone must not classify every Bash-only repository check as a
# Node job. The wrapper/npm invocation, not the application's folder name, matters.
reset_fixture
yq -i '.defaults.run.working-directory = "." | .jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [{"run": "bash scripts/editorial-contract.test.sh"}]
}' "${fixture}/.github/workflows/publish-pages.yaml"
check "${fixture}/package.json" "${fixture}/.github/workflows" ||
  fail "root-default Bash-only jobs must not require Node"

reset_fixture
yq -i '.jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [
    {"run": "./scripts/audit-dependencies.sh", "working-directory": "."},
    {"uses": "actions/setup-node@fixture", "with": {"node-version": "24"}}
  ]
}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "root audit before Node setup" "publish-pages.yaml:extra sets up Node after"

reset_fixture
yq -i '(.jobs.build.steps[] | select((.uses // "") | test("^actions/setup-node@")) | .with."node-version") = "22"' \
  "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "publish on Node 22" "publish-pages.yaml:build sets up Node 22, which bundles npm 10"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "cd docs && ./scripts/audit-dependencies.sh"}]}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "new job entering docs/ with cd" "publish-pages.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "pushd \"docs\" && ./scripts/audit-dependencies.sh"}]}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "new job entering docs/ with a quoted pushd" "publish-pages.yaml:extra sets up Node 22"

reset_fixture
CD_STEP="cd \"\${GITHUB_WORKSPACE}\"/docs && ./scripts/audit-dependencies.sh" yq -i '.jobs.extra = {
  "runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": strenv(CD_STEP)}]}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "new job entering docs/ through a quoted variable" "publish-pages.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "npx astro build"}]}' "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "new job running npx" "publish-pages.yaml:extra sets up Node 22"

reset_fixture
yq -i '.jobs.test-docs-npm-toolchain.if = "needs.changes.outputs.docs-npm-toolchain != '"'"'true'"'"'"' \
  "${fixture}/.github/workflows/ci.yaml"
expect_failure "inverted gate" "behind exactly needs.changes.outputs.<filter> == 'true'"

reset_fixture
WORKSPACE_DOCS="\${{ github.workspace }}/docs" yq -i '.jobs.extra = {"runs-on": "ubuntu-latest", "steps": [
  {"uses": "actions/setup-node@v7", "with": {"node-version": "22"}},
  {"run": "npm ci", "working-directory": strenv(WORKSPACE_DOCS)}]}' \
  "${fixture}/.github/workflows/publish-pages.yaml"
expect_failure "new job in docs/ by workspace path" "publish-pages.yaml:extra sets up Node 22"

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
