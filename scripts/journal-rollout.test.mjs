import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const unsetCommand = 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_OFFER_COPY -u FEATURE_FEATURED_PORTFOLIO -u FEATURE_COMPANY_IDENTITY npm run build';
const checkCommand = 'node scripts/check-journal-presentation.mjs dist';
/** The final artifact must contain the checked Journal, after the production builds. */
const valid = (ci) => {
  const steps = ci.jobs?.['build-docs']?.steps ?? [];
  const indices = (predicate) => steps.flatMap((step, i) => predicate(step) ? [i] : []);
  const disabled = indices(step => step.env?.FEATURE_PREVIEW_BANNER === 'false' && step.run === 'npm run build');
  const unset = indices(step => step.run === unsetCommand && step.env == null);
  const checked = indices(step => step.run === checkCommand);
  const artifact = indices(step => step.with?.name === 'business-site-preview' && step.with.path === 'dist');
  return [disabled, unset, checked, artifact].every(group => group.length === 1) &&
    disabled[0] < unset[0] && unset[0] < checked[0] && checked[0] < artifact[0] &&
    [...disabled, ...unset, ...checked].every(i => steps[i]['working-directory'] === '.' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('actual CI verifies the final professional Journal before uploading production', () => assert.ok(valid(workflow)));
for (const mode of ['missing-false', 'missing-unset', 'missing-check', 'missing-artifact', 'skipped-check', 'ignored-check-failure', 'skipped-build', 'ignored-build-failure', 'check-before-production', 'check-after-artifact', 'wrong-directory', 'duplicate-check', 'configured-unset']) {
  test(`Journal publication rejects ${mode}`, () => {
    const ci = structuredClone(workflow);
    const steps = ci.jobs['build-docs'].steps;
    assert.ok(valid(ci), 'negative controls start from the validated actual workflow');
    const disabled = steps.find(step => step.env?.FEATURE_PREVIEW_BANNER === 'false' && step.run === 'npm run build');
    const unset = steps.find(step => step.run === unsetCommand);
    const checked = steps.find(step => step.run === checkCommand);
    const artifact = steps.find(step => step.with?.name === 'business-site-preview');
    if (mode === 'missing-false') steps.splice(steps.indexOf(disabled), 1);
    if (mode === 'missing-unset') steps.splice(steps.indexOf(unset), 1);
    if (mode === 'missing-check') steps.splice(steps.indexOf(checked), 1);
    if (mode === 'missing-artifact') steps.splice(steps.indexOf(artifact), 1);
    if (mode === 'skipped-check') checked.if = 'false';
    if (mode === 'ignored-check-failure') checked['continue-on-error'] = true;
    if (mode === 'skipped-build') disabled.if = 'false';
    if (mode === 'ignored-build-failure') unset['continue-on-error'] = true;
    if (mode === 'check-before-production') {
      steps.splice(steps.indexOf(checked), 1);
      steps.splice(steps.indexOf(unset), 0, checked);
    }
    if (mode === 'check-after-artifact') {
      steps.splice(steps.indexOf(checked), 1);
      steps.splice(steps.indexOf(artifact) + 1, 0, checked);
    }
    if (mode === 'wrong-directory') checked['working-directory'] = 'docs';
    if (mode === 'duplicate-check') steps.push(structuredClone(checked));
    if (mode === 'configured-unset') unset.env = { FEATURE_OFFER_COPY: 'true' };
    assert.equal(valid(ci), false);
  });
}
