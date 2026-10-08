#!/usr/bin/env bash

set -euo pipefail

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(CDPATH='' cd -- "${script_dir}/.." && pwd -P)"
audit_script="${script_dir}/audit-dependencies.sh"
ci_workflow="${repo_root}/.github/workflows/ci.yaml"
scheduled_workflow="${repo_root}/.github/workflows/audit-docs.yaml"
npmrc="${repo_root}/.npmrc"
tmp_dir="$(mktemp -d)"
# An abort must not read as a pass. Bash 3.2 reports $? as 0 to an EXIT trap after a
# `set -u` abort, and a successful `rm` in the trap can become the script's own
# status, so reaching the end is the only way a zero status leaves this script.
completed=0
cleanup() {
  local rc=$?
  rm -rf "${tmp_dir}"
  if [ "${completed}" -ne 1 ] && [ "${rc}" -eq 0 ]; then
    printf 'docs audit: FAIL - aborted before finishing\n' >&2
    rc=1
  fi
  exit "${rc}"
}
trap cleanup EXIT

fail() {
  printf 'docs audit: FAIL - %s\n' "$*" >&2
  exit 1
}

[ -x "${audit_script}" ] || fail "audit-dependencies.sh is missing or not executable"
command -v yq >/dev/null 2>&1 || fail "yq is required to validate workflow wiring"

mkdir -p "${tmp_dir}/bin"
cat >"${tmp_dir}/bin/npm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "$#" -eq 2 ] && [ "$1" = "audit" ] && [ "$2" = "--omit=dev" ] || {
  printf 'unexpected npm arguments: %s\n' "$*" >&2
  exit 64
}

count_file="${FAKE_NPM_COUNT_FILE:?}"
count=0
[ ! -f "${count_file}" ] || count="$(<"${count_file}")"
count=$((count + 1))
printf '%s\n' "${count}" >"${count_file}"

case "${FAKE_NPM_MODE:?}" in
  success)
    printf 'found 0 vulnerabilities\n'
    ;;
  vulnerable)
    printf '1 high severity vulnerability\n' >&2
    exit 1
    ;;
  endpoint-then-success)
    if [ "${count}" -eq 1 ]; then
      printf 'npm error audit endpoint returned an error\n' >&2
      exit 1
    fi
    printf 'found 0 vulnerabilities\n'
    ;;
  endpoint-always)
    printf 'npm error audit endpoint returned an error\n' >&2
    exit 1
    ;;
  *)
    printf 'unknown fake mode\n' >&2
    exit 64
    ;;
esac
EOF
chmod +x "${tmp_dir}/bin/npm"

run_case() {
  local mode="$1" expected_status="$2" expected_attempts="$3"
  local count_file="${tmp_dir}/${mode}.count"
  local status=0

  PATH="${tmp_dir}/bin:${PATH}" \
    FAKE_NPM_MODE="${mode}" \
    FAKE_NPM_COUNT_FILE="${count_file}" \
    "${audit_script}" >/dev/null 2>&1 || status=$?

  [ "${status}" -eq "${expected_status}" ] ||
    fail "${mode} exited ${status}; expected ${expected_status}"
  [ "$(<"${count_file}")" -eq "${expected_attempts}" ] ||
    fail "${mode} used $(<"${count_file}") attempts; expected ${expected_attempts}"
}

run_case success 0 1
run_case vulnerable 1 1
run_case endpoint-then-success 0 2
run_case endpoint-always 1 2

# What decides the live audit's answer: the dependency tree npm reads (a shrinkwrap
# replaces the lockfile when one exists), the npm settings that select the advisory
# source and what the audit counts, and the wrapper that runs it.
live_audit_inputs='[".npmrc", "npm-shrinkwrap.json", "package-lock.json", "package.json", "scripts/audit-dependencies.sh"]'
# What the contract test reads.
contract_inputs='[".github/workflows/audit-docs.yaml", ".github/workflows/ci.yaml", ".npmrc", "scripts/audit-dependencies.sh", "scripts/audit-dependencies.test.sh"]'

# Evaluate a yq expression against the parsed change filters of a CI workflow, with
# NAME naming one filter and EXPECTED holding a JSON list of paths. Fails when the
# filters cannot be read or parsed.
filters_query() { # ci-workflow filter-name expected-json yq-expression
  local filters
  filters="$(yq -r '.jobs.changes.steps[] | select(.id == "filter") | .with.filters // ""' "$1" 2>/dev/null)" ||
    return 1
  [ -n "${filters}" ] || return 1
  printf '%s\n' "${filters}" | NAME="$2" EXPECTED="$3" yq -r "$4" 2>/dev/null
}

