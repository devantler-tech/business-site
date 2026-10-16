import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { parse } from "parse5";

const path = "blog/building-ksail-from-shell-to-dotnet-to-go/index.html";
const candidate = new URL("../build/", import.meta.url);
const published = new URL("../../dist/", import.meta.url);
/** @typedef {{nodeName:string,tagName?:string,value?:string,attrs?:{name:string,value:string}[],childNodes?:MarkupNode[]}} MarkupNode */
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
/** @param {MarkupNode|undefined} node @param {string} name */
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
/** @param {MarkupNode} node @param {string} name */
const hasClass = (node, name) => (attr(node, "class") ?? "").split(/\s+/).includes(name);
/** @param {MarkupNode} node @returns {string} */
const text = node => ["script","style"].includes(node.tagName ?? "") ? "" : node.nodeName === "#text" ? node.value ?? "" : (node.childNodes ?? []).map(text).join("");
/** @param {MarkupNode|undefined} node */
const normalized = node => { assert.ok(node); return text(node).replace(/\s+/g, " ").trim(); };
/** @param {string|undefined} href */
const absolute = href => { assert.ok(href); return new URL(href, "https://devantler.tech").href; };
/** @param {Buffer} bytes */
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
/** @param {URL} directory @param {string|undefined} href */
const asset = (directory, href) => { assert.ok(href); return readFileSync(new URL(href.slice(1),directory)); };
const reference = parse(readFileSync(new URL(path, published), "utf8"));
const original = all(reference).find(node => hasClass(node, "sl-markdown-content"));
assert.ok(original);

/** Compare complete real rendered content, not excerpts or expected source strings. @param {string} html */
function check(html) {
  const document = all(parse(html));
  const body = document.find(node => hasClass(node, "sl-markdown-content"));
  assert.ok(body, "complete reading body required");
  const nodes = all(body);
  assert.ok(original);
  const previous = all(original);
  assert.equal(normalized(body), normalized(original), "complete prose/code/metadata/tag text must match Astro");
  /** @param {MarkupNode[]} ns */
  const headings = ns => ns.filter(node => /^h[2-6]$/.test(node.tagName ?? "")).map(node => [node.tagName, attr(node,"id"), normalized(node)]);
  assert.deepEqual(headings(nodes), headings(previous), "every actual heading and bookmark is retained");
  /** @param {MarkupNode[]} ns */
  const copies = ns => ns.filter(node => node.tagName === "button").map(node => attr(node, "data-code"));
  assert.deepEqual(copies(nodes), copies(previous), "copy payloads follow the actual renderer, including its comment handling");
  /** @param {MarkupNode[]} ns */
  const links = ns => ns.filter(node => node.tagName === "a").map(node => [normalized(node), absolute(attr(node,"href"))]);
  assert.deepEqual(links(nodes), links(previous), "all authored, author and topic destinations survive");
  /** @param {MarkupNode[]} ns */
  const times = ns => ns.filter(node => node.tagName === "time").map(node => [attr(node,"datetime"),normalized(node)]);
  assert.deepEqual(times(document), times(all(reference)), "publication and committed modification dates survive");
  const toc = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === "On this page");
  assert.ok(toc);
  assert.deepEqual(all(toc).filter(node => node.tagName === "a").map(node => attr(node,"href")), headings(previous).map(item => `#${item[1]}`));
  /** @param {MarkupNode[]} ns */
  const adjacent = ns => ns.filter(node => node.tagName === "a" && ["prev","next"].includes(attr(node,"rel") ?? "")).map(node => [attr(node,"rel"),absolute(attr(node,"href")),normalized(node)]);
  const navigation = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === "More articles");
  assert.ok(navigation);
  const expectedAdjacent = adjacent(all(reference));
  const actualAdjacent = all(navigation).filter(node => node.tagName === "a").map(node => [attr(node,"rel"),absolute(attr(node,"href")),normalized(all(node).find(child => child.tagName === "strong"))]);
  assert.deepEqual(actualAdjacent, expectedAdjacent, "adjacent articles retain real titles and published destinations");
  /** @param {MarkupNode[]} ns */
  const metadata = ns => ns.filter(node => node.tagName === "meta" && (attr(node,"property") || ["description","author","twitter:card","twitter:image"].includes(attr(node,"name") ?? ""))).map(node => [attr(node,"name") ?? attr(node,"property"),attr(node,"content")]).sort();
  assert.deepEqual(metadata(document), metadata(all(reference)), "article sharing metadata is retained");
  /** @param {MarkupNode[]} ns */
  const structured = ns => ns.filter(node => node.tagName === "script" && attr(node,"type") === "application/ld+json").map(node => JSON.parse((node.childNodes ?? []).map(child => child.value).join("")));
  assert.deepEqual(structured(document), structured(all(reference)), "actual article structured data is preserved");
  for (const key of ["title", "canonical"]) {
    /** @param {MarkupNode[]} ns */
    const find = ns => key === "title" ? normalized(ns.find(node => node.tagName === "title")) : attr(ns.find(node => node.tagName === "link" && attr(node,"rel") === "canonical"), "href");
    assert.equal(find(document), find(all(reference)), `actual ${key} parity`);
  }
  /** @param {MarkupNode[]} ns */
  const images = ns => ns.filter(node => node.tagName === "img");
  assert.equal(images(nodes).length, images(previous).length);
  for (const [i, image] of images(nodes).entries()) {
    assert.equal(attr(image,"alt"), attr(images(previous)[i],"alt"));
    assert.equal(digest(asset(candidate,attr(image,"src"))), digest(asset(published,attr(images(previous)[i],"src"))), "actual local image bytes preserved");
  }
  for (const node of nodes.filter(node => node.tagName === "link")) {
    const href = attr(node,"href");
    assert.ok(href);
    assert.match(href, /^\/_app\/immutable\/assets\/reading-/);
    readFileSync(new URL(href.slice(1), candidate));
  }
  const article = document.find(node => node.tagName === "article");
  assert.ok(article && attr(article,"role") === undefined && attr(article,"tabindex") === undefined);
  assert.ok(!document.some(node => hasClass(node,"sl-blog-preview")), "reading prose is not an archive card");
  assert.ok(!html.includes("/da/blog/") && !html.includes("sveltekit.start") && !html.includes("PORTAL_"));
}

