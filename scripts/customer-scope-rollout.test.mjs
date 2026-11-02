import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const retired = 'FEATURE_CUSTOMER_SCOPE';
const unsetCommand = 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_OFFER_COPY -u FEATURE_FEATURED_PORTFOLIO -u FEATURE_COMPANY_IDENTITY npm run build';
/** Keep ordinary and entirely unset builds fail-closed after the invitation ships. */
const valid = ci => {
  const job = ci.jobs?.['build-docs'];
  if (ci.env?.[retired] != null || job?.env?.[retired] != null) return false;
  const steps = job?.steps ?? [];
  if (steps.some(step => step.env?.[retired] != null)) return false;
  const indices = predicate => steps.flatMap((step, i) => predicate(step) ? [i] : []);
  const normal = indices(step => step.run === 'npm run build' && step.name === 'Build');
  const unset = indices(step => step.run === unsetCommand && step.env == null);
  const artifact = indices(step => step.with?.name === 'business-site-preview' && step.with.path === 'dist');
  return [normal, unset, artifact].every(group => group.length === 1) &&
    normal[0] < unset[0] && unset[0] < artifact[0] &&
    [...normal, ...unset].every(i => steps[i]['working-directory'] === '.' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('shipped customer invitation has no active framework or component release gate', () => {
  for (const name of ['web/src/env.ts', 'web/src/lib/CustomerScope.svelte', 'src/components/business/business-copy.ts', 'src/components/business/supporting-copy.ts', 'web/src/lib/business.server.ts']) {
    const source = readFileSync(new URL(`../${name}`, import.meta.url), 'utf8');
    assert.doesNotMatch(source, /FEATURE_CUSTOMER_SCOPE|customerScopeEnabled/, name);
  }
});
test('actual CI retains ordinary and entirely unset checks before artifact upload', () => assert.ok(valid(workflow)));
for (const mode of ['missing-normal', 'missing-unset', 'missing-artifact', 'skipped-normal', 'ignored-normal-failure', 'skipped-unset', 'ignored-unset-failure', 'inherited-job', 'inherited-workflow', 'configured-unset', 'wrong-directory', 'reverse', 'duplicate-normal']) {
  test(`permanent customer-scope build refuses ${mode}`, () => {
    const ci = structuredClone(workflow), job = ci.jobs['build-docs'], steps = job.steps;
    assert.ok(valid(ci));
    const normal = steps.find(step => step.name === 'Build');
    const unset = steps.find(step => step.run === unsetCommand);
    const artifact = steps.find(step => step.with?.name === 'business-site-preview');
    if (mode === 'missing-normal') steps.splice(steps.indexOf(normal), 1);
    if (mode === 'missing-unset') steps.splice(steps.indexOf(unset), 1);
    if (mode === 'missing-artifact') steps.splice(steps.indexOf(artifact), 1);
    if (mode === 'skipped-normal') normal.if = 'false';
    if (mode === 'ignored-normal-failure') normal['continue-on-error'] = true;
    if (mode === 'skipped-unset') unset.if = 'false';
    if (mode === 'ignored-unset-failure') unset['continue-on-error'] = true;
    if (mode === 'inherited-job') job.env = { [retired]: 'false' };
    if (mode === 'inherited-workflow') ci.env = { [retired]: 'false' };
    if (mode === 'configured-unset') unset.env = { [retired]: 'false' };
    if (mode === 'wrong-directory') normal['working-directory'] = 'web';
    if (mode === 'reverse') steps.reverse();
    if (mode === 'duplicate-normal') steps.push(structuredClone(normal));
    assert.equal(valid(ci), false);
  });
}
