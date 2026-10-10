import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const [directory, ...extra] = process.argv.slice(2);
assert.ok(directory && extra.length === 0, 'Usage: check-offer-rollout.mjs <build-directory>');
const preview = process.env.FEATURE_OFFER_COPY === 'true';
const legacyProjects = (process.env.FEATURE_CLIENT_PORTFOLIO ?? 'true') !== 'true'
  && (process.env.FEATURE_FEATURED_PORTFOLIO ?? 'true') !== 'true';
// These visitor-visible alternatives distinguish a preview leak from the
// published offer through actual maintenance terms, not neutral labels (#41).
// Retire this check with the temporary gate after activation.
const cases = [
  ['', 'Hosting capacity, maintenance, backups and support scope are agreed separately.', 'Hosted projects include routine maintenance in the agreed monthly price;'],
  ['da', 'Hostingkapacitet, vedligeholdelse, backup og support aftales særskilt.', 'Hostede projekter har løbende vedligeholdelse med i den aftalte månedspris;'],
  ['about', 'Planned work, not an emergency service', 'Included maintenance, agreed development'],
  ['da/about', 'Planlagt arbejde, ikke akut beredskab', 'Inkluderet vedligeholdelse, aftalt udvikling'],
  ...(legacyProjects ? [
    ['projects', 'ongoing care is scoped separately.', 'Routine maintenance is included for hosted projects;'],
    ['da/projects', 'løbende vedligeholdelse aftales særskilt.', 'Løbende vedligeholdelse er inkluderet for hostede projekter;'],
  ] : []),
];
for (const [route, published, revised] of cases) {
  const page = readFileSync(resolve(directory, route, 'index.html'), 'utf8');
  assert.ok(page.includes(preview ? revised : published), `${route || '/'} uses the ${preview ? 'preview' : 'published'} offer`);
  assert.ok(!page.includes(preview ? published : revised), `${route || '/'} must not mix offer variants`);
}
// The client-facing Projects page links back to Home's quality highlight; it
// has no hosting terms to distinguish. Its inquiry label is neutral in both states.
for (const [route, label, home] of [['projects', 'Discuss your project', '/'], ['da/projects', 'Tal om dit projekt', '/da/']]) {
  const page = readFileSync(resolve(directory, route, 'index.html'), 'utf8');
  assert.ok(page.includes(label), `${route} retains a size-neutral inquiry`);
  assert.ok(page.includes(`href="${home}#contact"`), `${route} retains its localized inquiry destination`);
}
console.log(`PASS: six business routes retain neutral inquiries and the ${preview ? 'preview' : 'published'} offer boundaries`);
