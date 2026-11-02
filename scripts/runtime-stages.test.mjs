import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync('.github/workflows/ci.yaml', 'utf8'));

/** Check the real job boundaries, not a simulated runner or elapsed-time promise. */
function assertStages(w) {
  const identity = w.jobs['test-portal-identity'];
  const quality = w.jobs['test-portal-quality'];
  const runtime = w.jobs['test-combined-runtime'];
  assert.ok(identity && quality && runtime, 'quality, database tests and packaging need separate budgets');
  assert.deepEqual(runtime.needs, ['test-portal-quality', 'test-portal-identity']);
  assert.equal(quality.services, undefined, 'quality observers do not need a database');
  assert.equal(quality.needs, undefined, 'quality checks run independently of database tests');
  assert.equal(identity.needs, undefined, 'database tests run independently of quality setup');
  assert.equal(identity.steps.some(s => /\bnpm\b|\bnpx\b/.test(s.run ?? '')), false, 'database checks must not reinstall frontend tooling');
  assert.ok(quality.steps.some(s => s.run === 'npm ci'));
  assert.ok(quality.steps.some(s => s.run === 'node --test scripts/identity-cohorts.test.mjs'));
  for (const [name, job] of [['test-portal-quality', quality], ['test-portal-identity', identity], ['test-combined-runtime', runtime]]) {
    assert.equal(job.if, undefined);
    assert.ok(job['continue-on-error'] == null || job['continue-on-error'] === false);
    assert.equal(job['timeout-minutes'], 10, 'do not extend the existing job budget');
    assert.deepEqual(job.permissions, { contents: 'read' });
    for (const gate of ['validation', 'status']) {
      assert.ok(w.jobs[gate].needs.includes(name));
      assert.ok(w.jobs[gate].steps[0].with['job-results'].includes('${{ needs.' + name + '.result }}'));
    }
  }
  assert.equal(identity.steps.some(s => s.run === 'npm --prefix web ci'), false);
  assert.equal(identity.steps.some(s => s.run?.includes('scripts/package-runtime.sh')), false);
  assert.equal(runtime.services, undefined, 'packaging does not provision the identity database again');
  const checkout = runtime.steps.find(s => s.uses?.startsWith('actions/checkout@'));
  assert.equal(checkout?.with?.['fetch-depth'], 0);
  assert.equal(checkout?.with?.['persist-credentials'], false);
  const node = runtime.steps.find(s => s.uses?.startsWith('actions/setup-node@'));
  assert.equal(node?.with?.['node-version-file'], '.node-version');
  assert.equal(node?.with?.cache, 'npm');
  assert.deepEqual(node?.with?.['cache-dependency-path']?.trim().split(/\s+/), ['package-lock.json', 'web/package-lock.json']);
  const go = runtime.steps.find(s => s.uses?.startsWith('actions/setup-go@'));
  assert.equal(go?.with?.['go-version-file'], 'server/go.mod');
  assert.equal(go?.with?.['cache-dependency-path'], 'server/go.sum');
  const build = runtime.steps.findIndex(s => s.run === 'bash scripts/package-runtime.sh --release');
  assert.ok(build >= 0);
  const preparations = runtime.steps.filter(s => s.id === 'tenant-renderer');
  assert.equal(preparations.length, 1, 'renderer CLI startup must be prepared independently of package assertions');
  assert.deepEqual(preparations[0], {
    name: 'Prepare the runner-provided Kubernetes renderer',
    id: 'tenant-renderer',
    'timeout-minutes': 1,
    'working-directory': '.',
    run: 'command -v kubectl\nkubectl version --client --output=json\n',
  });
  const packageAssertions = runtime.steps.findIndex(s => s.run === 'node --test scripts/tenant-package.test.mjs');
  assert.ok(packageAssertions >= 0);
  assert.ok(runtime.steps.indexOf(preparations[0]) < packageAssertions, 'prepare the CLI before the first bounded render');
  for (const command of ['npm ci', 'npm --prefix web ci']) {
    const installs = runtime.steps.filter(s => s.run === command);
    assert.equal(installs.length, 1);
    assert.ok(runtime.steps.indexOf(installs[0]) < build);
    assert.equal(installs[0]['working-directory'], '.');
    assert.equal(installs[0].if, undefined);
    assert.ok(installs[0]['continue-on-error'] == null || installs[0]['continue-on-error'] === false);
  }
  assert.deepEqual(w.jobs['publish-container'].needs, ['validation', 'test-combined-runtime']);
}

