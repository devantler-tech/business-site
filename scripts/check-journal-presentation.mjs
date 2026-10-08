import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = process.argv[2] ?? 'dist';
const enabled = process.argv[3] ? process.argv[3] === 'on' : process.env.FEATURE_JOURNAL_PRESENTATION === 'true';
/** Recursively inventory generated index pages, including archive, author and tag routes. */
function pages(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap(entry =>
    entry.isDirectory() ? pages(join(dir, entry.name)) : entry.name === 'index.html' ? [join(dir, entry.name)] : []);
}
const blogPages = pages(join(root, 'blog'));
const articlePaths = readdirSync('src/content/docs/blog').filter(name => /\.mdx?$/.test(name))
  .map(name => name.replace(/\.mdx?$/, ''));
const htmlAt = path => readFileSync(join(root, path, 'index.html'), 'utf8');
const feed = readFileSync(join(root, 'blog/rss.xml'), 'utf8');
let assertions = 0;
for (const path of blogPages) {
  const html = readFileSync(path, 'utf8');
  const route = relative(root, path);
  assert.equal(html.includes('data-journal-presentation'), enabled, `${route}: release flag controls the emitted Journal frame`);
  assertions++;
  if (enabled) {
    assert.match(html, /<details\b[^>]*class="journal-browse"/, `${route}: native expandable browsing`);
    assert.match(html, /<summary[^>]*>Browse the Journal<\/summary>/, `${route}: discoverable browsing control`);
    assert.match(html, /<sl-sidebar-state-persist\b[^>]*>[\s\S]*?<ul\b/, `${route}: browsing retains the real Starlight list wrapper`);
    assert.equal((html.match(/<h1\b/g) ?? []).length, 1, `${route}: one page heading`);
    assert.match(html, /href="\/blog\/rss.xml"/, `${route}: RSS remains discoverable`);
    assertions += 5;
  }
}
const index = htmlAt('blog');
assert.equal((index.match(/<article\b[^>]*class="[^\"]*\bsl-blog-preview\b[^\"]*"/g) ?? []).length, 5, 'first archive page keeps five native post previews');
assert.match(index, /href="\/blog\/2\/"/, 'pagination remains available');
assertions += 2;
if (enabled) {
  assert.match(index, /<h1[^>]*id="_top"[^>]*>Journal<\/h1>/, 'landing page has a visible editorial heading');
  assert.match(index, /class="journal-intro"/, 'landing page explains the publication before its stories');
  assertions += 2;
}
for (const slug of articlePaths) {
  const html = htmlAt(`blog/${slug}`);
  if (enabled) {
    assert.match(html, /data-journal-kind="article"/, `${slug}: article receives the reading layout, not the archive`);
    assert.match(html, /<details\b[^>]*id="starlight__mobile-toc"/, `${slug}: native mobile contents remain emitted`);
    assertions += 2;
  }
  assert.match(html, /class="[^\"]*\bsl-markdown-content\b[^\"]*"/, `${slug}: real reading content remains rendered`);
  assert.doesNotMatch(html, /<article\b[^>]*class="[^\"]*\bsl-blog-preview\b[^\"]*"/, `${slug}: reading prose is not a navigation card`);
  assert.ok(feed.includes(`https://devantler.tech/blog/${slug}/`), `${slug}: canonical feed destination retained`);
  assertions += 3;
}
for (const guide of ['agentic-engineering', 'templates/go', 'templates/dotnet']) {
  assert.ok(!htmlAt(guide).includes('data-journal-presentation'), `${guide}: Journal layout does not spill into technical guides`);
  assertions++;
}
console.log(`Journal ${enabled ? 'enabled' : 'disabled'}: ${assertions} checks across ${blogPages.length} generated routes and ${articlePaths.length} preserved articles passed.`);
