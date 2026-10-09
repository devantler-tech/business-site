import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const unsetCommand = 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_JOURNAL_PRESENTATION -u FEATURE_COMPANY_IDENTITY npm run build';
test('company identity is a typed default-off release', () => {
  const config = readFileSync(new URL('../astro.config.mjs', import.meta.url), 'utf8');
  const flag = config.match(/FEATURE_COMPANY_IDENTITY:\s*envField\.boolean\(\{([\s\S]*?)\}\)/)?.[1];
  assert.ok(flag, 'The company release uses a typed Astro boolean');
  assert.match(flag, /default:\s*false\b/);
});
const valid = (ci) => {
  if (ci.env?.FEATURE_COMPANY_IDENTITY != null || ci.jobs?.['build-docs']?.env?.FEATURE_COMPANY_IDENTITY != null) return false;
  const steps = ci.jobs?.['build-docs']?.steps ?? [];
  const index = (flag) => steps.flatMap((step, i) => step.env?.FEATURE_COMPANY_IDENTITY === flag && step.run === 'npm run build' ? [i] : []);
  const enabled = index('true'), disabled = index('false');
  const unset = steps.flatMap((step, i) => step.run === unsetCommand ? [i] : []);
  const artifact = steps.flatMap((step, i) => step.with?.name === 'business-site-preview' && step.with.path === 'dist' ? [i] : []);
  return [enabled, disabled, unset, artifact].every((group) => group.length === 1) &&
    enabled[0] < disabled[0] && disabled[0] < unset[0] && unset[0] < artifact[0] &&
    [...enabled, ...disabled, ...unset].every((i) => steps[i]['working-directory'] === '.' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('actual CI validates enabled, false and unset registration before artifact upload', () => assert.ok(valid(workflow)));
for (const mode of ['missing-preview', 'missing-false', 'missing-unset', 'skipped-preview', 'ignored-failure', 'reverse', 'inherited-job', 'inherited-workflow']) {
  test(`company rollout rejects ${mode}`, () => {
    const ci = structuredClone(workflow), job = ci.jobs['build-docs'];
    if (mode === 'missing-preview') job.steps = job.steps.filter((step) => step.env?.FEATURE_COMPANY_IDENTITY !== 'true');
    if (mode === 'missing-false') job.steps = job.steps.filter((step) => step.env?.FEATURE_COMPANY_IDENTITY !== 'false');
    if (mode === 'missing-unset') job.steps = job.steps.filter((step) => step.run !== unsetCommand);
    if (mode === 'skipped-preview') job.steps.find((step) => step.env?.FEATURE_COMPANY_IDENTITY === 'true').if = 'false';
    if (mode === 'ignored-failure') job.steps.find((step) => step.env?.FEATURE_COMPANY_IDENTITY === 'true')['continue-on-error'] = true;
    if (mode === 'reverse') job.steps.reverse();
    if (mode === 'inherited-job') job.env.FEATURE_COMPANY_IDENTITY = 'true';
    if (mode === 'inherited-workflow') ci.env = { FEATURE_COMPANY_IDENTITY: 'true' };
    assert.equal(valid(ci), false);
  });
}
