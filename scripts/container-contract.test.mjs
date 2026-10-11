import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

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
  ]) {
    const changed = structuredClone(w); mutate(changed); assert.throws(() => assertCI(changed));
  }
});

function assertPublisher(w) {
  assert.deepEqual(w.on, { workflow_run: { workflows: ['CI'], types: ['completed'], branches: ['main'] } });
  assert.deepEqual(w.permissions, {});
  const j = w.jobs.publish;
  assert.equal(j.if.replace(/\s+/g, ' ').trim(), "github.event.workflow_run.conclusion == 'success' && github.event.workflow_run.event == 'push' && github.event.workflow_run.head_branch == 'main' && github.event.workflow_run.head_repository.full_name == github.repository");
  assert.deepEqual(j.permissions, { contents: 'read', actions: 'read', packages: 'write', 'id-token': 'write' });
  const steps = j.steps;
  assert.equal(steps.find(s => s.uses?.startsWith('actions/checkout@')).with.ref, '${{ github.event.workflow_run.head_sha }}');
  const download = steps.find(s => s.uses?.startsWith('actions/download-artifact@'));
  assert.equal(download.with['run-id'], '${{ github.event.workflow_run.id }}');
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
  assert.ok(existsSync('.github/workflows/publish-container.yaml'), 'There is no source-owned image publisher.');
  const yaml = await import('js-yaml');
  const w = yaml.load(readFileSync('.github/workflows/publish-container.yaml', 'utf8'));
  assertPublisher(w);
  for (const mutate of [
    w => { w.jobs.publish.if = `true || (${w.jobs.publish.if})`; },
    w => { w.jobs.publish.steps.find(s => s.uses?.startsWith('actions/checkout@')).with.ref = 'main'; },
    w => { delete w.jobs.publish.steps.find(s => s.uses?.startsWith('actions/download-artifact@')).with['run-id']; },
    w => { w.jobs.publish.steps.find(s => s.run?.includes('cosign verify '))['continue-on-error'] = true; },
    w => { w.jobs.publish.steps.find(s => s.run?.includes('node scripts/container-smoke.test.mjs "$PUBLISHED_IMAGE"')).if = 'false'; },
  ]) {
    const changed = structuredClone(w); mutate(changed); assert.throws(() => assertPublisher(changed));
  }
  const source = readFileSync('.github/workflows/publish-container.yaml', 'utf8');
  assert.match(source, /cosign attest --yes/);
  assert.match(source, /cosign verify-attestation/);
  assert.doesNotMatch(source, /:latest|pull_request_target|secrets\.(?!GITHUB_TOKEN)/);
});