# Validate how both workflows invoke the wrapper and what npm settings the audit runs
# under. Prints the first violation and returns 1; returns 0 when the wiring holds.
validate_wiring() { # ci-workflow scheduled-workflow npmrc
  local ci="$1" scheduled="$2" settings="$3" workflow job answer line

  for workflow in "${ci}" "${scheduled}"; do
    yq -e '
      [.jobs.audit-docs.steps[]
        | select(.name == "Test audit wrapper")
        | select(."working-directory" == ".")
        | select(.run == "./scripts/audit-dependencies.test.sh")
        | select(has("if") | not)
        | select(has("continue-on-error") | not)]
      | length == 1
    ' "${workflow}" >/dev/null 2>&1 || {
      printf '%s does not run the audit contract test unconditionally\n' "${workflow##*/}"
      return 1
    }

    yq -e '
      [.jobs.audit-docs.steps[]
        | select(.name == "Audit")
        | select(."working-directory" == ".")
        | select(.run == "./scripts/audit-dependencies.sh")
        | select(has("if") | not)
        | select(has("continue-on-error") | not)]
      | length == 1
    ' "${workflow}" >/dev/null 2>&1 || {
      printf '%s does not run the shared audit wrapper unconditionally\n' "${workflow##*/}"
      return 1
    }

    yq -e '
      [.jobs.audit-docs.steps[] | select(.run == "npm ci")]
      | length == 0
    ' "${workflow}" >/dev/null 2>&1 || {
      printf '%s rebuilds node_modules before a lockfile audit\n' "${workflow##*/}"
      return 1
    }
  done

  # The scheduled copy is what reports an advisory against an unchanged lockfile.
  yq -e '(.on.schedule | tag) == "!!seq" and (.on.schedule | length) > 0' \
    "${scheduled}" >/dev/null 2>&1 || {
    printf '%s has no schedule\n' "${scheduled##*/}"
    return 1
  }
  yq -e '.jobs.audit-docs | (has("if") or has("continue-on-error")) | not' \
    "${scheduled}" >/dev/null 2>&1 || {
    printf '%s runs its audit job conditionally\n' "${scheduled##*/}"
    return 1
  }

  # The live audit runs for exactly its inputs. A workflow-only change cannot alter
  # what the site depends on, so a workflow path here makes an advisory with no
  # released fix fail every pull request that edits CI; a glob can match one as
  # easily as a literal path. The filters are compared as parsed lists, so no layout
  # of the block can show these paths without the filter holding them.
  answer="$(filters_query "${ci}" docs-deps "${live_audit_inputs}" \
    '(.[strenv(NAME)] | tag) == "!!seq"')" && [ "${answer}" = true ] || {
    printf 'docs-deps filter is not a readable list of paths\n'
    return 1
  }
  answer="$(filters_query "${ci}" docs-deps "${live_audit_inputs}" \
    '((strenv(EXPECTED) | from_yaml) - .[strenv(NAME)]) | .[0] // ""')" || {
    printf 'docs-deps filter is not a readable list of paths\n'
    return 1
  }
  if [ -n "${answer}" ]; then
    printf 'docs-deps filter does not run the live audit for %s\n' "${answer}"
    return 1
  fi
  answer="$(filters_query "${ci}" docs-deps "${live_audit_inputs}" \
    '(.[strenv(NAME)] - (strenv(EXPECTED) | from_yaml)) | length')" && [ "${answer}" = 0 ] || {
    printf 'docs-deps filter runs the live audit for more than its inputs\n'
    return 1
  }

  yq -e '.jobs.audit-docs.if == "needs.changes.outputs.docs-deps == '"'"'true'"'"'"' \
    "${ci}" >/dev/null 2>&1 || {
    printf 'audit-docs is not gated on the docs-deps filter\n'
    return 1
  }

  # The contract test self-gates on everything it reads.
  answer="$(filters_query "${ci}" docs-audit-contract "${contract_inputs}" \
    '(.[strenv(NAME)] | tag) == "!!seq"')" && [ "${answer}" = true ] || {
    printf 'docs-audit-contract filter is not a readable list of paths\n'
    return 1
  }
  answer="$(filters_query "${ci}" docs-audit-contract "${contract_inputs}" \
    '((strenv(EXPECTED) | from_yaml) - .[strenv(NAME)]) | .[0] // ""')" || {
    printf 'docs-audit-contract filter is not a readable list of paths\n'
    return 1
  }
  if [ -n "${answer}" ]; then
    printf 'docs-audit-contract filter does not self-gate %s\n' "${answer}"
    return 1
  fi

  # shellcheck disable=SC2016 # ${{ … }} is workflow syntax, not a shell expansion.
  yq -e '
    .jobs.changes.outputs."docs-audit-contract"
      == "${{ steps.filter.outputs.docs-audit-contract }}"
  ' "${ci}" >/dev/null 2>&1 || {
    printf 'the changes job does not export the docs-audit-contract filter\n'
    return 1
  }

  yq -e '
    .jobs.test-docs-audit-wrapper.if
      == "needs.changes.outputs.docs-audit-contract == '"'"'true'"'"'"
  ' "${ci}" >/dev/null 2>&1 || {
    printf 'test-docs-audit-wrapper is not gated on the docs-audit-contract filter\n'
    return 1
  }

  yq -e '
    [.jobs.test-docs-audit-wrapper.steps[]
      | select(."working-directory" == ".")
      | select(.run == "./scripts/audit-dependencies.test.sh")
      | select(has("if") | not)
      | select(has("continue-on-error") | not)]
    | length == 1
  ' "${ci}" >/dev/null 2>&1 || {
    printf 'test-docs-audit-wrapper does not run the audit contract test unconditionally\n'
    return 1
  }

  yq -e '
    [.jobs.test-docs-audit-wrapper.steps[] | select((.run // "") | test("audit-dependencies\.sh"))]
    | length == 0
  ' "${ci}" >/dev/null 2>&1 || {
    printf 'test-docs-audit-wrapper runs the live audit\n'
    return 1
  }

  # The required check waits for both jobs and reports both results.
  for job in audit-docs test-docs-audit-wrapper; do
    JOB="${job}" yq -e '.jobs.status.needs | any_c(. == strenv(JOB))' \
      "${ci}" >/dev/null 2>&1 || {
      printf 'CI - Required Checks does not wait for %s\n' "${job}"
      return 1
    }
    JOB="${job}" yq -e '
      [.jobs.status.steps[]
        | (.with."job-results" // "")
        | select(contains("needs." + strenv(JOB) + ".result"))]
      | length == 1
    ' "${ci}" >/dev/null 2>&1 || {
      printf 'CI - Required Checks does not report the result of %s\n' "${job}"
      return 1
    }
  done

  # npm reads its settings from this file on every audit: `audit-level` and `include`
  # change what the audit counts and `registry` chooses who answers it. A change that
  # turns the audit green that way triggers nothing the audit itself could catch, so
  # the file may hold reviewed settings only. npm trims each line and accepts CRLF
  # endings, so a line is judged the way npm reads it: an indented comment or a
  # whitespace-only line sets nothing, while an indented setting is still a setting.
  if [ -e "${settings}" ]; then
    while IFS= read -r line || [ -n "${line}" ]; do
      line="${line%$'\r'}"
      line="${line#"${line%%[![:space:]]*}"}"
      line="${line%"${line##*[![:space:]]}"}"
      case "${line}" in
        '' | '#'* | ';'*) continue ;;
        'registry=https://registry.npmjs.org/') continue ;;
      esac
      printf '.npmrc sets "%s", which this contract has not reviewed\n' "${line}"
      return 1
    done <"${settings}"
  fi
}

