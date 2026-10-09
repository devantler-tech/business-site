import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const [directory, ...extra] = process.argv.slice(2);
assert.ok(directory && extra.length === 0, 'Usage: check-offer-rollout.mjs <build-directory>');
const preview = process.env.FEATURE_OFFER_COPY === 'true';
// These visitor-visible alternatives distinguish a preview leak from the
// published offer. Retire this check with the temporary gate after activation.
const cases = [
  ['', 'A small web app', 'A web app'],
  ['da', 'En lille webapp', 'En webapp'],
  ['about', 'I keep the work small enough', 'We plan manageable iterations'],
  ['da/about', 'Jeg holder opgaverne små nok', 'Vi planlægger overskuelige etaper'],
  ['projects', 'Discuss a small project', 'Discuss your project'],
  ['da/projects', 'Tal om et lille projekt', 'Tal om dit projekt'],
];
for (const [route, published, revised] of cases) {
  const page = readFileSync(resolve(directory, route, 'index.html'), 'utf8');
  assert.ok(page.includes(preview ? revised : published), `${route || '/'} uses the ${preview ? 'preview' : 'published'} offer`);
  assert.ok(!page.includes(preview ? published : revised), `${route || '/'} must not mix offer variants`);
}
console.log(`PASS: all six business routes use the ${preview ? 'preview' : 'published'} offer`);
