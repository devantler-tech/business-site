import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const flag = 'FEATURE_CUSTOMER_SCOPE';
const unsetCommand = 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_OFFER_COPY -u FEATURE_FEATURED_PORTFOLIO -u FEATURE_COMPANY_IDENTITY -u FEATURE_CUSTOMER_SCOPE npm run build';
/** Require all release states to succeed before CI uploads the normal artifact. */
const valid = ci => {
  const job = ci.jobs?.['build-docs'];
  if (ci.env?.[flag] != null || job?.env?.[flag] != null) return false;
  const steps = job?.steps ?? [];
  const indices = predicate => steps.flatMap((step, i) => predicate(step) ? [i] : []);
  const enabled = indices(step => step.run === 'npm run build' && step.env?.[flag] === 'true' && step.name === 'Build customer-scope preview');
  // The current-production probe selects the separately reviewed activation.
  // Its complete tuple is guarded by vat-activation.test; retain this false case.
  const disabled = indices(step => step.run === 'npm run build' && step.env?.[flag] === 'false' && step.name === 'Build');
  const unset = indices(step => step.run === unsetCommand && step.env == null);
  const artifact = indices(step => step.with?.name === 'business-site-preview' && step.with.path === 'dist');
  return [enabled, disabled, unset, artifact].every(group => group.length === 1) &&
    enabled[0] < disabled[0] && disabled[0] < unset[0] && unset[0] < artifact[0] &&
    [...enabled, ...disabled, ...unset].every(i => steps[i]['working-directory'] === '.' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('customer scope is a typed default-off Astro release flag', () => {
  const config = readFileSync(new URL('../astro.config.mjs', import.meta.url), 'utf8');
  assert.match(config, /FEATURE_CUSTOMER_SCOPE:\s*envField\.boolean\(\{\s*context: "server",\s*access: "public",\s*default: false,?\s*\}\)/);
});
test('actual CI checks customer-scope enabled, false and entirely unset before artifact upload', () => assert.ok(valid(workflow)));
for (const mode of ['missing-enabled', 'missing-disabled', 'missing-unset', 'missing-artifact', 'skipped-enabled', 'ignored-disabled-failure', 'skipped-unset', 'ignored-unset-failure', 'inherited-job', 'inherited-workflow', 'configured-unset', 'wrong-directory', 'reverse', 'duplicate-enabled']) {
  test(`customer-scope rollout rejects ${mode}`, () => {
    const ci = structuredClone(workflow), job = ci.jobs['build-docs'], steps = job.steps;
    assert.ok(valid(ci));
    const enabled = steps.find(step => step.env?.[flag] === 'true');
    const disabled = steps.find(step => step.env?.[flag] === 'false' && step.name === 'Build');
    const unset = steps.find(step => step.run === unsetCommand);
    const artifact = steps.find(step => step.with?.name === 'business-site-preview');
    if (mode === 'missing-enabled') steps.splice(steps.indexOf(enabled), 1);
    if (mode === 'missing-disabled') steps.splice(steps.indexOf(disabled), 1);
    if (mode === 'missing-unset') steps.splice(steps.indexOf(unset), 1);
    if (mode === 'missing-artifact') steps.splice(steps.indexOf(artifact), 1);
    if (mode === 'skipped-enabled') enabled.if = 'false';
    if (mode === 'ignored-disabled-failure') disabled['continue-on-error'] = true;
    if (mode === 'skipped-unset') unset.if = 'false';
    if (mode === 'ignored-unset-failure') unset['continue-on-error'] = true;
    if (mode === 'inherited-job') job.env = { [flag]: 'true' };
    if (mode === 'inherited-workflow') ci.env = { [flag]: 'true' };
    if (mode === 'configured-unset') unset.env = { [flag]: 'true' };
    if (mode === 'wrong-directory') enabled['working-directory'] = 'web';
    if (mode === 'reverse') steps.reverse();
    if (mode === 'duplicate-enabled') steps.push(structuredClone(enabled));
    assert.equal(valid(ci), false);
  });
}