violation="$(validate_wiring "${ci_workflow}" "${scheduled_workflow}" "${npmrc}")" ||
  fail "${violation}"

# Each case edits a fresh copy of the real files and names the violation it expects,
# so a case cannot pass for an unrelated reason.
fixture_ci="${tmp_dir}/ci.yaml"
fixture_scheduled="${tmp_dir}/audit-docs.yaml"
fixture_npmrc="${tmp_dir}/npmrc"
mutations_run=0

reset_fixture() {
  cp "${ci_workflow}" "${fixture_ci}"
  cp "${scheduled_workflow}" "${fixture_scheduled}"
  rm -f "${fixture_npmrc}"
  [ ! -e "${npmrc}" ] || cp "${npmrc}" "${fixture_npmrc}"
}

expect_violation() { # description expected-violation
  local description="$1" expected="$2" actual
  if actual="$(validate_wiring "${fixture_ci}" "${fixture_scheduled}" "${fixture_npmrc}")"; then
    fail "mutation passed: ${description}"
  fi
  [ "${actual}" = "${expected}" ] ||
    fail "${description}: got '${actual}', expected '${expected}'"
  mutations_run=$((mutations_run + 1))
}

# Apply a yq expression to the parsed change filters of the fixture CI workflow.
edit_filters() { # yq-expression
  yq -i '
    (.jobs.changes.steps[] | select(.id == "filter") | .with.filters)
      |= (from_yaml | '"$1"' | to_yaml)
  ' "${fixture_ci}"
}