test('CI isolates setup and packaging without weakening the complete publication chain', () => assertStages(workflow));

test('the actual renderer startup command propagates failure even after plausible version output', () => {
  const setup = workflow.jobs['test-combined-runtime'].steps.find(s => s.id === 'tenant-renderer');
  assert.ok(setup, 'the actual inline renderer preparation must exist');
  const scratch = mkdtempSync(path.join(tmpdir(), 'tenant-renderer-control-'));
  try {
    const executable = path.join(scratch, 'kubectl');
    const record = path.join(scratch, 'calls');
    writeFileSync(executable, '#!/bin/sh\nprintf "%s\\n" "$*" >> "$RENDERER_RECORD"\nprintf "%s\\n" \'{"clientVersion":{"gitVersion":"v1.35.0"},"kustomizeVersion":"v5.7.1"}\'\nexit "$RENDERER_EXIT"\n');
    chmodSync(executable, 0o700);
    for (const exit of [0, 23]) {
      writeFileSync(record, '');
      const result = spawnSync('/bin/bash', ['-e', '-o', 'pipefail', '-c', setup.run], {
        env: { PATH: scratch, RENDERER_RECORD: record, RENDERER_EXIT: String(exit) },
        encoding: 'utf8', timeout: 5000,
      });
      assert.equal(result.error, undefined);
      assert.equal(result.status, exit);
      assert.match(result.stdout, /clientVersion/, 'the refusing producer emitted plausible output first');
      assert.equal(readFileSync(record, 'utf8'), 'version --client --output=json\n');
    }
  } finally {
    rmSync(scratch, { recursive: true, force: true });
  }
});

// A scaled fault model, not a network benchmark or an Actions runner: execute a
// deadline-bound child for each actual job's assigned work. A delayed compiler
// install must exhaust the collapsed budget, but not either independent budget.
// Hosted execution is still required to prove the real commands finish in time.
function delayedSetup(w, failingStage = '') {
  const attempted = [];
  for (const name of ['test-portal-quality', 'test-portal-identity', 'test-combined-runtime']) {
    const job = w.jobs[name];
    if (!job) continue;
    attempted.push(name);
    const identity = job.steps.some(s => s.run?.startsWith('go test -race -coverprofile='));
    const quality = job.steps.some(s => s.run?.includes('portal-quality-provisioning.test.mjs'));
    const compiler = job.steps.some(s => s.run === 'npm --prefix web ci');
    const packageBuild = job.steps.some(s => s.run === 'bash scripts/package-runtime.sh --release');
    const milliseconds = Number(identity) * 600 + Number(quality) * 600 + Number(compiler) * 300 + Number(packageBuild) * 300;
    const result = spawnSync(process.execPath, ['-e',
      'setTimeout(() => process.exit(Number(process.argv[2])), Number(process.argv[1]))',
      String(milliseconds), String(Number(name === failingStage)),
    ], { timeout: job['timeout-minutes'] * 100, encoding: 'utf8' });
    if (result.error || result.status !== 0) return { attempted, published: false, timeout: result.error?.code === 'ETIMEDOUT' };
  }
  return { attempted, published: true, timeout: false };
}

test('delayed setup exhausts a collapsed budget but fits independently bounded stages', () => {
  const collapsed = structuredClone(workflow);
  collapsed.jobs['test-portal-identity'].steps.push(...collapsed.jobs['test-portal-quality'].steps);
  collapsed.jobs['test-portal-identity'].steps.push(...collapsed.jobs['test-combined-runtime'].steps);
  delete collapsed.jobs['test-portal-quality'];
  delete collapsed.jobs['test-combined-runtime'];
  assert.equal(delayedSetup(collapsed).timeout, true);
  assert.deepEqual(delayedSetup(workflow), {
    attempted: ['test-portal-quality', 'test-portal-identity', 'test-combined-runtime'], published: true, timeout: false,
  });
});

test('a genuine observer failure still stops the dependent package in the bounded fault model', () => {
  assert.deepEqual(delayedSetup(workflow, 'test-portal-quality'), {
    attempted: ['test-portal-quality'], published: false, timeout: false,
  });
  assert.deepEqual(delayedSetup(workflow, 'test-portal-identity'), {
    attempted: ['test-portal-quality', 'test-portal-identity'], published: false, timeout: false,
  });
});

