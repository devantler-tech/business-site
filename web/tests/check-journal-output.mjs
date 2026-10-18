import assert from "node:assert/strict";
import { readFile, readdir } from "node:fs/promises";
import { parse } from "parse5";
import { assertSameJournalCover } from "./journal-cover.mjs";

const enabled = process.argv[2] === "enabled";
assert.ok(enabled || process.argv[2] === "disabled");
const candidate = new URL("../build/", import.meta.url);
const published = new URL("../../dist/", import.meta.url);
for (const locale of ["", "da/"]) {
  if (enabled) {
    const html = await readFile(new URL(`${locale}projects/index.html`, candidate), "utf8");
    assert.ok(!html.includes('href="https://devantler.tech/blog/"'), "Projects Journal action stays local");
  }
}
/** @typedef {{nodeName:string, tagName?:string, value?:string, attrs?:{name:string,value:string}[], childNodes?:MarkupNode[], parentNode?:MarkupNode|null}} MarkupNode */
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const children = node => node.childNodes ?? [];
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const all = node => [node, ...children(node).flatMap(all)];
/** @param {MarkupNode | undefined} node @param {string} name */
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
/** @param {MarkupNode} node @param {string} name */
const hasClass = (node, name) => (attr(node, "class") ?? "").split(/\s+/).includes(name);
/** @param {MarkupNode | undefined} node @returns {string} */
const text = node => { assert.ok(node); return (node.nodeName === "#text" ? node.value ?? "" : children(node).map(text).join(" ")).replace(/\s+/g, " ").trim(); };
/** @param {MarkupNode} node @param {string} name */
const find = (node, name) => all(node).find(item => hasClass(item, name));
/** @param {string | undefined} href */
const absolute = href => { assert.ok(href); return new URL(href, "https://devantler.tech").href; };
const paths = ["blog/index.html", ...(await readdir(new URL("blog/", published), { withFileTypes: true }))
  .filter(entry => entry.isDirectory() && /^[1-9]\d*$/.test(entry.name))
  .sort((a, b) => Number(a.name) - Number(b.name)).map(entry => `blog/${entry.name}/index.html`)];
