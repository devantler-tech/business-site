import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { runInNewContext } from "node:vm";
import { parse } from "parse5";
import { readingPaths } from "../../src/data/journal-reading.mjs";
import { retiredGuides, retiredGuidePaths, retiredGuideCanonical } from "../src/retired-guides.ts";
import { journalPaths } from "../../src/data/journal.mjs";

const paths = readingPaths().map(path => `${path.slice(1)}index.html`);
const admitted = new Set([...readingPaths(), ...journalPaths(), "/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/"]);
const candidate = new URL("../build/", import.meta.url);
const published = new URL("../../.generated/content/", import.meta.url);
/** @typedef {{nodeName:string,tagName?:string,value?:string,attrs?:{name:string,value:string}[],childNodes?:MarkupNode[],parentNode?:MarkupNode|null}} MarkupNode */
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
/** @param {MarkupNode|undefined} node @param {string} name */
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
/** @param {MarkupNode} node @param {string} name */
const hasClass = (node, name) => (attr(node, "class") ?? "").split(/\s+/).includes(name);
/** @param {MarkupNode} node @returns {string} */
const text = node => ["script","style"].includes(node.tagName ?? "") || hasClass(node,"reading-diagram") || (node.tagName === "pre" && hasClass(node,"mermaid")) ? "" : node.nodeName === "#text" ? node.value ?? "" : (node.childNodes ?? []).map(text).join("");
/** @param {MarkupNode|undefined} node */
const normalized = node => { assert.ok(node); return text(node).replace(/\s+/g, " ").trim(); };
/** @param {Buffer} bytes */
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
/** @param {URL} directory @param {string|undefined} href */
const asset = (directory, href) => { assert.ok(href); return readFileSync(new URL(href.slice(1),directory)); };
/** @param {string} path */
function checker(path) {
/** @param {string|undefined} href */
const absolute = href => { assert.ok(href); return new URL(href, `https://devantler.tech/${path.replace('index.html','')}`).href; };
const reference = parse(readFileSync(new URL(path, published), "utf8"));
const original = all(reference).find(node => hasClass(node, "sl-markdown-content"));
assert.ok(original);

/** Compare complete real rendered content, not excerpts or expected source strings. @param {string} html */
return function check(html) {
  const document = all(parse(html));
  const body = document.find(node => hasClass(node, "sl-markdown-content"));
  assert.ok(body, "complete reading body required");
  const nodes = all(body);
  assert.ok(original);
  const previous = all(original);
  assert.equal(normalized(body), normalized(original), "complete prose/code/metadata/tag text must match the complete compiled source");
  const diagrams = nodes.filter(node => hasClass(node,"reading-diagram"));
  const expectedDiagrams = previous.filter(node => node.tagName === "pre" && hasClass(node,"mermaid"));
  /** @param {MarkupNode|undefined} node */
  const definition = node => { assert.ok(node, "diagram source is missing"); return (node.childNodes ?? []).map(child => child.value ?? "").join(""); };
  assert.deepEqual(diagrams.map(figure => {
    const details = all(figure).find(node => node.tagName === "details");
    assert.ok(details, "diagram text disclosure is missing");
    const summary = details.childNodes?.find(node => node.tagName === "summary");
    assert.ok(summary, "diagram text disclosure summary is missing");
    assert.equal(normalized(summary), "Diagram text", "diagram text disclosure summary must be Diagram text");
    return definition(all(details).find(node => node.tagName === "pre"));
  }), expectedDiagrams.map(definition), "complete authored diagram definitions survive independently of prose and code");
  const renderers = document.filter(node => node.tagName === "script" && /reading-diagrams\//.test(attr(node,"src") ?? ""));
  assert.equal(renderers.length, diagrams.length ? 1 : 0, "only diagram articles load the local renderer");
  for (const renderer of renderers) asset(candidate, attr(renderer,"src"));
  /** @param {MarkupNode[]} ns */
  const headings = ns => ns.filter(node => /^h[2-6]$/.test(node.tagName ?? "")).map(node => [node.tagName, attr(node,"id"), normalized(node)]);
  assert.deepEqual(headings(nodes), headings(previous), "every actual heading and bookmark is retained");
  /** @param {MarkupNode[]} ns */
  const copies = ns => ns.filter(node => node.tagName === "button").map(node => attr(node, "data-code"));
  assert.deepEqual(copies(nodes), copies(previous), "copy payloads follow the actual renderer, including its comment handling");
  const tables = nodes.filter(node => node.tagName === "table");
  const oldTables = previous.filter(node => node.tagName === "table");
  assert.deepEqual(tables.map(table => all(table).filter(node => ["th","td"].includes(node.tagName ?? "")).map(node => [node.tagName,normalized(node)])), oldTables.map(table => all(table).filter(node => ["th","td"].includes(node.tagName ?? "")).map(node => [node.tagName,normalized(node)])), "all native table cells retain their content and semantics");
  for (const table of tables) {
    const boundary = table.parentNode;
    assert.ok(boundary && hasClass(boundary,"reading-table") && attr(boundary,"tabindex") === "0" && attr(boundary,"role") === "region" && attr(boundary,"aria-label") === "Scrollable table", "each table requires a named keyboard-scrollable boundary");
  }
  /** @param {MarkupNode[]} ns */
  const links = ns => ns.filter(node => node.tagName === "a").map(node => [normalized(node), absolute(attr(node,"href"))]);
  assert.deepEqual(links(nodes), links(previous), "all authored, author and topic destinations survive");
  for (const link of nodes.filter(node => node.tagName === "a")) {
    const href = attr(link,"href");
    assert.ok(href);
    const destination = new URL(href, `https://devantler.tech/${path.replace('index.html','')}`);
    const local = destination.origin === "https://devantler.tech" && admitted.has(destination.pathname);
    if (!href.startsWith("#")) assert.ok(local ? href.startsWith("/") : /^[a-z][a-z0-9+.-]*:/i.test(href), "only admitted body destinations stay local");
  }
  /** @param {MarkupNode[]} ns */
  const times = ns => ns.filter(node => node.tagName === "time").map(node => [attr(node,"datetime"),normalized(node)]);
  assert.deepEqual(times(document), times(all(reference)), "publication and committed modification dates survive");
  const toc = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === "On this page");
  if (headings(previous).length) {
    assert.ok(toc);
    assert.deepEqual(all(toc).filter(node => node.tagName === "a").map(node => attr(node,"href")), headings(previous).map(item => `#${item[1]}`));
  } else assert.equal(toc, undefined, "prose-only articles have no empty contents control");
  /** @param {MarkupNode[]} ns */
  const adjacent = ns => ns.filter(node => node.tagName === "a" && ["prev","next"].includes(attr(node,"rel") ?? "")).map(node => [attr(node,"rel"),absolute(attr(node,"href")),normalized(node)]);
  const navigation = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === "More articles");
  const expectedAdjacent = adjacent(all(reference));
  if (expectedAdjacent.length) assert.ok(navigation);
  const actualAdjacent = navigation ? adjacent(all(navigation)) : [];
  assert.deepEqual(actualAdjacent, expectedAdjacent, "adjacent articles retain real titles and published destinations");
  assert.deepEqual(adjacent(document), expectedAdjacent, "one adjacent navigation, not a second set after the body");
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
    const actualSet = attr(image,"srcset")?.split(",").map(entry => entry.trim().split(/\s+/)) ?? [];
    const expectedSet = attr(images(previous)[i],"srcset")?.split(",").map(entry => entry.trim().split(/\s+/)) ?? [];
    assert.equal(actualSet.length, expectedSet.length, "every responsive image variant is retained");
    for (const [index, [href, descriptor]] of actualSet.entries()) {
      assert.equal(descriptor, expectedSet[index][1]);
      assert.equal(digest(asset(candidate,href)), digest(asset(published,expectedSet[index][0])), "responsive image bytes preserved");
    }
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
};
}

