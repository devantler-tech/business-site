import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { load } from 'js-yaml';
const publisher = load(readFileSync(new URL('../.github/workflows/publish-pages.yaml', import.meta.url), 'utf8'));
const valid = workflow => {
  const steps = workflow.jobs?.build?.steps ?? [];
  const indices = predicate => steps.flatMap((step, index) => predicate(step) ? [index] : []);
  const refresh = indices(step => step.run === 'bash scripts/refresh-public-stars.sh');
  const build = indices(step => step.run === 'npm run build');
  const upload = indices(step => step.uses?.startsWith('actions/upload-pages-artifact@'));
  return [refresh, build, upload].every(group => group.length === 1) &&
    refresh[0] < build[0] && build[0] < upload[0] &&
    [refresh[0], build[0]].every(index => steps[index].if == null &&
      (steps[index]['continue-on-error'] == null || steps[index]['continue-on-error'] === false) &&
      steps[index]['working-directory'] === '.') &&
    steps[refresh[0]].env?.GH_TOKEN === '${{ github.token }}' &&
    steps[refresh[0]].env?.FEATURE_FEATURED_PORTFOLIO == null &&
    workflow.jobs.build.permissions?.contents === 'read';
};
test('every publication refreshes the complete public ranking before building and uploading', () => assert.ok(valid(publisher)));
for (const mode of ['missing', 'skipped', 'ignored', 'after-build', 'wrong-directory', 'no-token']) {
  test(`fresh publication rejects ${mode}`, () => {
    const workflow = structuredClone(publisher);
    assert.ok(valid(workflow), 'controls start from the real valid publisher');
    const steps = workflow.jobs.build.steps;
    const refresh = steps.find(step => step.run === 'bash scripts/refresh-public-stars.sh');
    if (mode === 'missing') workflow.jobs.build.steps = steps.filter(step => step !== refresh);
    if (mode === 'skipped') refresh.if = 'false';
    if (mode === 'ignored') refresh['continue-on-error'] = true;
    if (mode === 'after-build') workflow.jobs.build.steps = steps.filter(step => step !== refresh).concat(refresh);
    if (mode === 'wrong-directory') refresh['working-directory'] = 'docs';
    if (mode === 'no-token') delete refresh.env.GH_TOKEN;
    assert.equal(valid(workflow), false);
  });
}
