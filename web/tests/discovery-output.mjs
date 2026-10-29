import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { parse } from "parse5";
import { XMLParser, XMLValidator } from "fast-xml-parser";
import { journalPaths } from "../../src/data/journal.mjs";
import { readingPaths, guidePaths } from "../../src/data/journal-reading.mjs";

/** Read the complete actual artifact, not a success-shaped subset. @param {string} root @returns {Map<string,Buffer>} */
export function artifact(root) {
  const files = new Map();
  /** @param {string} directory @param {string} prefix */
  const walk = (directory, prefix = "") => {
    for (const entry of readdirSync(directory, {withFileTypes:true})) {
      const path = `${prefix}${entry.name}`;
      if (entry.isDirectory()) walk(join(directory,entry.name), `${path}/`);
      else { assert.ok(entry.isFile(), "Public discovery requires regular artifact files"); files.set(path,readFileSync(join(directory,entry.name))); }
    }
  };
  walk(root);
  return files;
}
/** @param {import('parse5').DefaultTreeAdapterMap['node']} node @returns {import('parse5').DefaultTreeAdapterMap['element'][]} */
const all = node => [...("tagName" in node ? [node] : []),...("childNodes" in node ? node.childNodes.flatMap(all) : [])];
/** @param {import('parse5').DefaultTreeAdapterMap['element']|undefined} node @param {string} name */
const attr = (node,name) => node?.attrs.find(item => item.name === name)?.value;
/** @param {Buffer} bytes */
const nodes = bytes => all(parse(bytes.toString()));
/** @param {ReturnType<typeof nodes>} elements @param {string} key */
const meta = (elements,key) => elements.filter(node => node.tagName === "meta" && (attr(node,"property") === key || attr(node,"name") === key)).map(node => attr(node,"content") ?? "");
/** @param {ReturnType<typeof nodes>} elements @param {string} rel */
const links = (elements,rel) => elements.filter(node => node.tagName === "link" && attr(node,"rel") === rel);
const origin = "https://devantler.tech";
/** @param {string} path */
const fileFor = path => path === "/" ? "index.html" : `${path.slice(1)}index.html`;
/** @param {Buffer} bytes */
const xml = bytes => { assert.equal(XMLValidator.validate(bytes.toString()),true,"Public discovery XML must be complete"); return new XMLParser().parse(bytes.toString()); };
/** @param {any} value @returns {string[]} */
const locations = value => (Array.isArray(value) ? value : [value]).map(item => item.loc);
/** @param {Map<string,Buffer>} files @param {string} path */
const get = (files,path) => { const value=files.get(path); assert.ok(value,`Public discovery missing file: ${path}`); return value; };

/** @param {Map<string,Buffer>} candidate @param {Map<string,Buffer>} baseline */
export function assertDownloads(candidate, baseline) {
  for (const path of ["pdfs/nikolai-emil-damm-cv.pdf","pdfs/thesis.pdf","favicon.png","robots.txt"]) {
    assert.ok(get(candidate,path).equals(get(baseline,path)),`Public discovery authoritative bytes changed: ${path}`);
    if (path.endsWith(".pdf")) {
      assert.match(get(candidate,path).subarray(0,8).toString(),/^%PDF-1\./,"Public discovery requires a real PDF");
      assert.match(get(candidate,path).subarray(-40).toString(),/%%EOF\s*$/,"Public discovery PDF must be complete");
    }
  }
  assert.ok(!candidate.has("CNAME") && !candidate.has("publication-source.json"),"Public discovery does not own publisher configuration or receipts");
}