/** The actual emitted redirect controller, so a bookmark check exercises the shipped bytes. @param {MarkupNode[]} nodes */
const controller = nodes => {
  const scripts = nodes.filter(node => node.tagName === "script");
  assert.equal(scripts.length, 1, "a retired guide needs only its local bookmark controller");
  assert.match(attr(scripts[0], "src") ?? "", /^\/_app\/immutable\/assets\/project-redirect\.[A-Za-z0-9_-]+\.js$/);
  return scripts[0];
};
/** Every retired guide stays a noindex, bookmark-preserving redirect to where its content now lives (#697).
 * @param {import("../src/retired-guides.ts").RetiredGuidePath} route @param {string} html @param {string} script */
function checkRetired(route, html, script) {
  const {destination, fallback} = retiredGuides[route];
  const nodes = all(parse(html));
  assert.equal(attr(nodes.find(node => node.tagName === "link" && attr(node,"rel") === "canonical"), "href"), retiredGuideCanonical(route), "retired guide canonical");
  assert.equal(attr(nodes.find(node => node.tagName === "meta" && attr(node,"name") === "robots"), "content"), "noindex", "retired guide stays out of search results");
  assert.ok(nodes.some(node => node.tagName === "a" && attr(node,"href") === `${destination}${fallback}`), "retired guide retains a native continuation link");
  assert.ok(html.includes(`<noscript><meta http-equiv="refresh" content="0;url=${destination}${fallback}"`), "retired guide continues without JavaScript");
  const redirect = controller(nodes);
  for (const hash of ["", "#getting-started"]) {
    let target = "";
    runInNewContext(script, {
      document:{currentScript:{dataset:{destination:attr(redirect, "data-destination"),fallback:attr(redirect, "data-fallback")}}},
      location:{hash,replace:(/** @type {string} */ value) => { target = value; }},
    });
    assert.equal(target, `${destination}${hash || fallback}`, "the emitted redirect preserves the incoming bookmark");
  }
}

