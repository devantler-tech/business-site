import assert from "node:assert/strict";
import {readFileSync,readdirSync,existsSync,renameSync} from "node:fs";
import {createHash} from "node:crypto";
import {parse} from "parse5";
import {readJournal,journalPaths} from "../../src/data/journal.mjs";
import {readingPaths} from "../../src/data/journal-reading.mjs";
import {assertFeedParity,validateJournalFeed,feedAssetPaths} from "../../src/data/journal-feed.mjs";

const candidate = new URL("../build/",import.meta.url);
const published = new URL("../../dist/",import.meta.url);
const feedPath = "blog/rss.xml";
const enabled = process.argv[2] === "enabled";
assert.ok(enabled || process.argv[2] === "disabled","declare enabled or disabled feed");
if (!enabled) {
  assert.ok(!existsSync(new URL(feedPath,candidate)),"disabled candidate feed is absent");
  const assets = new URL("_astro/",candidate);
  assert.ok(!existsSync(assets) || readdirSync(assets).length === 0,"disabled feed image assets are absent");
  console.log("Svelte feed: XML and dependent image assets absent");
} else {
  const posts = readJournal();
  const baseline = readFileSync(new URL(feedPath,published),"utf8");
  const xml = readFileSync(new URL(feedPath,candidate),"utf8");
  const check = (/** @type {string} */ value) => assertFeedParity(value,baseline,posts);
  check(xml);
  /** @type {[string,(value:string)=>string][]} */
  const mutations = [
    ["missing item",value => value.replace(/<item>[\s\S]*?<\/item>/,"")],
    ["changed GUID",value => value.replace(/(<guid\b[^>]*>)/,"$1changed")],
    ["changed date",value => value.replace(/<pubDate>[^<]+<\/pubDate>/,"<pubDate>Thu, 01 Jan 1970 00:00:00 GMT</pubDate>")],
    ["lost full content",value => value.replace(/<content:encoded>[\s\S]*?<\/content:encoded>/,"<content:encoded>Excerpt only</content:encoded>")],
    ["wrong canonical destination",value => value.replace('href="https://devantler.tech/blog/rss.xml"','href="https://example.com/rss.xml"')],
    ["malformed XML",value => value.replace("</channel>","")],
  ];
  for (const [name,mutate] of mutations) {
    const changed = mutate(xml);
    assert.notEqual(changed,xml,`mutation must reach actual output: ${name}`);
    assert.throws(() => check(changed),/Journal feed/,`reject ${name}`);
  }
  const paths = feedAssetPaths(validateJournalFeed(xml,posts));
  assert.ok(paths.length > 1,"actual feed contains independent image dependencies");
  const digest = (/** @type {Buffer} */ value) => createHash("sha256").update(value).digest("hex");
  const checkAssets = () => {
    for (const path of paths) assert.equal(digest(readFileSync(new URL(path,candidate))),digest(readFileSync(new URL(path,published))),"every feed image and responsive variant retains actual bytes");
  };
  checkAssets();
  const file = new URL(paths[1],candidate);
  const held = new URL(`${paths[1]}.negative-control`,candidate);
  assert.ok(!existsSync(held));
  renameSync(file,held);
  try { assert.throws(checkAssets,/ENOENT/,"missing later image beside healthy images fails"); }
  finally { renameSync(held,file); }
  checkAssets();
  /** @param {import('parse5').DefaultTreeAdapterMap['node']} node @returns {import('parse5').DefaultTreeAdapterMap['element'][]} */
  const all = node => [...("tagName" in node ? [node] : []),...("childNodes" in node ? node.childNodes.flatMap(all) : [])];
  for (const path of [...journalPaths(),...readingPaths()]) {
    const elements = all(parse(readFileSync(new URL(`${path.slice(1)}index.html`,candidate),"utf8")));
    const attr = (/** @type {import('parse5').DefaultTreeAdapterMap['element']} */ node,/** @type {string} */ name) => node.attrs.find(item => item.name === name)?.value;
    const discovery = elements.filter(node => node.tagName === "link" && attr(node,"rel") === "alternate" && attr(node,"type") === "application/rss+xml");
    assert.equal(discovery.length,1,"one feed discovery link per archive/article");
    assert.equal(attr(discovery[0],"href"),"/blog/rss.xml","feed discovery stays local");
    assert.ok(elements.some(node => node.tagName === "a" && attr(node,"href") === "/blog/rss.xml"),"visible subscription journey stays local");
  }
  console.log(`Svelte feed: ${posts.length} complete actual items, ${paths.length} image variants, discovery and seven actual rejection controls verified`);
}
