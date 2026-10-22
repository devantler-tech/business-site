import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { readFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { load } from 'js-yaml';

const root = process.cwd();
const packages = {
  htmlhint: '1.9.2',
  secretlint: '13.0.5',
  '@secretlint/secretlint-rule-preset-recommend': '13.0.5',
  cspell: '10.1.1',
};

const manifest = JSON.parse(readFileSync('package.json'));
const lock = JSON.parse(readFileSync('package-lock.json'));
const workflow = load(readFileSync('.github/workflows/ci.yaml', 'utf8'));

function assertProvisioning(manifest, lock, workflow) {
  const steps = workflow.jobs['test-portal-identity'].steps;
  const install = steps.findIndex(s => s.run === 'npm ci');
  assert.ok(install >= 0);
  const witness = steps.findIndex(s => s.run === 'node --test scripts/portal-quality-provisioning.test.mjs');
  assert.ok(witness > install);
  for (const index of [install, witness]) {
    assert.equal(steps[index]['working-directory'], '.');
    assert.equal(steps[index].if, undefined);
    assert.ok(steps[index]['continue-on-error'] == null || steps[index]['continue-on-error'] === false);
  }
  for (const [name, version] of Object.entries(packages)) {
    assert.equal(manifest.devDependencies[name], version, `${name} must not provision in npx`);
    assert.equal(lock.packages[''].devDependencies[name], version);
    const entry = lock.packages[`node_modules/${name}`];
    assert.equal(entry.version, version);
    assert.match(entry.integrity, /^sha512-/);
    const check = steps.findIndex(s => s.run?.includes(`--package=${name}@${version}`));
    assert.ok(check > install, `${name}'s actual pinned checker must remain after npm ci`);
  }
}

test('portal checker provisioning is exact and part of the existing clean install', () => assertProvisioning(manifest, lock, workflow));

for (const [name, mutate] of [
  ['missing checker', (m) => { delete m.devDependencies.htmlhint; }],
  ['floating checker', (m) => { m.devDependencies.htmlhint = '^1.9.2'; }],
  ['stale root lock', (_, l) => { l.packages[''].devDependencies.htmlhint = '1.9.1'; }],
  ['stale installed lock', (_, l) => { l.packages['node_modules/htmlhint'].version = '1.9.1'; }],
  ['missing integrity', (_, l) => { delete l.packages['node_modules/htmlhint'].integrity; }],
  ['missing fault control', (_, __, w) => { w.jobs['test-portal-identity'].steps = w.jobs['test-portal-identity'].steps.filter(s => !s.run?.includes('portal-quality-provisioning')); }],
  ['conditional fault control', (_, __, w) => { w.jobs['test-portal-identity'].steps.find(s => s.run?.includes('portal-quality-provisioning')).if = 'false'; }],
  ['ignored fault control', (_, __, w) => { w.jobs['test-portal-identity'].steps.find(s => s.run?.includes('portal-quality-provisioning'))['continue-on-error'] = true; }],
  ['wrong fault-control directory', (_, __, w) => { delete w.jobs['test-portal-identity'].steps.find(s => s.run?.includes('portal-quality-provisioning'))['working-directory']; }],
]) {
  test(`locked provisioning rejects ${name}`, () => {
    const inputs = [manifest, lock, workflow].map(value => structuredClone(value));
    mutate(...inputs);
    assert.throws(() => assertProvisioning(...inputs));
  });
}

// Real npm/npx against a deliberately stalled loopback registry. This isolates
// resolution latency from checker execution; it is not a registry outage claim
// or a cold-install benchmark. A clean install can still require the registry.
test('installed exact checkers need no registry resolution and retain observer failures', { timeout: 45000 }, async () => {
  const scratch = await mkdtemp(join(tmpdir(), 'portal-quality-provisioning-'));
  const requests = [];
  let onRequest;
  const sockets = new Set();
  const server = createServer((request) => {
    requests.push(request.url);
    onRequest?.(); // Deliberately never respond, until fixture cleanup.
  });
  server.on('connection', socket => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const registry = `http://127.0.0.1:${server.address().port}/`;
  const run = (cwd, specifications, status = 0) => {
    const child = spawn('npx', ['--yes', `--registry=${registry}`, `--cache=${join(scratch, 'cache')}`,
      ...specifications.map(spec => `--package=${spec}`), '--', 'node', '-e', `process.exit(${status})`], {
      cwd, env: { ...process.env, NPM_CONFIG_USERCONFIG: join(scratch, 'npmrc'), NPM_CONFIG_UPDATE_NOTIFIER: 'false' },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let output = '';
    child.stdout.on('data', data => { output += data; });
    child.stderr.on('data', data => { output += data; });
    const timer = setTimeout(() => child.kill('SIGTERM'), 8000);
    const result = new Promise((resolve, reject) => {
      child.on('error', reject);
      child.on('close', (code, signal) => { clearTimeout(timer); resolve({ code, signal, output }); });
    });
    return { child, result };
  };
  try {
    await writeFile(join(scratch, 'package.json'), '{"name":"unprovisioned-checker-control","private":true}');
    await writeFile(join(scratch, 'npmrc'), '');
    let resolveRequested;
    const requested = new Promise(resolve => { resolveRequested = resolve; });
    onRequest = resolveRequested;
    const missing = run(scratch, ['htmlhint@1.9.2']);
    try {
      await Promise.race([requested, missing.result.then(result => {
        throw new Error(`unprovisioned control did not reach stalled registry: ${JSON.stringify(result)}`);
      })]);
      assert.ok(requests.some(path => path.startsWith('/htmlhint')), JSON.stringify(requests));
    } finally {
      missing.child.kill('SIGTERM');
      await missing.result;
      for (const socket of sockets) socket.destroy();
    }
    onRequest = undefined;
    const before = requests.length;
    for (const specifications of [
      ['htmlhint@1.9.2'],
      ['secretlint@13.0.5', '@secretlint/secretlint-rule-preset-recommend@13.0.5'],
      ['cspell@10.1.1'],
    ]) {
      assert.deepEqual(await run(root, specifications).result, { code: 0, signal: null, output: '' });
      const failure = await run(root, specifications, 17).result;
      assert.equal(failure.code, 17, failure.output);
      assert.equal(failure.signal, null);
    }
    assert.equal(requests.length, before, 'no registry resolution after exact locked provisioning');
  } finally {
    for (const socket of sockets) socket.destroy();
    await new Promise(resolve => server.close(resolve));
    await rm(scratch, { recursive: true, force: true });
  }
});
