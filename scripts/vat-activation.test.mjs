import assert from 'node:assert/strict';
import { readFileSync, mkdtempSync, mkdirSync, writeFileSync, rmSync, cpSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { load } from 'js-yaml';

const ci = load(readFileSync('.github/workflows/ci.yaml', 'utf8'));
// The served production image is CI's production-template package. Its packager,
// not the workflow, selects the presentation, so CI passes none of these settings.
const presentation = ['FEATURE_COMPANY_IDENTITY', 'FEATURE_OFFER_COPY', 'FEATURE_PORTAL_PRIVACY', 'FEATURE_VAT_PRICES', 'FEATURE_CUSTOMER_SCOPE'];
const runtimeJob = workflow => workflow.jobs?.['test-combined-runtime'];
const productionPackages = workflow => runtimeJob(workflow)?.steps?.filter(step => step.run === 'bash scripts/package-runtime.sh --release --production-template') ?? [];
const currentRuntimeCI = workflow => {
  const packages = productionPackages(workflow);
  return packages.length === 1 && [workflow.env, runtimeJob(workflow)?.env, packages[0].env]
    .every(env => presentation.every(flag => !Object.hasOwn(env ?? {}, flag)));
};
const current = state => state.FEATURE_COMPANY_IDENTITY === 'true' && state.FEATURE_OFFER_COPY === 'false' && state.FEATURE_PORTAL_PRIVACY === 'false' &&
  !Object.hasOwn(state, 'FEATURE_VAT_PRICES') && !Object.hasOwn(state, 'FEATURE_CUSTOMER_SCOPE');
const packagerSource = readFileSync('scripts/package-runtime.sh', 'utf8');
/** Run the actual (or a changed) packager; the build double records every FEATURE_ setting it receives. */
function productionPresentation(packager = packagerSource, inherited = {}) {
  const root = mkdtempSync(join(tmpdir(), 'vat-production-'));
  try {
    const go = packagerSource.match(/go version go([0-9.]+) /)?.[1];
    assert.ok(go, 'the packager names its exact Go toolchain');
    const node = readFileSync('.node-version', 'utf8').trim();
    mkdirSync(join(root, 'scripts')); mkdirSync(join(root, 'bin'));
    writeFileSync(join(root, 'scripts', 'package-runtime.sh'), packager);
    cpSync('.node-version', join(root, '.node-version'));
    writeFileSync(join(root, 'scripts', 'refresh-public-stars.sh'), '#!/bin/sh\nprintf "{}" > "$1"\n');
    writeFileSync(join(root, 'bin', 'git'), `#!/bin/sh\nprintf '%s\\n' ${'a'.repeat(40)}\n`, { mode: 0o700 });
    writeFileSync(join(root, 'bin', 'node'), `#!/bin/sh\nprintf 'v${node}.0.0\\n'\n`, { mode: 0o700 });
    writeFileSync(join(root, 'bin', 'go'), `#!/bin/sh\nprintf 'go version go${go} linux/amd64\\n'\n`, { mode: 0o700 });
    writeFileSync(join(root, 'bin', 'npm'), '#!/bin/bash\nif [[ "$1" == --version ]]; then printf \'11.0.0\\n\'; exit 0; fi\n[[ "$1 $2" == "run build" ]] || exit 41\nenv | grep \'^FEATURE_\' | sort > "$CAPTURE"\nexit 42\n', { mode: 0o700 });
    const env = { ...process.env };
    for (const key of Object.keys(env).filter(key => key.startsWith('FEATURE_'))) delete env[key];
    const capture = join(root, 'captured');
    const result = spawnSync('bash', ['scripts/package-runtime.sh', '--development', '--production-template'], {
      cwd: root, timeout: 10000, encoding: 'utf8', env: { ...env, ...inherited, PATH: `${root}/bin:${env.PATH}`, CAPTURE: capture },
    });
    assert.equal(result.status, 42, `the build double stops at the real production build: ${result.stderr}`);
    return Object.fromEntries(readFileSync(capture, 'utf8').split('\n').filter(Boolean).map(line => [line.slice(0, line.indexOf('=')), line.slice(line.indexOf('=') + 1)]));
  } finally { rmSync(root, { recursive: true, force: true }); }
}

const currentCI = workflow => {
  const builds = workflow.jobs?.['build-docs']?.steps?.filter(step => step.run === 'npm run build' && step.id === 'current-production') ?? [];
  return builds.length === 1 && builds[0]['working-directory'] === '.' && builds[0].if == null &&
    (builds[0]['continue-on-error'] == null || builds[0]['continue-on-error'] === false) && !Object.hasOwn(builds[0].env, 'FEATURE_VAT_PRICES') &&
    builds[0].env.FEATURE_COMPANY_IDENTITY === 'true' &&
    builds[0].env.FEATURE_OFFER_COPY === 'false' && !Object.hasOwn(builds[0].env, 'FEATURE_CUSTOMER_SCOPE') &&
    typeof builds[0].env.COMPANY_POSTAL_ADDRESS === 'string' && builds[0].env.COMPANY_POSTAL_ADDRESS.split('\n').length === 2;
};

test('actual CI runs the complete current-production tuple with no skipped failures', () => assert.ok(currentCI(ci)));

const productionPreview = workflow => {
  const steps = workflow.jobs?.['build-docs']?.steps ?? [];
  const [build, boundary, upload] = steps.slice(-3);
  const required = step => step && step.if == null && (step['continue-on-error'] == null || step['continue-on-error'] === false);
  return currentCI(workflow) && build?.run === 'npm run build' && build.id === 'current-production' &&
    boundary?.run === 'bash scripts/check-portal-artifacts.sh dist' &&
    /^actions\/upload-artifact@[a-f0-9]{40}$/.test(upload?.uses ?? '') &&
    upload.with?.name === 'business-site-preview' && upload.with.path === 'dist' &&
    upload.with['if-no-files-found'] === 'error' && [build, boundary, upload].every(required);
};
test('the review artifact is the final checked current-production output', () => assert.ok(productionPreview(ci)));

test('executing actual CI producer commands leaves the production tuple for upload', () => {
  const directory = mkdtempSync(join(tmpdir(), 'vat-preview-order-'));
  try {
    mkdirSync(join(directory, 'bin'));
    writeFileSync(join(directory, 'bin', 'npm'), '#!/bin/sh\n[ "$1 $2" = "run build" ] || exit 23\nmkdir -p dist\nprintf "%s\\n" "${FEATURE_COMPANY_IDENTITY-}" "${FEATURE_OFFER_COPY-}" > dist/presentation\n', { mode: 0o700 });
    for (const step of ci.jobs['build-docs'].steps.filter(step => /^(?:npm run build|env .* npm run build)$/.test(step.run ?? ''))) {
      const env = { ...process.env, PATH: `${directory}/bin:${process.env.PATH}` };
      for (const key of Object.keys(env).filter(key => key.startsWith('FEATURE_'))) delete env[key];
      const result = spawnSync('bash', ['-eo', 'pipefail', '-c', step.run], {
        cwd: directory, env: { ...env, ...step.env }, encoding: 'utf8', timeout: 10000,
      });
      assert.ifError(result.error);
      assert.equal(result.status, 0, result.stderr);
    }
    assert.equal(readFileSync(join(directory, 'dist', 'presentation'), 'utf8'), 'true\nfalse\n');
  } finally { rmSync(directory, { recursive: true, force: true }); }
});

for (const mode of ['late-overwrite', 'wrong-artifact', 'skipped-upload', 'ignored-upload', 'skipped-boundary', 'wrong-boundary', 'duplicate-upload']) {
  test(`production-preview lineage refuses ${mode}`, () => {
    const changed = structuredClone(ci), steps = changed.jobs['build-docs'].steps;
    const upload = steps.find(step => step.with?.name === 'business-site-preview');
    const boundary = steps.find(step => step.run === 'bash scripts/check-portal-artifacts.sh dist');
    if (mode === 'late-overwrite') steps.splice(steps.indexOf(upload), 0, { run: 'npm run build' });
    if (mode === 'wrong-artifact') upload.with.path = 'web/build';
    if (mode === 'skipped-upload') upload.if = 'false';
    if (mode === 'ignored-upload') upload['continue-on-error'] = true;
    if (mode === 'skipped-boundary') boundary.if = 'false';
    if (mode === 'wrong-boundary') boundary.run = 'bash scripts/check-portal-artifacts.sh web/build';
    if (mode === 'duplicate-upload') steps.push(structuredClone(upload));
    assert.equal(productionPreview(changed), false);
  });
}
for (const mode of ['missing', 'duplicate', 'skipped', 'ignored', 'no-address', 'wrong-directory', 'legacy-vat', 'revised-offer', 'stale-audience']) {
  test(`current-production CI probe refuses ${mode}`, () => {
    const changed = structuredClone(ci), steps = changed.jobs['build-docs'].steps;
    const build = steps.find(step => step.id === 'current-production');
    if (mode === 'missing') steps.splice(steps.indexOf(build), 1);
    if (mode === 'duplicate') steps.push(structuredClone(build));
    if (mode === 'skipped') build.if = 'false';
    if (mode === 'ignored') build['continue-on-error'] = true;
    if (mode === 'no-address') delete build.env.COMPANY_POSTAL_ADDRESS;
    if (mode === 'wrong-directory') build['working-directory'] = 'web';
    if (mode === 'legacy-vat') build.env.FEATURE_VAT_PRICES = 'false';
    if (mode === 'revised-offer') build.env.FEATURE_OFFER_COPY = 'true';
    if (mode === 'stale-audience') build.env.FEATURE_CUSTOMER_SCOPE = 'false';
    assert.equal(currentCI(changed), false);
  });
}

test('the actual production runtime packager selects confirmed VAT, identity and customer invitation', () => {
  assert.ok(currentRuntimeCI(ci), 'CI builds the production package once and passes it no presentation setting');
  assert.ok(current(productionPresentation()));
  assert.ok(current(productionPresentation(packagerSource, { FEATURE_COMPANY_IDENTITY: 'false', FEATURE_OFFER_COPY: 'true', FEATURE_PORTAL_PRIVACY: 'true' })),
    'inherited settings cannot change the production presentation');
});
for (const flag of presentation) {
  for (const scope of ['workflow', 'job', 'step']) {
    test(`production CI refuses ${flag} passed at ${scope} level`, () => {
      const changed = structuredClone(ci);
      const owner = scope === 'workflow' ? changed : scope === 'job' ? runtimeJob(changed) : productionPackages(changed)[0];
      owner.env = { ...owner.env, [flag]: 'false' };
      assert.equal(currentRuntimeCI(changed), false);
    });
  }
}
test('production CI refuses a missing or duplicate production package', () => {
  const packaged = step => step.run === 'bash scripts/package-runtime.sh --release --production-template';
  for (const change of [steps => steps.filter(step => !packaged(step)), steps => steps.concat(steps.filter(packaged))]) {
    const changed = structuredClone(ci);
    runtimeJob(changed).steps = change(runtimeJob(changed).steps);
    assert.equal(currentRuntimeCI(changed), false);
  }
});
const production = '"PUBLIC_CONTENT_PROFILE=production-template"';
for (const [mode, from, to] of [
  ['identity removed', ' "FEATURE_COMPANY_IDENTITY=true"', ''],
  ['ambiguous identity', '"FEATURE_COMPANY_IDENTITY=true"', '"FEATURE_COMPANY_IDENTITY=ambiguous"'],
  ['offer removed', ' "FEATURE_OFFER_COPY=false"', ''],
  ['revised offer', '"FEATURE_OFFER_COPY=false"', '"FEATURE_OFFER_COPY=true"'],
  ['ambiguous offer', '"FEATURE_OFFER_COPY=false"', '"FEATURE_OFFER_COPY=ambiguous"'],
  ['portal privacy unpinned', ' "FEATURE_PORTAL_PRIVACY=false"', ''],
  ['portal privacy released', '"FEATURE_PORTAL_PRIVACY=false"', '"FEATURE_PORTAL_PRIVACY=true"'],
  ['legacy VAT', production, `${production} "FEATURE_VAT_PRICES=false"`],
  ['stale audience', production, `${production} "FEATURE_CUSTOMER_SCOPE=false"`],
]) {
  test(`production packager refuses ${mode}`, () => {
    const changed = packagerSource.replace(from, to);
    assert.notEqual(changed, packagerSource, 'the control changes the actual packager');
    assert.equal(current(productionPresentation(changed)), false);
  });
}
