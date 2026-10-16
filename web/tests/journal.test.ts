import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { assertSameJournalCover } from "./journal-cover.mjs";

async function adapter() {
  let module: typeof import("../../src/data/journal.mjs") | undefined;
  await assert.doesNotReject(async () => { module = await import("../../src/data/journal.mjs"); }, "the real Journal metadata adapter must exist");
  return module!;
}
const post = (title: string, date = "2026-01-01", extra = "") => `---\ntitle: ${title}\ndate: ${date}\nexcerpt: A **useful** summary.\ndescription: A useful description.\nauthors: [devantler]\ntags: [Kubernetes, Hello World]\ncover:\n  image: ../../../assets/editorial/journal-welcome.webp\n  alt: Illustration of a workshop\n${extra}---\n\nReading body.\n`;
test("the real authored archive supplies ordered dates, excerpts, destinations and all page boundaries", async () => {
  const { readJournal, paginateJournal } = await adapter();
  const posts = readJournal();
  assert.ok(posts.length > 0);
  const pages = Array.from({ length: Math.ceil(posts.length / 5) }, (_, i) => paginateJournal(posts, i === 0 ? undefined : `${i + 1}`));
  assert.deepEqual(pages.flatMap(page => page.posts.map(item => item.href)), posts.map(item => item.href));
  assert.equal(new Set(posts.map(item => item.href)).size, posts.length);
  assert.match(posts[0].href, /^https:\/\/devantler.tech\/blog\//);
  assert.equal(pages[0].previous, null);
  assert.equal(pages.at(-1)?.next, null);
  for (const page of ["1", "0", "01", "2x", "-1", "Infinity", "999999999999999999999999999999"]) assert.throws(() => paginateJournal(posts, page), /Journal page/);
  assert.throws(() => paginateJournal(posts, `${pages.length + 1}`), /Journal page/);
});
test("maintained parsing handles Markdown formatting, date ties, drafts and topic slugging", async () => {
  const { readJournal, paginateJournal } = await adapter();
  const dir = mkdtempSync(join(tmpdir(), "journal-fixture-"));
  try {
    writeFileSync(join(dir, "b.md"), post("Bravo"));
    writeFileSync(join(dir, "a.mdx"), post("Alpha"));
    writeFileSync(join(dir, "new.md"), post("Newest", "2026-02-01"));
    writeFileSync(join(dir, "draft.md"), post("Draft", "2026-03-01", "draft: true\n"));
    const posts = readJournal(pathToFileURL(dir + "/"));
    assert.deepEqual(posts.map(item => item.title), ["Newest", "Alpha", "Bravo"]);
    assert.equal(posts[0].date, "2026-02-01T00:00:00.000Z");
    assert.equal(posts[0].excerpt, "<p>A <strong>useful</strong> summary.</p>\n");
    assert.equal(posts[0].authors[0].name, "Nikolai Emil Damm");
    assert.equal(posts[0].authors[0].url, "https://github.com/devantler");
    assert.deepEqual(posts[0].tags, [{label:"Kubernetes",href:"https://devantler.tech/blog/tags/kubernetes/"},{label:"Hello World",href:"https://devantler.tech/blog/tags/hello-world/"}]);
    assert.equal(paginateJournal(posts).posts.length, 3);
  } finally { rmSync(dir, { recursive:true, force:true }); }
});
test("pagination admits later decimal pages without accepting aliases or empty slices", async () => {
  const { paginateJournal } = await adapter();
  const posts = Array.from({ length:51 }, (_, i) => ({ href:`post-${i}` }));
  assert.equal(paginateJournal(posts, "10").posts[0].href, "post-45");
  assert.equal(paginateJournal(posts, "11").posts.length, 1);
});
for (const [name, invalid] of [
  ["missing frontmatter", "No metadata"],
  ["malformed YAML", post("[unfinished")],
  ["missing excerpt", post("Title").replace("excerpt: A **useful** summary.\n", "")],
  ["missing title", post("Title").replace("title: Title\n", "")],
  ["invalid date", post("Title", "yesterday")],
  ["unknown author", post("Title").replace("[devantler]", "[unknown]")],
  ["external cover", post("Title").replace("../../../assets/editorial/journal-welcome.webp", "https://example.com/image.webp")],
  ["unsupported custom slug", post("Title", "2026-01-01", "slug: other\n")],
  ["non-boolean draft", post("Title", "2026-01-01", "draft: false-ish\n")],
  ["duplicate tags", post("Title").replace("[Kubernetes, Hello World]", "[same, same]")],
  ["empty topic slug", post("Title").replace("[Kubernetes, Hello World]", '["!!!"]')],
  ["unsupported featured ordering", post("Title", "2026-01-01", "featured: true\n")],
]) test(`incomplete Journal metadata fails closed: ${name}`, async () => {
  const { readJournal } = await adapter();
  const dir = mkdtempSync(join(tmpdir(), "journal-invalid-"));
  try {
    writeFileSync(join(dir, "invalid.md"), invalid);
    assert.throws(() => readJournal(pathToFileURL(dir + "/")), /Journal|YAML/);
  } finally { rmSync(dir, { recursive:true, force:true }); }
});
test("conflicting Markdown and MDX routes fail rather than duplicate a published destination", async () => {
  const { readJournal } = await adapter();
  const dir = mkdtempSync(join(tmpdir(), "journal-duplicate-"));
  try {
    writeFileSync(join(dir, "same.md"), post("One"));
    writeFileSync(join(dir, "same.mdx"), post("Two"));
    assert.throws(() => readJournal(pathToFileURL(dir + "/")), /Journal.*duplicate/);
  } finally { rmSync(dir, { recursive:true, force:true }); }
});
test("browse choices preserve the ten recent posts and count-ranked topics from the actual collection", async () => {
  const { readJournal } = await adapter();
  const { browseJournal } = await import("../../src/components/business/journal-pages.js");
  const posts = readJournal();
  const browse = browseJournal(posts);
  assert.deepEqual(browse.recent.map(post => post.href), posts.slice(0, 10).map(post => post.href));
  assert.equal(browse.tags.reduce((count, tag) => count + tag.count, 0), posts.reduce((count, post) => count + post.tags.length, 0));
  assert.equal(new Set(browse.tags.map(tag => tag.href)).size, browse.tags.length);
  for (const [index, tag] of browse.tags.entries()) {
    const previous = browse.tags[index - 1];
    if (previous) assert.ok(previous.count > tag.count || (previous.count === tag.count && previous.label.localeCompare(tag.label) <= 0));
  }
});
test("image parity rejects a permutation of otherwise unique, valid cover assets", async () => {
  const { readJournal } = await adapter();
  const posts = readJournal();
  assert.ok(posts.length > 1);
  for (const [index, post] of posts.entries()) {
    const stem = post.cover.replace(/\.webp$/, "");
    assertSameJournalCover(`/_app/immutable/assets/${stem}.hash.avif`, `/_astro/${stem}.different.webp`);
    const swapped = posts[(index + 1) % posts.length].cover.replace(/\.webp$/, "");
    assert.throws(() => assertSameJournalCover(`/_app/immutable/assets/${swapped}.hash.avif`, `/_astro/${stem}.different.webp`), /own authored editorial cover/);
  }
});
test("empty and unreadable inventories cannot become a reassuring empty archive", async () => {
  const { readJournal } = await adapter();
  const dir = mkdtempSync(join(tmpdir(), "journal-empty-"));
  try { assert.throws(() => readJournal(pathToFileURL(dir + "/")), /Journal/); }
  finally { rmSync(dir, { recursive:true, force:true }); }
  assert.throws(() => readJournal(pathToFileURL(dir + "/")), /ENOENT/);
});
