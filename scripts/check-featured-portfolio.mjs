import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { selectPublicTools } from '../src/data/public-products.ts';
import catalogue from '../src/data/public-products.json' with { type: 'json' };
import snapshot from '../src/data/github-stars.json' with { type: 'json' };
const directory = process.argv[2];
assert.ok(directory, 'Usage: check-featured-portfolio.mjs <build-directory>');
const enabled = process.env.FEATURE_FEATURED_PORTFOLIO === 'true';
for (const prefix of ['', 'da/']) {
  const home = readFileSync(resolve(directory, prefix, 'index.html'), 'utf8');
  const projects = readFileSync(resolve(directory, prefix, 'projects/index.html'), 'utf8');
  assert.equal(projects.includes('data-featured-portfolio'), enabled);
  if (!enabled) continue;
  for (const html of [home, projects]) {
    const visibleText = html.replace(/<script\b[^>]*>[\s\S]*?<\/script>/g, '').replace(/<style\b[^>]*>[\s\S]*?<\/style>/g, '').replace(/<[^>]*>/g, ' ');
    assert.doesNotMatch(visibleText, /family|familie|Wedding App|Anonymized|Anonymiseret/i, 'The business presentation omits personal framing and anonymous demos');
    assert.equal((html.match(/data-work-example="coaching"/g) ?? []).length, 1);
    assert.ok(html.includes('href="https://ascoachingogvaner.dk/"'));
    assert.match(html, /alt="[^"<>]*AS Coaching[^"<>]*"/);
  }
  const cards = [...projects.matchAll(/data-public-product="([^"]+)"[^>]*data-stars="(\d+)"/g)];
  assert.equal(cards.length, 6);
  assert.deepEqual(cards.map(([, name, stars]) => [name, Number(stars)]), selectPublicTools(catalogue, snapshot).map(({ repository, stars }) => [repository, stars]));
  assert.ok(projects.includes('href="https://github.com/orgs/devantler-tech/repositories"'));
  assert.ok(projects.includes(`datetime="${snapshot.fetchedAt}"`));
  for (const [, name] of cards) assert.ok(projects.includes(`href="https://github.com/devantler-tech/${name}"`));
  for (const id of ['open-title', 'family-title', 'technical-projects', 'research', 'engineering-checks', '-self-hosted-personal-apps']) assert.ok(projects.includes(`id="${id}"`));
  assert.ok(projects.includes('href="/pdfs/thesis.pdf"'));
  assert.doesNotMatch(projects, /<script[^>]*>[\s\S]*?(?:api\.github\.com|fetch\()[\s\S]*?<\/script>/, 'No visitor-side GitHub calls');
}
console.log('Featured portfolio rollout and bilingual visitor output verified.');
