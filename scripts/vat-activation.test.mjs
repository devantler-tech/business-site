import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
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