reset_fixture
violation="$(validate_wiring "${fixture_ci}" "${fixture_scheduled}" "${fixture_npmrc}")" ||
  fail "an unedited copy of the real files is rejected: ${violation}"
edit_filters '.'
violation="$(validate_wiring "${fixture_ci}" "${fixture_scheduled}" "${fixture_npmrc}")" ||
  fail "a reformatted but unchanged filter block is rejected: ${violation}"

# The live audit's filter.
reset_fixture
edit_filters '.docs-deps += [".github/workflows/ci.yaml"]'
expect_violation "the CI workflow re-enters the live audit filter" \
  "docs-deps filter runs the live audit for more than its inputs"

reset_fixture
edit_filters '.docs-deps += [".github/workflows/audit-docs.yaml"]'
expect_violation "the scheduled workflow re-enters the live audit filter" \
  "docs-deps filter runs the live audit for more than its inputs"

reset_fixture
edit_filters '.docs-deps += ["**"]'
expect_violation "a catch-all glob enters the live audit filter" \
  "docs-deps filter runs the live audit for more than its inputs"

reset_fixture
edit_filters '.docs-deps += ["docs/**"]'
expect_violation "every docs change runs the live audit" \
  "docs-deps filter runs the live audit for more than its inputs"

reset_fixture
edit_filters '.docs-deps -= ["package-lock.json"]'
expect_violation "a lockfile change no longer runs the live audit" \
  "docs-deps filter does not run the live audit for package-lock.json"

reset_fixture
edit_filters '.docs-deps -= ["scripts/audit-dependencies.sh"]'
expect_violation "a wrapper change no longer runs the live audit" \
  "docs-deps filter does not run the live audit for scripts/audit-dependencies.sh"

reset_fixture
edit_filters '.docs-deps -= [".npmrc"]'
expect_violation "an npm settings change no longer runs the live audit" \
  "docs-deps filter does not run the live audit for .npmrc"

reset_fixture
edit_filters '.docs-deps = ["docs/never-matches", ".npmrc\nnpm-shrinkwrap.json\npackage-lock.json\npackage.json\nscripts/audit-dependencies.sh"]'
expect_violation "the paths appear in the block but the filter holds none of them" \
  "docs-deps filter does not run the live audit for .npmrc"

reset_fixture
edit_filters '.docs-deps = "package.json"'
expect_violation "the live audit filter is not a list" \
  "docs-deps filter is not a readable list of paths"

reset_fixture
yq -i '.jobs.audit-docs.if = "needs.changes.outputs.docs-audit-contract == '"'"'true'"'"'"' \
  "${fixture_ci}"
expect_violation "the live audit is gated on the contract filter" \
  "audit-docs is not gated on the docs-deps filter"

# The contract test's filter and job.
reset_fixture
edit_filters '.docs-audit-contract -= [".github/workflows/ci.yaml"]'
expect_violation "a CI workflow change no longer runs the contract test" \
  "docs-audit-contract filter does not self-gate .github/workflows/ci.yaml"

reset_fixture
edit_filters '.docs-audit-contract -= [".npmrc"]'
expect_violation "an npm settings change no longer runs the contract test" \
  "docs-audit-contract filter does not self-gate .npmrc"

reset_fixture
yq -i 'del(.jobs.changes.outputs."docs-audit-contract")' "${fixture_ci}"
expect_violation "the contract filter is not exported" \
  "the changes job does not export the docs-audit-contract filter"

reset_fixture
yq -i '.jobs.test-docs-audit-wrapper.if = "false"' "${fixture_ci}"
expect_violation "the contract job never runs" \
  "test-docs-audit-wrapper is not gated on the docs-audit-contract filter"

reset_fixture
yq -i '.jobs.test-docs-audit-wrapper.steps[-1].continue-on-error = true' "${fixture_ci}"
expect_violation "the contract test may fail without failing its job" \
  "test-docs-audit-wrapper does not run the audit contract test unconditionally"

reset_fixture
yq -i '.jobs.test-docs-audit-wrapper.steps[-1].if = "false"' "${fixture_ci}"
expect_violation "the contract test step is switched off" \
  "test-docs-audit-wrapper does not run the audit contract test unconditionally"

