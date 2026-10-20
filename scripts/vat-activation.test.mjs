import assert from 'node:assert/strict';
import { readFileSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { load } from 'js-yaml';

const publisher = load(readFileSync('.github/workflows/publish-site.yaml', 'utf8'));
const ci = load(readFileSync('.github/workflows/ci.yaml', 'utf8'));
const selected = workflow => workflow.jobs?.build?.steps?.filter(step => step.run === 'npm run build') ?? [];
const current = workflow => {
  const builds = selected(workflow);
  return builds.length === 1 && builds[0].env?.FEATURE_COMPANY_IDENTITY === 'true' &&
    builds[0].env.FEATURE_VAT_PRICES === 'true' && builds[0].env.FEATURE_OFFER_COPY === 'false' &&
    builds[0].env.FEATURE_CUSTOMER_SCOPE === 'false';
};

const currentCI = workflow => {
  const builds = workflow.jobs?.['build-docs']?.steps?.filter(step => step.run === 'npm run build' && step.env?.FEATURE_VAT_PRICES === 'true') ?? [];
  return builds.length === 1 && builds[0]['working-directory'] === '.' && builds[0].if == null &&
    (builds[0]['continue-on-error'] == null || builds[0]['continue-on-error'] === false) &&
    builds[0].env.FEATURE_COMPANY_IDENTITY === 'true' && builds[0].env.FEATURE_SVELTE_FRONTEND === 'true' &&
    builds[0].env.FEATURE_OFFER_COPY === 'false' && builds[0].env.FEATURE_CUSTOMER_SCOPE === 'false' &&
    typeof builds[0].env.COMPANY_POSTAL_ADDRESS === 'string' && builds[0].env.COMPANY_POSTAL_ADDRESS.split('\n').length === 2;
};

test('actual CI runs the complete current-production tuple with no skipped failures', () => assert.ok(currentCI(ci)));

const productionPreview = workflow => {
  const steps = workflow.jobs?.['build-docs']?.steps ?? [];
  const [build, boundary, upload] = steps.slice(-3);
  const required = step => step && step.if == null && (step['continue-on-error'] == null || step['continue-on-error'] === false);
  return currentCI(workflow) && build?.run === 'npm run build' && build.env?.FEATURE_VAT_PRICES === 'true' &&
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
    writeFileSync(join(directory, 'bin', 'npm'), '#!/bin/sh\n[ "$1 $2" = "run build" ] || exit 23\nmkdir -p dist\nprintf "%s\\n" "${FEATURE_COMPANY_IDENTITY-}" "${FEATURE_VAT_PRICES-}" "${FEATURE_OFFER_COPY-}" "${FEATURE_CUSTOMER_SCOPE-}" "${FEATURE_SVELTE_FRONTEND-}" > dist/presentation\n', { mode: 0o700 });
    for (const step of ci.jobs['build-docs'].steps.filter(step => /^(?:npm run build|env .* npm run build)$/.test(step.run ?? ''))) {
      const env = { ...process.env, PATH: `${directory}/bin:${process.env.PATH}` };
      for (const key of Object.keys(env).filter(key => key.startsWith('FEATURE_'))) delete env[key];
      const result = spawnSync('bash', ['-eo', 'pipefail', '-c', step.run], {
        cwd: directory, env: { ...env, ...step.env }, encoding: 'utf8', timeout: 10000,
      });
      assert.ifError(result.error);
      assert.equal(result.status, 0, result.stderr);
    }
    assert.equal(readFileSync(join(directory, 'dist', 'presentation'), 'utf8'), 'true\ntrue\nfalse\nfalse\ntrue\n');
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
for (const mode of ['missing', 'duplicate', 'skipped', 'ignored', 'no-address', 'wrong-directory', 'legacy-vat', 'revised-offer', 'expanded-audience']) {
  test(`current-production CI probe refuses ${mode}`, () => {
    const changed = structuredClone(ci), steps = changed.jobs['build-docs'].steps;
    const build = steps.find(step => step.env?.FEATURE_VAT_PRICES === 'true');
    if (mode === 'missing') steps.splice(steps.indexOf(build), 1);
    if (mode === 'duplicate') steps.push(structuredClone(build));
    if (mode === 'skipped') build.if = 'false';
    if (mode === 'ignored') build['continue-on-error'] = true;
    if (mode === 'no-address') delete build.env.COMPANY_POSTAL_ADDRESS;
    if (mode === 'wrong-directory') build['working-directory'] = 'web';
    if (mode === 'legacy-vat') build.env.FEATURE_VAT_PRICES = 'false';
    if (mode === 'revised-offer') build.env.FEATURE_OFFER_COPY = 'true';
    if (mode === 'expanded-audience') build.env.FEATURE_CUSTOMER_SCOPE = 'true';
    assert.equal(currentCI(changed), false);
  });
}

test('the actual publisher selects only the confirmed VAT and identity release', () => assert.ok(current(publisher)));
for (const flag of ['FEATURE_COMPANY_IDENTITY', 'FEATURE_VAT_PRICES', 'FEATURE_OFFER_COPY', 'FEATURE_CUSTOMER_SCOPE']) {
  for (const value of [undefined, true, false, 'ambiguous', flag.includes('VAT') || flag.includes('IDENTITY') ? 'false' : 'true']) {
    test(`publisher refuses changed ${flag}=${String(value)}`, () => {
      const changed = structuredClone(publisher), env = selected(changed)[0].env;
      if (value === undefined) delete env[flag];
      else env[flag] = value;
      assert.equal(current(changed), false);
    });
  }
}
