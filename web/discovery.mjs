import assert from "node:assert/strict";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "parse5";

/** @param {import('parse5').DefaultTreeAdapterMap['node']} node @returns {import('parse5').DefaultTreeAdapterMap['element'][]} */
const all = node => [...("tagName" in node ? [node] : []),...("childNodes" in node ? node.childNodes.flatMap(all) : [])];
/** @param {import('parse5').DefaultTreeAdapterMap['element']} node @param {string} name */
const attr = (node,name) => node.attrs.find(item => item.name === name)?.value;

/** Derive membership only after the native static adapter has emitted its admitted HTML. @param {string} directory */
export function writeDiscovery(directory) {
  /** @type {string[]} */
  const urls = [];
  /** @param {string} prefix */
  function walk(prefix = "") {
    for (const entry of readdirSync(join(directory,prefix),{withFileTypes:true})) {
      const path = `${prefix}${entry.name}`;
      if (entry.isDirectory()) { walk(`${path}/`); continue; }
      if (!path.endsWith(".html")) continue;
      assert.ok(entry.isFile() && path.endsWith("index.html"),"Public discovery unexpected HTML artifact");
      const elements = all(parse(readFileSync(join(directory,path),"utf8")));
      const canonical = elements.filter(node => node.tagName === "link" && attr(node,"rel") === "canonical");
      assert.equal(canonical.length,1,"Public discovery requires one page canonical");
      const href = attr(canonical[0],"href");
      const url = new URL(/** @type {string} */ (href));
      assert.ok(url.origin === "https://devantler.tech" && !url.hash && !url.search,"Public discovery canonical must be local and unambiguous");
      const noindex = elements.some(node => node.tagName === "meta" && attr(node,"name") === "robots" && /\bnoindex\b/i.test(attr(node,"content") ?? ""));
      if (!noindex && url.pathname === `/${path.slice(0,-"index.html".length)}`) urls.push(url.href);
    }
  }
  walk();
  assert.ok(urls.length > 0 && new Set(urls).size === urls.length,"Public discovery requires distinct real canonical pages");
  const escape = (/** @type {string} */ value) => value.replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll('"',"&quot;");
  writeFileSync(join(directory,"sitemap-0.xml"),`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls.sort().map(href => `<url><loc>${escape(href)}</loc></url>`).join("")}</urlset>`);
  writeFileSync(join(directory,"sitemap-index.xml"),'<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>https://devantler.tech/sitemap-0.xml</loc></sitemap></sitemapindex>');
}
