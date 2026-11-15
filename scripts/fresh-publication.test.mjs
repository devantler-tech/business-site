import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { test } from 'node:test';
import { load } from 'js-yaml';

// CI's production-template package is the image the platform serves. Its packager
// refreshes the complete public ranking itself, so CI must run it unconditionally,
// from the repository root and with read-only GitHub authority.
const ci = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const production = 'bash scripts/package-runtime.sh --release --production-template';
const valid = workflow => {
  const job = workflow.jobs?.['test-combined-runtime'];
  const steps = job?.steps ?? [];
  const indices = predicate => steps.flatMap((step, index) => predicate(step) ? [index] : []);
  const packaged = indices(step => step.run === production);
  const image = indices(step => step.run?.includes('--tag business-site:production-test'));
  const directory = step => step['working-directory'] ??
    job.defaults?.run?.['working-directory'] ??
    workflow.defaults?.run?.['working-directory'] ?? '.';
  return [packaged, image].every(group => group.length === 1) && packaged[0] < image[0] &&
    steps[packaged[0]].if == null &&
    (steps[packaged[0]]['continue-on-error'] == null || steps[packaged[0]]['continue-on-error'] === false) &&
    directory(steps[packaged[0]]) === '.' &&
    steps[packaged[0]].env?.GH_TOKEN === '${{ github.token }}' &&
    job.permissions?.contents === 'read';
};
test('the production runtime package refreshes the complete public ranking before its image is built', () => assert.ok(valid(ci)));
for (const mode of ['missing', 'skipped', 'ignored', 'after-image', 'wrong-directory', 'workflow-default-directory', 'job-default-directory', 'no-token']) {
  test(`fresh production package rejects ${mode}`, () => {
    const workflow = structuredClone(ci);
    assert.ok(valid(workflow), 'controls start from the real valid CI workflow');
    const job = workflow.jobs['test-combined-runtime'];
    const step = job.steps.find(candidate => candidate.run === production);
    if (mode === 'missing') job.steps = job.steps.filter(candidate => candidate !== step);
    if (mode === 'skipped') step.if = 'false';
    if (mode === 'ignored') step['continue-on-error'] = true;
    if (mode === 'after-image') job.steps = job.steps.filter(candidate => candidate !== step).concat(step);
    if (mode === 'wrong-directory') step['working-directory'] = 'web';
    if (mode === 'workflow-default-directory') { delete step['working-directory']; workflow.defaults = { run: { 'working-directory': 'web' } }; }
    if (mode === 'job-default-directory') { delete step['working-directory']; job.defaults = { run: { 'working-directory': 'web' } }; }
    if (mode === 'no-token') delete step.env.GH_TOKEN;
    assert.equal(valid(workflow), false);
  });
}

// Run the actual packager with boundary doubles. The build double copies the
// snapshot it receives, so a refresh that ran late, failed or fed another file
// cannot look like a fresh production build.
function packageProduction(refresh) {
  const root = mkdtempSync(path.join(tmpdir(), 'fresh-production-'));
  try {
    const packager = readFileSync('scripts/package-runtime.sh', 'utf8');
    const go = packager.match(/go version go([0-9.]+) /)?.[1];
    assert.ok(go, 'the packager names its exact Go toolchain');
    const node = readFileSync('.node-version', 'utf8').trim();
    mkdirSync(path.join(root, 'scripts')); mkdirSync(path.join(root, 'bin'));
    cpSync('scripts/package-runtime.sh', path.join(root, 'scripts/package-runtime.sh'));
    cpSync('.node-version', path.join(root, '.node-version'));
    if (refresh != null) writeFileSync(path.join(root, 'scripts/refresh-public-stars.sh'), refresh);
    writeFileSync(path.join(root, 'bin/git'), `#!/bin/sh\nprintf '%s\\n' ${'a'.repeat(40)}\n`, { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/node'), `#!/bin/sh\nprintf 'v${node}.0.0\\n'\n`, { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/go'), `#!/bin/sh\nprintf 'go version go${go} linux/amd64\\n'\n`, { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/npm'), `#!/bin/bash
if [[ "$1" == --version ]]; then printf '11.0.0\\n'; exit 0; fi
[[ "$1 $2" == "run build" ]] || exit 41
cp -- "$PUBLIC_STARS_SNAPSHOT" "$CAPTURE" || exit 43
exit 42
`, { mode: 0o700 });
    const capture = path.join(root, 'captured.json');
    const result = spawnSync('bash', ['scripts/package-runtime.sh', '--development', '--production-template'], {
      cwd: root, timeout: 10000, encoding: 'utf8',
      env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, CAPTURE: capture, GH_TOKEN: 'fixture-read-token' },
    });
    return { status: result.status, snapshot: existsSync(capture) ? readFileSync(capture, 'utf8') : null };
  } finally { rmSync(root, { recursive: true, force: true }); }
}
test('the actual packager builds production only from the ranking it just refreshed', () => {
  const fresh = packageProduction('#!/bin/sh\n[ "$#" = 1 ] || exit 9\nprintf \'{"fresh":"%s"}\' "$GH_TOKEN" > "$1"\n');
  assert.equal(fresh.status, 42, 'the build double stops right after receiving the refreshed snapshot');
  assert.equal(fresh.snapshot, '{"fresh":"fixture-read-token"}', 'the refresh ran first with the caller token and fed the build');
});
for (const [mode, refresh] of [['failed refresh', '#!/bin/sh\nexit 7\n'], ['missing refresh', null]]) {
  test(`the actual packager stops before building on a ${mode}`, () => {
    const result = packageProduction(refresh);
    assert.notEqual(result.status, 0);
    assert.notEqual(result.status, 42, 'a failed ranking read never reaches the production build');
    assert.equal(result.snapshot, null);
  });
}
