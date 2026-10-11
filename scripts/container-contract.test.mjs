import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

// Exercise the README's real shell selection/cd/argument handling. A Docker
// boundary double avoids requiring a daemon for these documentation checks;
// container-smoke.test.mjs separately exercises the actual image in Linux CI.
const examples = [...readFileSync('server/README.md', 'utf8').matchAll(/```sh\n([\s\S]*?)\n```/g)].map(match => match[1]);
const packageExample = examples.find(example => example.includes('./business-site --version'));
const containerExample = examples.find(example => example.includes('docker build --file Dockerfile'));

for (const [name, example] of [['package', packageExample], ['container', containerExample]]) {
  for (const directories of [[], ['package.one'], ['package.with spaces'], ['package.one', 'package.two']]) {
    test(`${name} instructions handle ${directories.length ? directories.join(', ') : 'no generated package'}`, () => {
      const fixture = realpathSync(mkdtempSync(path.join(tmpdir(), 'runtime-readme-')));
      try {
        mkdirSync(path.join(fixture, 'runtime-dist'));
        for (const directory of directories) mkdirSync(path.join(fixture, 'runtime-dist', directory));
        const selection = name === 'package'
          ? example.split('node scripts/runtime-smoke.test.mjs runtime-dist\n')[1]
            .replace('./business-site --version', 'pwd -P')
            .replace('./business-site --listen 127.0.0.1:8080', ':')
          : example.split('\nnode scripts/container-smoke.test.mjs')[0];
        const capture = name === 'container' ? 'docker() { printf "%s\\0" "$@"; }\ngit() { printf "%s" fixture-revision; }\n' : '';
        const result = spawnSync('bash', ['-c', capture + selection], { cwd: fixture, encoding: 'utf8', timeout: 5000 });
        if (directories.length !== 1) {
          assert.notEqual(result.status, 0, 'Missing or ambiguous package selection must stop.');
          assert.equal(result.stdout, '', 'A rejected package must not reach execution/build.');
        } else {
          assert.equal(result.status, 0, result.stderr);
          if (name === 'package') {
            assert.equal(result.stdout.trim(), path.join(fixture, 'runtime-dist', directories[0]));
          } else {
            assert.deepEqual(result.stdout.split('\0').slice(0, -1), [
              'build', '--file', 'Dockerfile', '--label',
              'org.opencontainers.image.source=https://github.com/devantler-tech/business-site',
              '--label', 'org.opencontainers.image.revision=fixture-revision',
              '--tag', 'business-site:test', `runtime-dist/${directories[0]}`,
            ]);
          }
        }
      } finally {
        rmSync(fixture, { recursive: true, force: true });
      }
    });
  }
}

