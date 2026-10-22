#!/usr/bin/env bash
# Pin the committed article-date boundary against real Git and the maintained renderer.
set -euo pipefail
# Fixture writes must never inherit another repository's Git context.
unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY \
  GIT_ALTERNATE_OBJECT_DIRECTORIES GIT_PREFIX GIT_NAMESPACE
export GIT_NO_REPLACE_OBJECTS=1 GIT_NO_LAZY_FETCH=1
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
scratch="$(cd -- "$scratch" && pwd -P)"
trap 'rm -rf "$scratch"' EXIT
# Report the failed behavioral control and terminate this fixture run.
fail() { printf 'journal history: FAIL — %s\n' "$*" >&2; exit 1; }
fixture="$scratch/source"
mkdir -p "$fixture/src/content/docs/blog"
git -C "$fixture" init -q
git -C "$fixture" config user.name Fixture
git -C "$fixture" config user.email fixture@example.invalid
git -C "$fixture" config commit.gpgsign false
printf '%s\n' 'A committed article.' > "$fixture/src/content/docs/blog/history.md"
# Commit timestamps are independent literal expectations, not renderer-derived values.
# Commit one fixture path at the supplied literal timestamp with its description.
commit() {
  git -C "$fixture" add -- "$1"
  GIT_AUTHOR_DATE="$2" GIT_COMMITTER_DATE="$2" git -C "$fixture" commit -qm "$3"
}
commit src/content/docs/blog/history.md 2020-01-01T12:00:00Z article
printf '%s\n' 'Unrelated release work.' > "$fixture/unrelated.txt"
commit unrelated.txt 2020-02-01T12:00:00Z unrelated
# Compare the actual maintained collector's article date with a literal expectation.
assert_date() {
  node --input-type=module - "$root" "$1" "$2" <<'JS'
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
const [root, fixture, expected] = process.argv.slice(2);
const { getAllNewestCommitDate } = await import(pathToFileURL(`${root}/node_modules/@astrojs/starlight/dist/utils/git.js`));
const { makeAPI } = await import(pathToFileURL(`${root}/node_modules/@astrojs/starlight/dist/utils/gitInlined.js`));
const dates = getAllNewestCommitDate(fixture, `${fixture}/src/content/docs`);
const actual = makeAPI(dates).getNewestCommitDate('src/content/docs/blog/history.md').toISOString();
assert.equal(actual, expected, 'an unrelated commit must not become the article modification date');
JS
}
assert_date "$fixture" 2020-01-01T12:00:00.000Z
printf '%s\n' 'A real article revision.' >> "$fixture/src/content/docs/blog/history.md"
commit src/content/docs/blog/history.md 2020-03-01T12:00:00Z revision
assert_date "$fixture" 2020-03-01T12:00:00.000Z
printf '%s\n' 'Another unrelated release.' >> "$fixture/unrelated.txt"
commit unrelated.txt 2020-04-01T12:00:00Z release
assert_date "$fixture" 2020-03-01T12:00:00.000Z

# Exercise the depth configured by each actual publishing/package builder, not a
# duplicate desired workflow. Shallow clones really reproduce the misleading date.
node --input-type=module - "$root" > "$scratch/checkouts" <<'JS'
import fs from 'node:fs';
import { load } from 'js-yaml';
const root = process.argv[2];
for (const [file, job] of [
  ['ci.yaml', 'build-docs'], ['ci.yaml', 'test-combined-runtime'],
  ['publish-site.yaml', 'build'],
]) {
  const workflow = load(fs.readFileSync(`${root}/.github/workflows/${file}`, 'utf8'));
  const steps = workflow.jobs[job].steps.filter(step => step.uses?.startsWith('actions/checkout@'));
  if (steps.length !== 1 || steps[0].if != null ||
      (steps[0]['continue-on-error'] != null && steps[0]['continue-on-error'] !== false)) {
    throw new Error(`${file}/${job}: one unconditional, required checkout is needed`);
  }
  const depth = String(steps[0].with?.['fetch-depth'] ?? 1);
  if (depth !== '0') throw new Error(`${file}/${job}: complete source history requires fetch-depth: 0`);
  console.log(`${file}/${job}\t${depth}`);
}
JS
count=0
guard="$root/scripts/check-source-history.sh"
[[ -f "$guard" ]] || fail 'source history preflight is missing'
while IFS=$'\t' read -r label depth; do
  count=$((count + 1))
  clone="$scratch/checkout-$count"
  if [[ "$depth" == 0 ]]; then
    git clone --quiet --no-local "file://$fixture" "$clone"
  else
    git clone --quiet --no-local --depth "$depth" "file://$fixture" "$clone"
  fi
  bash "$guard" "$clone" || fail "$label has incomplete source history"
  assert_date "$clone" 2020-03-01T12:00:00.000Z || fail "$label rewrites the unchanged article date"
done < "$scratch/checkouts"
[[ "$count" == 3 ]] || fail 'not every maintained builder was examined'