reset_fixture
yq -i '.jobs.test-docs-audit-wrapper.steps += [{"name": "Audit", "working-directory": ".", "run": "./scripts/audit-dependencies.sh"}]' \
  "${fixture_ci}"
expect_violation "the contract job runs the live audit" \
  "test-docs-audit-wrapper runs the live audit"

# The required check.
reset_fixture
yq -i '.jobs.status.needs -= ["test-docs-audit-wrapper"]' "${fixture_ci}"
expect_violation "the required check does not wait for the contract job" \
  "CI - Required Checks does not wait for test-docs-audit-wrapper"

reset_fixture
yq -i '(.jobs.status.steps[] | select(.with."job-results") | .with."job-results") |= sub("\$\{\{ needs\.audit-docs\.result \}\}\s*"; "")' \
  "${fixture_ci}"
expect_violation "the required check does not report the live audit" \
  "CI - Required Checks does not report the result of audit-docs"

# The audit steps of both call sites.
reset_fixture
yq -i 'del(.jobs.audit-docs.steps[] | select(.name == "Test audit wrapper"))' "${fixture_ci}"
expect_violation "the CI audit job stops running the contract test" \
  "ci.yaml does not run the audit contract test unconditionally"

reset_fixture
yq -i '.jobs.audit-docs.steps += [{"name": "Install", "working-directory": ".", "run": "npm ci"}]' \
  "${fixture_ci}"
expect_violation "the CI audit job installs before auditing" \
  "ci.yaml rebuilds node_modules before a lockfile audit"

reset_fixture
yq -i '(.jobs.audit-docs.steps[] | select(.name == "Audit") | .continue-on-error) = true' \
  "${fixture_ci}"
expect_violation "a failing audit no longer fails the CI job" \
  "ci.yaml does not run the shared audit wrapper unconditionally"

reset_fixture
yq -i 'del(.jobs.audit-docs.steps[] | select(.name == "Audit"))' "${fixture_scheduled}"
expect_violation "the scheduled workflow stops auditing" \
  "audit-docs.yaml does not run the shared audit wrapper unconditionally"

reset_fixture
yq -i '(.jobs.audit-docs.steps[] | select(.name == "Audit") | .if) = "false"' \
  "${fixture_scheduled}"
expect_violation "the scheduled audit step is switched off" \
  "audit-docs.yaml does not run the shared audit wrapper unconditionally"

reset_fixture
yq -i 'del(.on.schedule)' "${fixture_scheduled}"
expect_violation "the scheduled workflow loses its schedule" \
  "audit-docs.yaml has no schedule"

reset_fixture
yq -i '.jobs.audit-docs.if = "false"' "${fixture_scheduled}"
expect_violation "the scheduled audit job is switched off" \
  "audit-docs.yaml runs its audit job conditionally"

# The npm settings the audit runs under.
reset_fixture
printf '\r\n   \n\t# indented comment\r\n  ; another\n  registry=https://registry.npmjs.org/ \r\n' >"${fixture_npmrc}"
violation="$(validate_wiring "${fixture_ci}" "${fixture_scheduled}" "${fixture_npmrc}")" ||
  fail "formatting npm ignores is rejected: ${violation}"

reset_fixture
printf 'audit-level=critical\n' >>"${fixture_npmrc}"
expect_violation "the audit level is raised above the advisory" \
  '.npmrc sets "audit-level=critical", which this contract has not reviewed'

reset_fixture
printf 'include=dev\n' >>"${fixture_npmrc}"
expect_violation "the audited dependency set is changed" \
  '.npmrc sets "include=dev", which this contract has not reviewed'

reset_fixture
printf 'registry=https://registry.example.invalid/\n' >"${fixture_npmrc}"
expect_violation "another registry answers the audit" \
  '.npmrc sets "registry=https://registry.example.invalid/", which this contract has not reviewed'

reset_fixture
printf 'audit=false' >>"${fixture_npmrc}"
expect_violation "a setting on a final line without a newline" \
  '.npmrc sets "audit=false", which this contract has not reviewed'

reset_fixture
printf '  \taudit-level=critical\n' >>"${fixture_npmrc}"
expect_violation "an indented setting" \
  '.npmrc sets "audit-level=critical", which this contract has not reviewed'

reset_fixture
printf 'include=dev\r\n' >>"${fixture_npmrc}"
expect_violation "a setting on a CRLF line" \
  '.npmrc sets "include=dev", which this contract has not reviewed'

[ "${mutations_run}" -eq 32 ] || fail "ran ${mutations_run} wiring mutations; expected 32"

completed=1
printf 'docs audit: PASS\n'
