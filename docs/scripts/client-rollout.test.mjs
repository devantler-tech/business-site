import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';

const workflow = load(readFileSync(new URL('../../.github/workflows/ci.yaml', import.meta.url), 'utf8'));
const valid = (ci) => {
  if (ci.env?.FEATURE_CLIENT_PORTFOLIO != null || ci.jobs?.['build-docs']?.env?.FEATURE_CLIENT_PORTFOLIO != null) return false;
  const steps = ci.jobs?.['build-docs']?.steps ?? [];
  const index = (flag, run) => steps.flatMap((step, i) => step.env?.FEATURE_CLIENT_PORTFOLIO === flag && step.run === run ? [i] : []);
  const enabled = index('true', 'npm run build');
  const disabled = index('false', 'npm run build');
  const unset = steps.flatMap((step, i) => step.run === 'env -u FEATURE_CLIENT_PORTFOLIO -u FEATURE_PREVIEW_BANNER npm run build' ? [i] : []);
  const artifact = steps.flatMap((step, i) => step.with?.name === 'business-site-preview' && step.with.path === 'docs/dist' ? [i] : []);
  return [enabled, disabled, unset, artifact].every((group) => group.length === 1) &&
    enabled[0] < disabled[0] && disabled[0] < unset[0] && unset[0] < artifact[0] &&
    [...enabled, ...disabled, ...unset].every((i) => steps[i]['working-directory'] === 'docs' && steps[i].if == null &&
      (steps[i]['continue-on-error'] == null || steps[i]['continue-on-error'] === false));
};
test('actual CI builds the client preview, false and entirely unset before production artifact upload', () => assert.ok(valid(workflow)));
for (const mode of ['missing-preview', 'missing-false', 'missing-unset', 'skipped-preview', 'ignored-failure', 'reverse', 'inherited-flag']) {
  test(`client rollout rejects ${mode}`, () => {
    const ci = structuredClone(workflow);
    const job = ci.jobs['build-docs'];
    if (mode === 'missing-preview') job.steps = job.steps.filter((step) => step.env?.FEATURE_CLIENT_PORTFOLIO !== 'true');
    if (mode === 'missing-false') job.steps = job.steps.filter((step) => step.env?.FEATURE_CLIENT_PORTFOLIO !== 'false');
    if (mode === 'missing-unset') job.steps = job.steps.filter((step) => !step.run?.startsWith('env -u FEATURE_CLIENT_PORTFOLIO'));
    if (mode === 'skipped-preview') job.steps.find((step) => step.env?.FEATURE_CLIENT_PORTFOLIO === 'true').if = 'false';
    if (mode === 'ignored-failure') job.steps.find((step) => step.env?.FEATURE_CLIENT_PORTFOLIO === 'true')['continue-on-error'] = true;
    if (mode === 'reverse') job.steps.reverse();
    if (mode === 'inherited-flag') job.env.FEATURE_CLIENT_PORTFOLIO = 'true';
    assert.equal(valid(ci), false);
  });
}