const sitemap = existsSync(new URL("sitemap-0.xml", candidate)) ? readFileSync(new URL("sitemap-0.xml", candidate), "utf8") : "";
for (const route of retiredGuidePaths) {
  const file = new URL(`${route.slice(1)}index.html`, candidate);
  if (process.argv[2] === "disabled") { assert.ok(!existsSync(file), "disabled retired-guide redirect must be removed"); continue; }
  assert.ok(existsSync(file), `retired guide ${route} must stay reachable for old bookmarks`);
  const html = readFileSync(file, "utf8");
  const script = asset(candidate, attr(controller(all(parse(html))), "src")).toString("utf8");
  checkRetired(route, html, script);
  assert.ok(!sitemap.includes(`<loc>https://devantler.tech${route}</loc>`), `retired guide ${route} is not advertised in the sitemap`);
  if (route === "/agentic-engineering/") {
    const post = all(parse(readFileSync(new URL(`${retiredGuides[route].destination.slice(1)}index.html`, candidate), "utf8")));
    const heading = post.find(node => attr(node, "id") === "the-process");
    assert.ok(heading && /^h[2-6]$/.test(heading.tagName ?? ""), "the old #the-process bookmark lands on the post's process section");
    assert.ok(post.some(node => hasClass(node, "reading-diagram")), "the post carries the process diagram");
  }
  if (route === "/templates/go/") {
    /** @type {[string,(value:string)=>string,string?][]} */
    const mutations = [
      ["indexable redirect", v => v.replace('name="robots"', 'name="missing-robots"')],
      ["wrong canonical", v => v.replace('rel="canonical"', 'rel="alternate"')],
      ["lost no-script continuation", v => v.replace("<noscript>", "<div>")],
      ["wrong destination", v => v.replaceAll("application-template", "go-template")],
      ["dropped bookmark", v => v, script.replace("location.hash", "''")],
    ];
    for (const [name, mutate, changedScript] of mutations) {
      assert.ok(mutate(html) !== html || changedScript !== script, `mutation must reach actual output: ${name}`);
      assert.throws(() => checkRetired(route, mutate(html), changedScript ?? script), `reject ${name}`);
    }
  }
}
if (process.argv[2] !== "disabled") console.log(`Svelte reading: all ${retiredGuidePaths.length} retired guide addresses redirect with their bookmarks, stay out of the sitemap, and reject five actual-output losses`);