bash "$guard" "$fixture" || fail 'complete source history rejected'
# Require the supplied source directory to fail specifically inside the history guard.
reject() {
  if bash "$guard" "$1" > "$scratch/rejection" 2>&1; then fail "$2 accepted"; fi
  grep -q 'source history: FAIL' "$scratch/rejection" || fail "$2 failed outside the history guard"
}
git clone --quiet --no-local --depth 1 "file://$fixture" "$scratch/shallow"
reject "$scratch/shallow" 'shallow source'
git clone --quiet --no-local --depth 2 "file://$fixture" "$scratch/depth-two"
assert_date "$scratch/depth-two" 2020-03-01T12:00:00.000Z
reject "$scratch/depth-two" 'shallow source with the latest article edit visible'
mkdir "$scratch/archive"
reject "$scratch/archive" 'source archive without Git'
mkdir "$fixture/nested"
reject "$fixture/nested" 'enclosing repository'
cp -R "$fixture" "$scratch/incomplete"
parent="$(git -C "$scratch/incomplete" rev-parse HEAD^)"
object="$scratch/incomplete/.git/objects/${parent:0:2}/${parent:2}"
[[ -f "$object" ]] || fail 'missing-ancestor fixture is not a loose commit'
mv "$object" "$scratch/retained-parent"
reject "$scratch/incomplete" 'missing ancestor'

# A plausible partial traversal is not complete evidence when Git exits nonzero.
mkdir "$scratch/bin"
real_git="$(command -v git)"
cat > "$scratch/bin/git" <<'SH'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ "$arg" == log ]]; then printf '%s\n' 'plausible partial history'; exit 17; fi
done
exec "$REAL_HISTORY_GIT" "$@"
SH
chmod +x "$scratch/bin/git"
if PATH="$scratch/bin:$PATH" REAL_HISTORY_GIT="$real_git" bash "$guard" "$fixture" > "$scratch/rejection" 2>&1; then
  fail 'partial failed history accepted'
fi
grep -q 'source history: FAIL' "$scratch/rejection" || fail 'partial traversal failed outside the history guard'

# Exercise the actual build wrapper on shallow history. It must stop before any
# renderer runs; a missing, ignored, skipped or late preflight is not protection.
mkdir -p "$scratch/shallow/scripts" "$scratch/build-bin"
cp "$root/scripts/build-astro-site.sh" "$scratch/shallow/scripts/build-astro-site.sh"
cp "$guard" "$scratch/shallow/scripts/check-source-history.sh"
real_bash="$(command -v bash)"
cat > "$scratch/build-bin/bash" <<'SH'
#!/bin/bash
case "$1" in
  scripts/check-source-history.sh|*/scripts/build-astro-site.sh) exec "$REAL_HISTORY_BASH" "$@" ;;
  *) exit 0 ;;
esac
SH
cat > "$scratch/build-bin/node" <<'SH'
#!/bin/bash
exit 0
SH
cat > "$scratch/build-bin/astro" <<'SH'
#!/bin/bash
if [[ "${HISTORY_CHECK_RENDER_ENV:-false}" == true ]]; then
  "$REAL_HISTORY_NODE" --input-type=module - "$HISTORY_MODULE_ROOT" "$HISTORY_SOURCE_ROOT" <<'JS'
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
const [root, fixture] = process.argv.slice(2);
const { getAllNewestCommitDate } = await import(pathToFileURL(`${root}/node_modules/@astrojs/starlight/dist/utils/git.js`));
const { makeAPI } = await import(pathToFileURL(`${root}/node_modules/@astrojs/starlight/dist/utils/gitInlined.js`));
const actual = makeAPI(getAllNewestCommitDate(fixture, `${fixture}/src/content/docs`))
  .getNewestCommitDate('src/content/docs/blog/history.md').toISOString();
assert.equal(actual, '2020-03-01T12:00:00.000Z', 'the actual renderer must use the same source history as its preflight');
for (const variable of ['GIT_DIR', 'GIT_WORK_TREE', 'GIT_COMMON_DIR', 'GIT_INDEX_FILE',
  'GIT_OBJECT_DIRECTORY', 'GIT_ALTERNATE_OBJECT_DIRECTORIES', 'GIT_PREFIX', 'GIT_NAMESPACE']) {
  assert.equal(process.env[variable], undefined, `${variable} must not redirect rendering`);
}
assert.equal(process.env.GIT_NO_REPLACE_OBJECTS, '1', 'rendering must not reinterpret replacement history');
assert.equal(process.env.GIT_NO_LAZY_FETCH, '1', 'rendering must not fetch missing history implicitly');
JS
  [[ $? == 0 ]] || exit 1
