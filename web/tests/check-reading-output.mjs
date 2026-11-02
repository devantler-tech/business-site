import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { runInNewContext } from "node:vm";
import { parse, serialize } from "parse5";
import { readingPaths, guidePaths } from "../../src/data/journal-reading.mjs";
import { journalPaths } from "../../src/data/journal.mjs";

const paths = [...readingPaths(), ...guidePaths()].map(path => `${path.slice(1)}index.html`);
const admitted = new Set([...readingPaths(), ...guidePaths(), ...journalPaths(), "/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/"]);
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
const technical = !path.startsWith("blog/");
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
  const adjacent = ns => ns.filter(node => node.tagName === "a" && ["prev","next"].includes(attr(node,"rel") ?? "")).map(node => [attr(node,"rel"),absolute(attr(node,"href")),normalized(technical ? all(node).find(child => hasClass(child,"link-title")) : node)]);
  const navigation = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === (technical ? "More guides" : "More articles"));
  const expectedAdjacent = adjacent(all(reference));
  if (expectedAdjacent.length) assert.ok(navigation);
  const actualAdjacent = navigation ? adjacent(all(navigation)) : [];
  assert.deepEqual(actualAdjacent, expectedAdjacent, "adjacent articles retain real titles and published destinations");
  assert.deepEqual(adjacent(document), expectedAdjacent, "one adjacent navigation, not a second set after the body");
  if (technical && navigation) for (const link of all(navigation).filter(node => node.tagName === "a")) {
    const href = attr(link,"href");
    assert.ok(href);
    if (admitted.has(new URL(href,"https://devantler.tech").pathname)) assert.ok(href.startsWith("/"), "admitted adjacent guides stay local");
  }
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
    if (/^https:\/\//.test(attr(images(previous)[i],"src") ?? "")) {
      assert.equal(attr(image,"src"), attr(images(previous)[i],"src"), "authored external image destination retained without build-time fetch");
      continue;
    }
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

const legacyTemplate = new URL("templates/gitops-tenant-template/index.html", candidate);
if (process.argv[2] === "disabled") {
  assert.ok(!existsSync(legacyTemplate), "disabled legacy template redirect must be removed");
} else {
  assert.ok(existsSync(legacyTemplate), "enabled legacy template redirect required beside healthy guides");
  const redirectNodes = all(parse(readFileSync(legacyTemplate, "utf8")));
  assert.equal(attr(redirectNodes.find(node => node.tagName === "link" && attr(node,"rel") === "canonical"), "href"), "https://devantler.tech/templates/platform-tenant-template/");
  assert.equal(attr(redirectNodes.find(node => node.tagName === "meta" && attr(node,"name") === "robots"), "content"), "noindex");
  assert.ok(redirectNodes.some(node => node.tagName === "a" && attr(node,"href") === "/templates/platform-tenant-template/#_top"), "legacy redirect retains a native continuation link");
  const redirect = redirectNodes.filter(node => node.tagName === "script");
  assert.equal(redirect.length, 1, "redirect needs only its local bookmark controller");
  assert.equal(attr(redirect[0], "data-destination"), "/templates/platform-tenant-template/");
  assert.equal(attr(redirect[0], "data-fallback"), "#_top");
  const href = attr(redirect[0], "src");
  assert.match(href ?? "", /^\/_app\/immutable\/assets\/project-redirect\.[A-Za-z0-9_-]+\.js$/);
  const actualScript = asset(candidate, href).toString("utf8");
  for (const hash of ["", "#getting-started", "#what-the-template-owns-vs-what-you-own"]) {
    let destination = "";
    runInNewContext(actualScript, {
      document:{currentScript:{dataset:{destination:attr(redirect[0], "data-destination"),fallback:attr(redirect[0], "data-fallback")}}},
      location:{hash,replace:(/** @type {string} */ value) => { destination = value; }},
    });
    assert.equal(destination, `/templates/platform-tenant-template/${hash || "#_top"}`, "actual emitted legacy redirect preserves the incoming bookmark");
  }
  assert.match(readFileSync(legacyTemplate, "utf8"), /<noscript><meta http-equiv="refresh" content="0;url=\/templates\/platform-tenant-template\/#_top"/);
  console.log("Svelte guides: actual legacy redirect retains three bookmark cases and a no-script continuation");
}

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
  if (!path.startsWith("blog/")) {
    assert.ok(!html.includes("/da/templates/") && !html.includes("/da/agentic-engineering/"));
    const document = all(parse(html));
    const navigation = document.find(node => node.tagName === "nav" && attr(node,"aria-label") === "Technical guides");
    assert.ok(navigation, "guide collection navigation required");
    assert.deepEqual(all(navigation).filter(node => node.tagName === "a").map(node => attr(node,"href")), guidePaths(), "every guide is reachable locally");
    if (path === "templates/go/index.html") {
      /** @param {(node:MarkupNode)=>boolean} predicate @param {(node:MarkupNode)=>void} change */
      const corrupt = (predicate, change) => {
        const tree = parse(html);
        const body = all(tree).find(node => hasClass(node, "sl-markdown-content"));
        assert.ok(body);
        const node = all(body).find(predicate);
        assert.ok(node, "negative control must reach actual guide output");
        change(node);
        return serialize(tree);
      };
      /** @type {[string,(node:MarkupNode)=>boolean,(node:MarkupNode)=>void,RegExp][]} */
      const mutations = [
        ["lost complete body", node => hasClass(node,"sl-markdown-content"), node => { node.attrs = []; }, /complete reading body/],
        ["missing paragraph", node => node.tagName === "p", node => { node.childNodes = []; }, /complete prose/],
        ["lost bookmark", node => node.tagName === "h2", node => { node.attrs = (node.attrs ?? []).filter(item => item.name !== "id"); }, /bookmark/],
        ["wrong copy payload", node => node.tagName === "button", node => { const payload = node.attrs?.find(item => item.name === "data-code"); assert.ok(payload); payload.value = "wrong"; }, /copy payload/],
      ];
      for (const [name,predicate,change,failure] of mutations) assert.throws(() => check(corrupt(predicate,change)), failure, `reject ${name} beside healthy guides`);
      console.log("Svelte guides: four independent actual-output body/bookmark/copy losses rejected");
    }
  }
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
console.log(`Svelte reading: all ${readingPaths().length} articles and ${guidePaths().length} technical guides ${process.argv[2] === "enabled" ? "match compiled content bodies, dates, metadata, assets, bookmarks, copy controls and destinations" : "and dependent assets absent"}`);