test('a confined non-root image is supplied for the actual runtime package', () => {
  assert.ok(existsSync('Dockerfile'), 'The combined runtime has no container definition.');
  const file = readFileSync('Dockerfile', 'utf8');
  assert.match(file, /^FROM gcr\.io\/distroless\/static-debian13:nonroot@sha256:[a-f0-9]{64}$/m);
  assert.match(file, /^USER 65532:65532$/m);
  assert.match(file, /^COPY --chown=65532:65532 business-site build-receipt\.json README\.md \/app\/$/m);
  assert.match(file, /^COPY --chown=65532:65532 public\/ \/app\/public\/$/m);
  assert.match(file, /^COPY --chown=65532:65532 notices\/ \/app\/notices\/$/m);
  assert.doesNotMatch(file, /^COPY .*\. \/app\//m);
  assert.match(file, /^ENTRYPOINT \["\/app\/business-site"\]$/m);
  assert.match(file, /^CMD \["--public", "\/app\/public", "--listen", "0\.0\.0\.0:8080"\]$/m);
  assert.doesNotMatch(file, /^RUN |^ADD |^ENV PORTAL_/m);
});

/** Reject skipped image validation, broad artifacts or incomplete success aggregation. */
function assertCI(w) {
  assert.equal(w.jobs['test-portal-identity'].if, undefined);
  const steps = w.jobs['test-portal-identity'].steps;
  const build = steps.findIndex(s => s.run?.includes('docker build --file Dockerfile'));
  const smoke = steps.findIndex(s => s.run === 'node scripts/container-smoke.test.mjs business-site:test runtime-dist');
  const archive = steps.findIndex(s => s.run?.includes('docker save business-site:test'));
  const upload = steps.findIndex(s => s.with?.name === 'business-site-container');
  assert.ok(build >= 0 && smoke > build && archive > smoke && upload > archive, 'Missing actual container build/test/archive chain.');
  for (const i of [build, smoke, archive, upload]) {
    assert.equal(steps[i].if, undefined);
    assert.ok(steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false);
  }
  assert.equal(steps[upload].with.path, 'runtime-dist/container');
  assert.equal(steps[upload].with['if-no-files-found'], 'error');
  const publish = w.jobs['publish-container'];
  assert.ok(publish, 'Publication must be inside the successful CI chain, not a privileged workflow_run.');
  assert.deepEqual(publish.needs, ['validation', 'test-portal-identity']);
  assert.ok(w.jobs.status.needs.includes('publish-container'), 'Final required checks must include publication.');
  assert.ok(w.jobs.status.needs.includes('validation'), 'Final required checks must include prepublication validation.');
  assert.deepEqual(w.jobs.validation.needs, w.jobs.status.needs.filter(n => !['publish-container', 'validation'].includes(n)));
  assert.equal(w.jobs.validation.if, 'always()');
  assert.match(w.jobs.validation.steps[0].uses, /^devantler-tech\/\.github\/actions\/aggregate-job-checks@[0-9a-f]{40}$/);
  assert.equal(w.jobs.validation.steps.length, 1);
  assert.equal(w.jobs.validation.steps[0].if, undefined);
  assert.ok(w.jobs.validation.steps[0]['continue-on-error'] == null || w.jobs.validation.steps[0]['continue-on-error'] === false);
  assert.deepEqual(w.jobs.validation.steps[0].with['job-results'].trim().split(/\s+(?=\$\{\{)/), w.jobs.validation.needs.map(n => '${{ needs.' + n + '.result }}'));
  assert.equal(publish.if.replace(/\s+/g, ' ').trim(), "github.event_name == 'push' && github.ref == 'refs/heads/main' && github.repository == 'devantler-tech/business-site'");
  assert.equal(publish.uses, undefined);
  assert.ok(Array.isArray(publish.steps), 'The main-only publisher must be a native CI job.');
  assert.deepEqual(publish.permissions, { contents: 'read', actions: 'read', packages: 'write', 'id-token': 'write' });
  assert.equal(publish.secrets, undefined);
}

test('CI must exercise and retain the actual container before distribution', async () => {
  const yaml = await import('js-yaml');
  const w = yaml.load(readFileSync('.github/workflows/ci.yaml', 'utf8'));
  assertCI(w);
  for (const mutate of [
    w => { w.jobs['test-portal-identity'].if = 'false'; },
    w => { w.jobs['test-portal-identity'].steps.find(s => s.run?.includes('docker build --file Dockerfile')).if = 'false'; },
    w => { w.jobs['test-portal-identity'].steps.find(s => s.run?.includes('node scripts/container-smoke.test.mjs'))['continue-on-error'] = true; },
    w => { w.jobs['test-portal-identity'].steps.find(s => s.with?.name === 'business-site-container').with.path = '.'; },
    w => { w.jobs['publish-container'].needs = ['test-portal-identity']; },
    w => { w.jobs.status.needs = w.jobs.status.needs.filter(n => n !== 'publish-container'); },
    w => { w.jobs.validation.needs = w.jobs.validation.needs.filter(n => n !== 'test-portal-identity'); },
    w => { w.jobs.validation.steps[0]['continue-on-error'] = true; },
    w => { w.jobs.validation.steps[0].if = 'false'; },
    w => { w.jobs.validation.steps[0].with['job-results'] = 'success'; },
    w => { w.jobs['publish-container'].if = `true || (${w.jobs['publish-container'].if})`; },
    w => { w.jobs['publish-container'].secrets = 'inherit'; },
  ]) {
    const changed = structuredClone(w); mutate(changed); assert.throws(() => assertCI(changed));
  }
});

/** Bind publication to this main-push CI artifact and require verified-image execution. */
function assertPublisher(w) {
  assert.equal(w.on.workflow_run, undefined);
  assert.equal(w.on.pull_request_target, undefined);
  assert.deepEqual(w.permissions, {});
  const j = w.jobs['publish-container'];
  assert.equal(j.if.replace(/\s+/g, ' ').trim(), "github.event_name == 'push' && github.ref == 'refs/heads/main' && github.repository == 'devantler-tech/business-site'");
  assert.equal(j.env.SOURCE_SHA, '${{ github.sha }}');
  assert.equal(j.env.CI_RUN_ID, '${{ github.run_id }}');
  assert.equal(j.env.CI_RUN_ATTEMPT, '${{ github.run_attempt }}');
  assert.deepEqual(j.permissions, { contents: 'read', actions: 'read', packages: 'write', 'id-token': 'write' });
  const steps = j.steps;
  assert.equal(steps.find(s => s.uses?.startsWith('actions/checkout@')).with.ref, '${{ github.sha }}');
  const download = steps.find(s => s.uses?.startsWith('actions/download-artifact@'));
  assert.equal(download.with['run-id'], '${{ github.run_id }}');
  assert.equal(download.with.name, 'business-site-container');
  const exercise = steps.findIndex(s => s.run?.includes('node scripts/container-smoke.test.mjs business-site:test runtime-dist'));
  const publish = steps.findIndex(s => s.run?.includes('docker push'));
  const verify = steps.findIndex(s => s.run?.includes('cosign verify '));
  const publishedSmoke = steps.findIndex(s => s.run?.includes('node scripts/container-smoke.test.mjs "$PUBLISHED_IMAGE" runtime-dist'));
  assert.ok(exercise >= 0 && publish > exercise && verify > publish && publishedSmoke > verify);
  for (const i of [exercise, publish, verify, publishedSmoke]) {
    assert.equal(steps[i].if, undefined);
    assert.ok(steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false);
  }
}

test('publication accepts only successful same-repository main push CI and tests its exact image', async () => {
  const yaml = await import('js-yaml');
  const w = yaml.load(readFileSync('.github/workflows/ci.yaml', 'utf8'));
  assertPublisher(w);
  for (const mutate of [
    w => { w.jobs['publish-container'].if = `true || (${w.jobs['publish-container'].if})`; },
    w => { w.on = { workflow_run: { workflows: ['CI'], types: ['completed'], branches: ['main'] } }; },
    w => { w.jobs['publish-container'].steps.find(s => s.uses?.startsWith('actions/checkout@')).with.ref = 'main'; },
    w => { delete w.jobs['publish-container'].steps.find(s => s.uses?.startsWith('actions/download-artifact@')).with['run-id']; },
    w => { w.jobs['publish-container'].steps.find(s => s.run?.includes('cosign verify '))['continue-on-error'] = true; },
    w => { w.jobs['publish-container'].steps.find(s => s.run?.includes('node scripts/container-smoke.test.mjs "$PUBLISHED_IMAGE"')).if = 'false'; },
  ]) {
    const changed = structuredClone(w); mutate(changed); assert.throws(() => assertPublisher(changed));
  }
  const source = readFileSync('.github/workflows/ci.yaml', 'utf8');
  assert.match(source, /cosign attest --yes/);
  assert.match(source, /cosign verify-attestation/);
  assert.doesNotMatch(source, /:latest|pull_request_target|secrets\.(?!GITHUB_TOKEN)/);
});