fi
touch "$HISTORY_RENDER_RECEIPT"
exit 0
SH
chmod +x "$scratch/build-bin/bash" "$scratch/build-bin/node" "$scratch/build-bin/astro"
# Verify the actual wrapper rejects shallow history before starting its renderer.
build_rejected() {
  local output="$scratch/build-output" receipt="$scratch/renderer-started"
  rm -f "$receipt"
  if PATH="$scratch/build-bin:$PATH" HISTORY_RENDER_RECEIPT="$receipt" REAL_HISTORY_BASH="$real_bash" \
      "$real_bash" "$scratch/shallow/scripts/build-astro-site.sh" > "$output" 2>&1; then return 1; fi
  [[ ! -e "$receipt" ]] && grep -q 'source history: FAIL' "$output"
}
build_rejected || fail 'actual build renders with incomplete history'
# The common entrypoint must reject shallow history before even resolving a
# frontend (including before loading framework modules or a dotenv file).
cp "$root/scripts/build-business-site.sh" "$scratch/shallow/scripts/build-business-site.sh"
if PATH="$scratch/build-bin:$PATH" REAL_HISTORY_BASH="$real_bash" \
    "$real_bash" "$scratch/shallow/scripts/build-business-site.sh" > "$scratch/selection-output" 2>&1; then
  fail 'common entrypoint accepted shallow history'
fi
grep -q 'source history: FAIL' "$scratch/selection-output" || fail 'common entrypoint failed outside history admission'
for mutation in missing ignored skipped late; do
  case "$mutation" in
    missing) sed '/^bash scripts\/check-source-history.sh$/d' "$root/scripts/build-astro-site.sh" ;;
    ignored) sed 's|^bash scripts/check-source-history.sh$|bash scripts/check-source-history.sh \|\| true|' "$root/scripts/build-astro-site.sh" ;;
    skipped) sed 's|^bash scripts/check-source-history.sh$|if false; then bash scripts/check-source-history.sh; fi|' "$root/scripts/build-astro-site.sh" ;;
    late) sed '/^bash scripts\/check-source-history.sh$/d; /^astro build$/a\
bash scripts/check-source-history.sh
' "$root/scripts/build-astro-site.sh" ;;
  esac > "$scratch/shallow/scripts/build-astro-site.sh"
  if build_rejected; then fail "$mutation preflight regression escaped the build control"; fi
done

# Drive the real wrapper and actual Starlight collector with conflicting inherited
# Git settings. A safe preflight alone cannot protect an unsanitized renderer.
mkdir -p "$fixture/scripts"
cp "$root/scripts/build-astro-site.sh" "$fixture/scripts/build-astro-site.sh"
cp "$guard" "$fixture/scripts/check-source-history.sh"
real_node="$(command -v node)"
receipt="$scratch/context-rendered"
if ! PATH="$scratch/build-bin:$PATH" REAL_HISTORY_BASH="$real_bash" REAL_HISTORY_NODE="$real_node" \
    HISTORY_CHECK_RENDER_ENV=true HISTORY_MODULE_ROOT="$root" HISTORY_SOURCE_ROOT="$fixture" \
    HISTORY_RENDER_RECEIPT="$receipt" GIT_DIR="$scratch/shallow/.git" GIT_WORK_TREE="$scratch/shallow" \
    GIT_NO_REPLACE_OBJECTS=0 GIT_NO_LAZY_FETCH=0 \
    "$real_bash" "$fixture/scripts/build-astro-site.sh" > "$scratch/context-output" 2>&1; then
  cat "$scratch/context-output" >&2
  fail 'build preflight and renderer use different Git history'
fi
[[ -f "$receipt" ]] || fail 'source-context control never reached the renderer'

if [[ $# == 1 ]]; then
  node --input-type=module - "$root" "$1" <<'JS'
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { parse } from 'parse5';
const [root, output] = process.argv.slice(2);
const article = 'how-my-agentic-engineer-turns-problems-into-proved-working-solutions';
const document = parse(fs.readFileSync(`${output}/blog/${article}/index.html`, 'utf8'));
const times = [];
const visit = node => {
  if (node.tagName === 'time') times.push(node);
  for (const child of node.childNodes ?? []) visit(child);
};
visit(document);
const date = node => node.attrs.find(attr => attr.name === 'datetime')?.value;
const publication = times.find(node => node.attrs.some(attr => attr.name === 'class' && attr.value.includes('sl-blog-metadata-date')));
assert.equal(date(publication), '2026-08-26T00:00:00.000Z', 'deployment must not replace the article publication date');
const footer = times.filter(node => {
  for (let parent = node.parentNode; parent; parent = parent.parentNode) {
    if (parent.tagName === 'footer') return true;
  }
  return false;
});
assert.equal(footer.length, 1, 'the committed article retains one native modification date');
const timestamp = execFileSync('git', ['--no-replace-objects', '-C', root, 'log', '--format=%ct', '--max-count=1', '--', `src/content/docs/blog/${article}.mdx`], { encoding: 'utf8' }).trim();
assert.match(timestamp, /^\d+$/);
assert.equal(date(footer[0]), new Date(Number(timestamp) * 1000).toISOString(), 'rendered modification date must match the committed article, not the deployment');
console.log('journal output: PASS — native article modification and publication dates preserved');
JS
fi
printf '%s\n' 'journal history: PASS — article edit/unrelated commits, three real checkout depths, and incomplete-history rejection'
