import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { parse } from 'parse5';

const root = process.argv[2] ?? 'dist';
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
const businessDescription = 'Devantler Tech — a one-person software business building websites, apps and open-source tools.';
/** Read emitted, entity-decoded metadata rather than matching HTML source spelling. */
function descriptionMetadata(node) {
  const attrs = Object.fromEntries((node.attrs ?? []).map(attr => [attr.name, attr.value]));
  const key = attrs.name === 'description' ? 'description' : attrs.property === 'og:description' ? 'og:description' : undefined;
  return [
    ...(node.tagName === 'meta' && key ? [{ key, content: attrs.content }] : []),
    ...(node.childNodes ?? []).flatMap(descriptionMetadata),
  ];
}
let assertions = 0;
for (const path of blogPages) {
  const html = readFileSync(path, 'utf8');
  const route = relative(root, path);
  assert.ok(html.includes('data-journal-presentation'), `${route}: professional Journal frame is always emitted`);
  assertions++;
  assert.match(html, /<details\b[^>]*class="journal-browse"/, `${route}: native expandable browsing`);
  assert.match(html, /<summary[^>]*>Browse the Journal<\/summary>/, `${route}: discoverable browsing control`);
  assert.match(html, /<sl-sidebar-state-persist\b[^>]*>[\s\S]*?<ul\b/, `${route}: browsing retains the real Starlight list wrapper`);
  assert.equal((html.match(/<h1\b/g) ?? []).length, 1, `${route}: one page heading`);
  assert.match(html, /href="\/blog\/rss.xml"/, `${route}: RSS remains discoverable`);
  assertions += 5;
  if (html.includes('data-journal-kind="archive"')) {
    const descriptions = descriptionMetadata(parse(html));
    for (const key of ['description', 'og:description']) {
      assert.deepEqual(descriptions.filter(meta => meta.key === key).map(meta => meta.content), [businessDescription],
        `${route}: ${key} presents application work without a size qualifier`);
      assertions++;
    }
  }
}
const index = htmlAt('blog');
assert.equal((index.match(/<article\b[^>]*class="[^\"]*\bsl-blog-preview\b[^\"]*"/g) ?? []).length, 5, 'first archive page keeps five native post previews');
assert.match(index, /href="\/blog\/2\/"/, 'pagination remains available');
assertions += 2;
assert.match(index, /<h1[^>]*id="_top"[^>]*>Journal<\/h1>/, 'landing page has a visible editorial heading');
assert.match(index, /class="journal-intro"/, 'landing page explains the publication before its stories');
assertions += 2;
for (const slug of articlePaths) {
  const html = htmlAt(`blog/${slug}`);
  assert.match(html, /data-journal-kind="article"/, `${slug}: article receives the reading layout, not the archive`);
  assert.match(html, /<details\b[^>]*id="starlight__mobile-toc"/, `${slug}: native mobile contents remain emitted`);
  assertions += 2;
  assert.match(html, /class="[^\"]*\bsl-markdown-content\b[^\"]*"/, `${slug}: real reading content remains rendered`);
  assert.doesNotMatch(html, /<article\b[^>]*class="[^\"]*\bsl-blog-preview\b[^\"]*"/, `${slug}: reading prose is not a navigation card`);
  assert.ok(feed.includes(`https://devantler.tech/blog/${slug}/`), `${slug}: canonical feed destination retained`);
  assertions += 3;
}
for (const guide of ['agentic-engineering', 'templates/go', 'templates/dotnet']) {
  assert.ok(!htmlAt(guide).includes('data-journal-presentation'), `${guide}: Journal layout does not spill into technical guides`);
  assertions++;
}
console.log(`Journal: ${assertions} checks across ${blogPages.length} generated routes and ${articlePaths.length} preserved articles passed.`);
