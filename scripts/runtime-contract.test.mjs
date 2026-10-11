import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import * as yaml from 'js-yaml';

const workflow = yaml.load(readFileSync('.github/workflows/ci.yaml', 'utf8'));
/** Reject missing, conditional or error-tolerant runtime checks in the actual CI workflow. */
function assertRuntime(workflow) {
  const job = workflow.jobs['test-portal-identity'];
  assert.equal(job.if, undefined);
  const steps = job.steps;
  const node = steps.findIndex(s => s.uses?.startsWith('actions/setup-node@'));
  const install = steps.findIndex(s => s.run === 'npm ci' && s['working-directory'] === '.');
  const build = steps.findIndex(s => s.run === 'bash scripts/package-runtime.sh --release' && s['working-directory'] === '.');
  const smoke = steps.findIndex(s => s.run === 'node scripts/runtime-smoke.test.mjs runtime-dist' && s['working-directory'] === '.');
  assert.equal(steps[node]?.with?.['node-version'], '24');
  assert.ok(node >= 0 && install > node && build > install && smoke > build);
  for (const index of [node, install, build, smoke]) {
    assert.equal(steps[index].if, undefined, 'required runtime steps cannot be conditional');
    assert.ok(steps[index]['continue-on-error'] == null || steps[index]['continue-on-error'] === false, 'required runtime failures cannot be ignored');
  }
  assert.ok(workflow.jobs.status.needs.includes('test-portal-identity'));
  assert.match(workflow.jobs.status.steps[0].with['job-results'], /needs\.test-portal-identity\.result/);
  const upload = steps.find(s => s.with?.name === 'business-site-runtime');
  assert.equal(upload?.with?.['if-no-files-found'], 'error');
  assert.equal(upload?.with?.path, 'runtime-dist');
}
test('CI builds and exercises the actual combined package unconditionally', () => assertRuntime(workflow));
for (const [name, mutate] of [
  ['conditional build', w => { w.jobs['test-portal-identity'].steps.find(s => s.run === 'bash scripts/package-runtime.sh --release').if = 'false' }],
  ['ignored smoke failure', w => { w.jobs['test-portal-identity'].steps.find(s => s.run === 'node scripts/runtime-smoke.test.mjs runtime-dist')['continue-on-error'] = true }],
  ['skipped dependency install', w => { w.jobs['test-portal-identity'].steps.find(s => s.run === 'npm ci').if = 'false' }],
  ['missing build', w => { w.jobs['test-portal-identity'].steps = w.jobs['test-portal-identity'].steps.filter(s => s.run !== 'bash scripts/package-runtime.sh --release') }],
  ['unaggregated runtime', w => { w.jobs.status.needs = w.jobs.status.needs.filter(n => n !== 'test-portal-identity') }],
]) {
  test(`runtime contract rejects ${name}`, () => {
    const changed = structuredClone(workflow); mutate(changed);
    assert.throws(() => assertRuntime(changed));
  });
}
