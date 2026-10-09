import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
/** Admit only unconditional enabled, disabled and unset builds before the production artifact. */
const valid = (ci) => {
  const flag = 'FEATURE_JOURNAL_PRESENTATION';
  const job = ci.jobs?.['build-docs'];
  if (ci.env?.[flag] != null || job?.env?.[flag] != null) return false;
  const steps = job?.steps ?? [];
  const indices = (predicate) => steps.flatMap((step, i) => predicate(step) ? [i] : []);
  const enabled = indices(step => step.env?.[flag] === 'true' && step.run === 'npm run build');
  const disabled = indices(step => step.env?.[flag] === 'false' && step.run === 'npm run build');
  const unset = indices(step => step.run === 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER -u FEATURE_JOURNAL_PRESENTATION -u FEATURE_OFFER_COPY npm run build' && step.env?.[flag] == null);
  const artifact = indices(step => step.with?.name === 'business-site-preview' && step.with.path === 'dist');
  return [enabled, disabled, unset, artifact].every(group => group.length === 1) &&
    enabled[0] < disabled[0] && disabled[0] < unset[0] && unset[0] < artifact[0] &&
    [...enabled, ...disabled, ...unset].every(i => steps[i]['working-directory'] === '.' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('actual CI builds Journal enabled, false and entirely unset before uploading production', () => assert.ok(valid(workflow)));
for (const mode of ['missing-preview', 'missing-false', 'missing-unset', 'skipped-preview', 'ignored-failure', 'reverse', 'inherited-job', 'inherited-workflow', 'inherited-unset']) {
  test(`Journal rollout rejects ${mode}`, () => {
    const ci = structuredClone(workflow);
    const job = ci.jobs['build-docs'];
    assert.ok(valid(ci), 'negative controls start from the validated actual workflow');
    const enabled = job.steps.find(step => step.env?.FEATURE_JOURNAL_PRESENTATION === 'true');
    const disabled = job.steps.find(step => step.env?.FEATURE_JOURNAL_PRESENTATION === 'false');
    assert.ok(enabled && disabled, 'CI must first supply both flag states');
    if (mode === 'missing-preview') job.steps = job.steps.filter(step => step !== enabled);
    if (mode === 'missing-false') job.steps = job.steps.filter(step => step !== disabled);
    if (mode === 'missing-unset') job.steps = job.steps.filter(step => !step.run?.startsWith('env -u FEATURE_CLIENT_PORTFOLIO'));
    if (mode === 'skipped-preview') enabled.if = 'false';
    if (mode === 'ignored-failure') enabled['continue-on-error'] = true;
    if (mode === 'reverse') job.steps.reverse();
    if (mode === 'inherited-job') job.env.FEATURE_JOURNAL_PRESENTATION = 'true';
    if (mode === 'inherited-workflow') ci.env = { FEATURE_JOURNAL_PRESENTATION: 'true' };
    if (mode === 'inherited-unset') job.steps.find(step => step.run?.startsWith('env -u FEATURE_CLIENT_PORTFOLIO')).env = { FEATURE_JOURNAL_PRESENTATION: 'true' };
    assert.equal(valid(ci), false);
  });
}