const archivePaths = [];
for (const kind of ["tags", "authors"]) {
  for (const entry of await readdir(new URL(`blog/${kind}/`, published), {withFileTypes:true})) {
    assert.ok(entry.isDirectory(), "published archives must have a complete route directory");
    archivePaths.push(`blog/${kind}/${entry.name}/index.html`);
  }
}
assert.ok(archivePaths.length > 1, "the actual topic and author inventory must be read");
const seen = new Set();
const covers = new Set();
let count = 0;
for (const [index, path] of [...paths, ...archivePaths].entries()) {
  const mainArchive = index < paths.length;
  if (!enabled) {
    await assert.rejects(readFile(new URL(path, candidate)), { code: "ENOENT" });
    continue;
  }
  const reference = parse(await readFile(new URL(path, published), "utf8"));
  let html = "";
  await assert.doesNotReject(async () => { html = await readFile(new URL(path, candidate), "utf8"); }, `missing Journal candidate archive: ${path}`);
  const page = parse(html);
  const cards = all(page).filter(node => hasClass(node, "sl-blog-preview"));
  const referenceCards = all(reference).filter(node => hasClass(node, "sl-blog-preview"));
  /** @param {MarkupNode} card */
  const metadata = card => ({
    title: text(find(card, "sl-blog-preview-title")),
    href: absolute(attr(find(card, "sl-blog-preview-link"), "href")),
    date: attr(find(card, "sl-blog-metadata-date"), "datetime"),
    dateLabel: text(find(card, "sl-blog-metadata-date")),
    excerpt: text(find(card, "sl-blog-preview-excerpt")),
    authors: all(card).filter(node => hasClass(node, "sl-blog-author")).map(node => [text(node), attr(node, "href")]),
    tags: all(card).filter(node => hasClass(node, "sl-blog-tag")).map(node => [text(node), absolute(attr(node, "href"))]),
  });
  assert.deepEqual(cards.map(metadata), referenceCards.map(metadata), `actual Astro archive parity: ${path}`);
  assert.equal(attr(all(page).find(node => node.tagName === "html"), "lang"), "en");
  const canonical = `https://devantler.tech/${path.replace(/index\.html$/, "")}`;
  assert.equal(attr(all(page).find(node => node.tagName === "link" && attr(node, "rel") === "canonical"), "href"), canonical);
  assert.equal(text(all(page).find(node => node.tagName === "title")), text(all(reference).find(node => node.tagName === "title")));
  assert.equal(attr(all(page).find(node => node.tagName === "meta" && attr(node, "name") === "description"), "content"), attr(all(reference).find(node => node.tagName === "meta" && attr(node, "name") === "description"), "content"));
  assert.ok(!html.includes("/da/blog/"), "English articles must not acquire invented Danish destinations");
  assert.ok(html.includes('href="/projects/"') && html.includes('href="/about/"') && html.includes('href="/da/"'));
  assert.ok(html.includes('href="https://devantler.tech/blog/rss.xml"'));
  assert.ok(html.includes('href="/blog/authors/nikolai-emil-damm/"'));
  for (const tag of all(page).filter(node => hasClass(node, "sl-blog-tag"))) {
    const href = attr(tag, "href");
    assert.ok(href);
    assert.ok(href.startsWith("/blog/tags/"), "topic choices stay inside the candidate");
    await readFile(new URL(`${href.slice(1)}index.html`, candidate));
  }
  const publishedBrowse = find(reference, "journal-browser");
  assert.ok(publishedBrowse);
  const persister = all(publishedBrowse).find(node => node.tagName === "sl-sidebar-state-persist");
  assert.ok(persister);
  const menu = children(persister).find(node => node.tagName === "ul");
  assert.ok(menu);
  for (const section of all(page).filter(node => hasClass(node, "browse-group"))) {
    const heading = text(children(section).find(node => node.tagName === "h2"));
    /** @type {MarkupNode | undefined} */
    const group = children(menu).flatMap(children).find(node => node.tagName === "details" && text(children(node).find(item => item.tagName === "summary")) === heading);
    assert.ok(group, `missing actual Astro browse group: ${heading}`);
    assert.deepEqual(all(section).filter(node => node.tagName === "a").map(node => [text(node), absolute(attr(node, "href"))]), all(group).filter(node => node.tagName === "a").map(node => [text(node), absolute(attr(node, "href"))]), `retained Astro browse choices: ${heading}`);
  }
  assert.equal(all(page).filter(node => hasClass(node, "browse-group")).length, 2);
  const pagination = find(page, "journal-pagination");
  if (mainArchive) {
    assert.ok(pagination, "native local pagination required, including first and last boundaries");
    const pageLinks = all(pagination).filter(node => node.tagName === "a").map(node => attr(node, "href"));
    if (index > 0) assert.ok(pageLinks.includes(index === 1 ? "/blog/" : `/blog/${index}/`));
    if (index < paths.length - 1) assert.ok(pageLinks.includes(`/blog/${index + 2}/`));
    assert.ok(pageLinks.every(href => href && /^\/blog\/(?:(?:[2-9]|[1-9]\d+)\/)?$/.test(href)));
  } else {
    assert.equal(pagination, undefined, "topic and author archives retain every matching post without truncation");
    assert.equal(text(all(page).find(node => node.tagName === "h1")), text(all(reference).find(node => node.tagName === "h1")));
  }
  for (const [cardIndex, card] of cards.entries()) {
    assert.equal(attr(card, "role"), undefined);
    assert.equal(attr(card, "tabindex"), undefined);
    const destination = attr(find(card, "sl-blog-preview-link"), "href");
    if (mainArchive) {
      assert.ok(!seen.has(destination), "post omitted/duplicated across page boundaries");
      seen.add(destination);
    }
    const image = all(card).find(node => node.tagName === "img");
    const src = attr(image, "src");
    assert.ok(src);
    assertSameJournalCover(src, attr(all(referenceCards[cardIndex]).find(node => node.tagName === "img"), "src"));
    assert.match(src, /^\/_app\/immutable\/assets\//);
    if (mainArchive) {
      assert.ok(!covers.has(src), "each post retains a distinct optimized cover");
      covers.add(src);
    }
    await readFile(new URL(src.slice(1), candidate));
    assert.ok(all(card).some(node => node.tagName === "source" && attr(node, "type") === "image/avif"));
    for (const source of all(card).filter(node => node.tagName === "source")) {
      const srcset = attr(source, "srcset");
      assert.ok(srcset);
      for (const entry of srcset.split(",")) {
        const url = entry.trim().split(/\s+/)[0];
        assert.match(url, /^\/_app\/immutable\/assets\//);
        await readFile(new URL(url.slice(1), candidate));
      }
    }
    const titleLink = find(card, "sl-blog-preview-link");
    assert.ok(titleLink);
    for (let parent = titleLink.parentNode; parent; parent = parent.parentNode) {
      assert.ok(attr(parent, "tabindex") === undefined && attr(parent, "role") !== "link", "native link cannot gain a duplicate focus/link ancestor");
    }
    if (mainArchive) count++;
  }
  const scripts = all(page).filter(node => node.tagName === "script");
  assert.equal(scripts.length, 2, "theme plus shared card controller only; no application hydration");
  for (const script of scripts) {
    const src = attr(script, "src");
    assert.ok(src);
    assert.match(src, /^\/_app\/immutable\/assets\/(theme|journal-navigation)\.[A-Za-z0-9_-]+\.js$/);
    await readFile(new URL(src.slice(1), candidate));
  }
  assert.ok(!/sveltekit.start|PORTAL_|COMPANY_POSTAL_ADDRESS|type="module"/.test(html));
}
if (enabled) {
  const dirs = await readdir(new URL("blog/", candidate), { withFileTypes: true });
  assert.deepEqual(dirs.filter(entry => entry.isDirectory()).map(entry => entry.name).sort(), [...paths.slice(1).map(path => path.split("/")[1]), "tags", "authors", "building-ksail-from-shell-to-dotnet-to-go"].sort(), "only reviewed archives and the admitted reading route enter the candidate");
  for (const kind of ["tags", "authors"]) {
    const actual = await readdir(new URL(`blog/${kind}/`, candidate));
    assert.deepEqual(actual.sort(), archivePaths.filter(path => path.startsWith(`blog/${kind}/`)).map(path => path.split("/")[2]).sort(), "the candidate may neither omit nor invent a filtered archive");
  }
  assert.ok(count > 0);
}
console.log(`Svelte Journal: ${enabled ? `${count} actual posts across ${paths.length} main and ${archivePaths.length} topic/author Astro-matched archives` : `${paths.length + archivePaths.length} archives absent`} verified`);