for (const [name, mutate] of [
  ['missing quality stage', w => { delete w.jobs['test-portal-quality']; }],
  ['conditional quality stage', w => { w.jobs['test-portal-quality'].if = 'false'; }],
  ['ignored quality failure', w => { w.jobs['test-portal-quality']['continue-on-error'] = true; }],
  ['missing quality dependency', w => { w.jobs['test-combined-runtime'].needs = ['test-portal-identity']; }],
  ['extended quality budget', w => { w.jobs['test-portal-quality']['timeout-minutes'] = 15; }],
  ['quality database coupling', w => { w.jobs['test-portal-quality'].services = {postgres: {}}; }],
  ['frontend setup inside database stage', w => { w.jobs['test-portal-identity'].steps.push({run: 'npm ci'}); }],
  ['quality waits for database tests', w => { w.jobs['test-portal-quality'].needs = ['test-portal-identity']; }],
  ['missing package stage', w => { delete w.jobs['test-combined-runtime']; }],
  ['conditional package stage', w => { w.jobs['test-combined-runtime'].if = 'false'; }],
  ['ignored package stage failure', w => { w.jobs['test-combined-runtime']['continue-on-error'] = true; }],
  ['missing identity dependency', w => { w.jobs['test-combined-runtime'].needs = []; }],
  ['extended identity budget', w => { w.jobs['test-portal-identity']['timeout-minutes'] = 15; }],
  ['extended package budget', w => { w.jobs['test-combined-runtime']['timeout-minutes'] = 15; }],
  ['package writes permission', w => { w.jobs['test-combined-runtime'].permissions.packages = 'write'; }],
  ['incomplete compiler cache key', w => { w.jobs['test-combined-runtime'].steps.find(s => s.with?.cache === 'npm').with['cache-dependency-path'] = 'package-lock.json'; }],
  ['shallow package checkout', w => { w.jobs['test-combined-runtime'].steps.find(s => s.uses?.startsWith('actions/checkout@')).with['fetch-depth'] = 1; }],
  ['missing renderer startup', w => { w.jobs['test-combined-runtime'].steps = w.jobs['test-combined-runtime'].steps.filter(s => s.id !== 'tenant-renderer'); }],
  ['conditional renderer startup', w => { w.jobs['test-combined-runtime'].steps.find(s => s.id === 'tenant-renderer').if = 'false'; }],
  ['ignored renderer startup failure', w => { w.jobs['test-combined-runtime'].steps.find(s => s.id === 'tenant-renderer')['continue-on-error'] = true; }],
  ['masked renderer startup failure', w => { w.jobs['test-combined-runtime'].steps.find(s => s.id === 'tenant-renderer').run += ' || true'; }],
  ['extended renderer startup budget', w => { w.jobs['test-combined-runtime'].steps.find(s => s.id === 'tenant-renderer')['timeout-minutes'] = 10; }],
  ['renderer startup after package assertions', w => {
    const runtime = w.jobs['test-combined-runtime'];
    const setup = runtime.steps.find(s => s.id === 'tenant-renderer');
    runtime.steps = runtime.steps.filter(s => s.id !== 'tenant-renderer');
    runtime.steps.push(setup);
  }],
  ...['validation', 'status'].flatMap(gate => ['test-portal-quality', 'test-portal-identity', 'test-combined-runtime'].flatMap(stage => [
    [`${gate} omits ${stage}`, w => { w.jobs[gate].needs = w.jobs[gate].needs.filter(n => n !== stage); }],
    [`${gate} ignores ${stage} result`, w => { w.jobs[gate].steps[0].with['job-results'] = w.jobs[gate].steps[0].with['job-results'].replace('${{ needs.' + stage + '.result }}', 'success'); }],
  ])),
  ...['npm ci', 'npm --prefix web ci'].flatMap(command => [
    [`conditional ${command}`, w => { w.jobs['test-combined-runtime'].steps.find(s => s.run === command).if = 'false'; }],
    [`ignored ${command} failure`, w => { w.jobs['test-combined-runtime'].steps.find(s => s.run === command)['continue-on-error'] = true; }],
  ]),
]) {
  test(`stage boundary rejects ${name}`, () => {
    const changed = structuredClone(workflow);
    mutate(changed);
    assert.throws(() => assertStages(changed));
  });
}