/** @param {Map<string,Buffer>} candidate @param {Map<string,Buffer>} baseline */
export function assertSitemap(candidate, baseline) {
  const index = locations(xml(get(candidate,"sitemap-index.xml")).sitemapindex.sitemap);
  assert.deepEqual(index,[`${origin}/sitemap-0.xml`],"Public discovery sitemap relationship");
  const actual = locations(xml(get(candidate,"sitemap-0.xml")).urlset.url);
  const baselineLocations = baseline.has("sitemap-0.xml") ? locations(xml(get(baseline,"sitemap-0.xml")).urlset.url) : null;
  const expected = baselineLocations ? baselineLocations.filter(href => {
    const page = baseline.get(fileFor(new URL(href).pathname));
    if (!page) return false;
    const elements = nodes(page);
    return !meta(elements,"robots").some(value => /\bnoindex\b/i.test(value)) && attr(links(elements,"canonical")[0],"href") === href;
  }) : ["/","/da/","/about/","/da/about/","/projects/","/da/projects/",...(process.env.FEATURE_COMPANY_IDENTITY === "true" ? ["/company/","/da/company/"] : []),...journalPaths(),...readingPaths(),...guidePaths()].map(path=>origin+path);
  assert.ok(expected.length > 10,"Public discovery baseline must contain real route coverage");
  assert.equal(new Set(actual).size,actual.length,"Public discovery duplicate sitemap entry");
  assert.deepEqual([...actual].sort(),[...expected].sort(),"Public discovery complete canonical route membership");
  for (const href of actual) {
    const url = new URL(href);
    assert.equal(url.origin,origin,"Public discovery sitemap stays on the canonical origin");
    const elements = nodes(get(candidate,fileFor(url.pathname)));
    assert.equal(attr(links(elements,"canonical")[0],"href"),href,"Public discovery destination canonical");
    assert.ok(!meta(elements,"robots").some(value => /\bnoindex\b/i.test(value)),"Public discovery excludes noindex pages");
  }
  return actual.length;
}

/** @param {Map<string,Buffer>} candidate @param {Map<string,Buffer>} baseline */
export function assertSharing(candidate, baseline) {
  for (const path of ["/","/da/","/about/","/da/about/","/projects/","/da/projects/"]) {
    const elements = nodes(get(candidate,fileFor(path)));
    const referencePage = baseline.get(fileFor(path));
    const reference = referencePage ? nodes(referencePage) : [];
    const hasReference = meta(reference,"og:title").length > 0;
    if (path.includes("projects")) {
      assert.doesNotMatch(meta(elements,"description").join(" "),/family projects|familieprojekter/i,"Public discovery must use the professional portfolio description");
    }
    for (const key of ["description","og:title","og:description","og:type","og:url","og:locale","og:image","og:image:alt","twitter:card"]) {
      if (hasReference) assert.deepEqual(meta(elements,key),meta(reference,key),`Public discovery metadata ${key} at ${path}`);
      assert.equal(meta(elements,key).length,1,`Public discovery metadata must be unique: ${key}`);
      assert.ok(meta(elements,key)[0].trim(), `Public discovery metadata must be nonempty: ${key}`);
    }
    for (const [rel,expected] of [["canonical",origin+path],["sitemap","/sitemap-index.xml"],["icon","/favicon.png"]]) assert.deepEqual(links(elements,rel).map(node=>attr(node,"href")),[expected],`Public discovery ${rel} at ${path}`);
    const suffix = path.replace(/^\/da\//,"/");
    assert.deepEqual(links(elements,"alternate").filter(node=>attr(node,"hreflang")).map(node=>[attr(node,"hreflang"),attr(node,"href")]),[["en",origin+suffix],["da",origin+"/da"+suffix],["x-default",origin+suffix]],`Public discovery languages at ${path}`);
    assert.deepEqual(meta(elements,"og:locale"),[path.startsWith("/da/")?"da_DK":"en_GB"],"Public discovery locale");
    assert.deepEqual(meta(elements,"og:url"),[origin+path],"Public discovery social canonical");
    assert.deepEqual(meta(elements,"og:description"),meta(elements,"description"),"Public discovery social description");
    const portrait = new URL(meta(elements,"og:image")[0]);
    assert.equal(portrait.origin,origin,"Public discovery local sharing portrait");
    assert.ok(get(candidate,portrait.pathname.slice(1)).equals(get(baseline,portrait.pathname.slice(1))),"Public discovery sharing image dependency retained");
    if (path.includes("about")) {
      const cv = elements.find(node => node.tagName === "a" && attr(node,"href") === "/pdfs/nikolai-emil-damm-cv.pdf");
      assert.ok(cv,"Public discovery CV download stays local");
      assert.ok(cv.attrs.some(item => item.name === "download"),"Public discovery CV retains explicit native download behavior");
    }
    if (path.includes("projects")) assert.ok(elements.some(node => node.tagName === "a" && attr(node,"href") === "/pdfs/thesis.pdf"),"Public discovery thesis download stays local");
  }
}

/** Downloads are source-bound; canonical route/locale expectations are independent of adapter output. @param {Map<string,Buffer>} candidate @param {Map<string,Buffer>} baseline */
export function assertDiscovery(candidate,baseline) { assertDownloads(candidate,baseline); const count=assertSitemap(candidate,baseline); assertSharing(candidate,baseline); return count; }