if (process.argv[2] === "disabled") {
  assert.ok(!existsSync(new URL(path,candidate)), "disabled reading route must be removed after enabled output");
  const assets = new URL("_app/immutable/assets/",candidate);
  assert.ok(!existsSync(assets) || !readdirSync(assets).some(name => name.startsWith("reading-")), "disabled reading assets must also be absent");
  console.log("Svelte reading: route and dependent assets absent");
} else {
  assert.equal(process.argv[2], "enabled");
  const html = readFileSync(new URL(path,candidate), "utf8");
  check(html);
  /** @type {[string,(value:string)=>string,RegExp][]} */
  const mutations = [
    ["missing paragraph", v => v.replace(/<p>Building a developer tool[\s\S]*?<\/p>/, ""), /complete prose/],
    ["broken bookmark", v => v.replace('id="the-problem"', 'id="broken-problem"'), /bookmark/],
    ["wrong clipboard text", v => v.replace('data-code="', 'data-code="wrong'), /copy payload/],
    ["wrong destination", v => v.replace('href="https://github.com/devantler"', 'href="https://example.com/"'), /destinations/],
    ["fabricated modified date", v => v.replace(`datetime="${attr(all(reference).filter(node => node.tagName === "time").at(-1),"datetime")}"`, 'datetime="2000-01-01T00:00:00.000Z"'), /dates survive/],
    ["missing sharing metadata", v => v.replace('property="og:title"', 'property="wrong:title"'), /sharing metadata/],
  ];
  for (const [name, mutate, failure] of mutations) {
    const changed = mutate(html);
    assert.notEqual(changed, html, `mutation must reach actual output: ${name}`);
    assert.throws(() => check(changed), failure, `reject ${name}`);
  }
  console.log(`Svelte reading: complete body, ${all(original).filter(n=>/^h[2-6]$/.test(n.tagName??"")).length} bookmarks, ${all(original).filter(n=>n.tagName==="button").length} actual copy controls, assets, dates, metadata and six rejection controls verified`);
}