for (const path of paths) {
if (process.argv[2] === "disabled") {
  assert.ok(!existsSync(new URL(path,candidate)), "disabled reading route must be removed after enabled output");
  const assets = new URL("_app/immutable/assets/",candidate);
  assert.ok(!existsSync(assets) || !readdirSync(assets).some(name => name.startsWith("reading-")), "disabled reading assets must also be absent");
} else {
  assert.equal(process.argv[2], "enabled");
  const html = readFileSync(new URL(path,candidate), "utf8");
  const check = checker(path);
  check(html);
  if (path === "blog/autonomous-oss-with-github-agentic-workflows/index.html") {
    /** @type {[string,(value:string)=>string][]} */
    const disclosureMutations = [
      ["missing disclosure", v => v.replace(/<details><summary>Diagram text<\/summary>([\s\S]*?)<\/details>/, "$1")],
      ["missing disclosure summary", v => v.replace("<summary>Diagram text</summary>", "")],
      ["wrong disclosure summary", v => v.replace("<summary>Diagram text</summary>", "<summary>Other content</summary>")],
    ];
    for (const [name, mutate] of disclosureMutations) {
      const changed = mutate(html);
      assert.notEqual(changed, html, `mutation must reach actual output: ${name}`);
      assert.throws(() => check(changed), /diagram text disclosure/, `reject ${name}`);
    }
    console.log("Svelte reading: three actual diagram-disclosure losses rejected");
  }
  if (path === "blog/why-i-chose-the-polyform-shield-license-for-ksail/index.html") {
    const missingBoundary = html.replace('class="reading-table"', 'class="missing-table-boundary"');
    assert.notEqual(missingBoundary,html);
    assert.throws(() => check(missingBoundary), /keyboard-scrollable boundary/);
    console.log("Svelte reading: actual comparison-table boundary loss rejected");
  }
  if (path !== "blog/building-ksail-from-shell-to-dotnet-to-go/index.html") continue;
  const reference = parse(readFileSync(new URL(path, published), "utf8"));
  /** @type {[string,(value:string)=>string,RegExp][]} */
  const mutations = [
    ["lost complete body", v => v.replace('class="sl-markdown-content"', 'class="lost-body"'), /complete reading body/],
    ["missing paragraph", v => v.replace(/<p>Building a developer tool[\s\S]*?<\/p>/, ""), /complete prose/],
    ["broken bookmark", v => v.replace('id="the-problem"', 'id="broken-problem"'), /bookmark/],
    ["wrong clipboard text", v => v.replace('data-code="', 'data-code="wrong'), /copy payload/],
    ["wrong destination", v => v.replace('href="https://github.com/devantler"', 'href="https://example.com/"'), /destinations/],
    ["fabricated modified date", v => v.replace(`datetime="${attr(all(reference).filter(node => node.tagName === "time").at(-1),"datetime")}"`, 'datetime="2000-01-01T00:00:00.000Z"'), /dates survive/],
    ["missing sharing metadata", v => v.replace('property="og:title"', 'property="wrong:title"'), /sharing metadata/],
    ["duplicate sharing description", v => v.replace(/(<meta\b[^>]*property="og:description"[^>]*>)/, '$1$1'), /sharing metadata/],
    ["admitted topic sent back to production", v => v.replace('href="/blog/tags/ksail/"', 'href="https://devantler.tech/blog/tags/ksail/"'), /only admitted body destinations/],
  ];
  for (const [name, mutate, failure] of mutations) {
    const changed = mutate(html);
    assert.notEqual(changed, html, `mutation must reach actual output: ${name}`);
    assert.throws(() => check(changed), failure, `reject ${name}`);
  }
  console.log(`Svelte reading: ${mutations.length} independent output-rejection controls verified`);
}
}
console.log(`Svelte reading: all ${readingPaths().length} articles ${process.argv[2] === "enabled" ? "match compiled content bodies, dates, metadata, assets, bookmarks, copy controls and destinations" : "and dependent assets absent"}`);
