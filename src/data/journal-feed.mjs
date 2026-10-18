import assert from "node:assert/strict";
import { XMLParser, XMLValidator } from "fast-xml-parser";
import { parseFragment } from "parse5";

const parser = new XMLParser({ignoreAttributes:false,parseTagValue:false,trimValues:false,isArray:name => name === "item"});

/** Validate a complete, authored English feed before it can enter the candidate.
 * @param {string} xml @param {{title:string,date:string,href:string}[]} posts
 */
export function validateJournalFeed(xml, posts) {
  assert.ok(typeof xml === "string" && !/<!DOCTYPE|<!ENTITY/i.test(xml), "Journal feed must not declare entities");
  assert.equal(XMLValidator.validate(xml),true,"Journal feed XML is malformed");
  const feed = parser.parse(xml);
  const channel = feed.rss?.channel;
  assert.ok(channel && !Array.isArray(channel),"Journal feed requires one channel");
  assert.equal(feed.rss["@_version"],"2.0","Journal feed version required");
  assert.equal(feed.rss["@_xmlns:atom"],"http://www.w3.org/2005/Atom","Journal feed Atom namespace required");
  assert.equal(feed.rss["@_xmlns:content"],"http://purl.org/rss/1.0/modules/content/","Journal feed content namespace required");
  assert.equal(feed.rss["@_xmlns:fh"],"http://purl.org/syndication/history/1.0","Journal feed history namespace required");
  assert.equal(channel.language,"en","Journal feed language required");
  assert.equal(channel.link,"https://devantler.tech/","Journal feed canonical identity required");
  assert.equal(channel["atom:link"]?.["@_rel"],"self","Journal feed self link required");
  assert.equal(channel["atom:link"]?.["@_href"],"https://devantler.tech/blog/rss.xml","Journal feed self destination required");
  assert.ok(Object.hasOwn(channel,"fh:complete"),"Journal feed is incomplete; archive support needs explicit migration");
  assert.ok(typeof channel.title === "string" && channel.title.trim(),"Journal feed title required");
  assert.ok(Array.isArray(channel.item),"Journal feed item inventory required");
  assert.deepEqual(channel.item.map((/** @type {any} */ item) => {
    const date = new Date(item.pubDate);
    assert.ok(Number.isFinite(date.getTime()),"Journal feed publication date required");
    return [item.title,item.link,item.guid?.["#text"],item.guid?.["@_isPermaLink"],date.toISOString()];
  }), posts.map(post => [post.title,post.href,post.href,"true",post.date]),"Journal feed complete authored item identity/order/date required");
  assert.ok(channel.item.every((/** @type {any} */ item) => typeof item.description === "string" && item.description.trim() && typeof item["content:encoded"] === "string" && item["content:encoded"].trim()),"Journal feed complete description/content required");
  return feed;
}

/** Compare all meaningful XML fields, including full HTML and namespace attributes.
 * @param {string} actual @param {string} baseline @param {{title:string,date:string,href:string}[]} posts
 */
export function assertFeedParity(actual, baseline, posts) {
  assert.deepEqual(validateJournalFeed(actual,posts),validateJournalFeed(baseline,posts),"Journal feed complete baseline parity required");
}

/** Keep the feed's canonical image URLs working after the old frontend retires.
 * @param {ReturnType<typeof validateJournalFeed>} feed @returns {string[]}
 */
export function feedAssetPaths(feed) {
  const paths = new Set();
  /** @param {string} href */
  const add = href => {
    const url = new URL(href,"https://devantler.tech/blog/rss.xml");
    assert.ok(url.origin === "https://devantler.tech" && !url.search && !url.hash && /^\/_astro\/[A-Za-z0-9._-]+\.(?:webp|png|jpg)$/.test(url.pathname),"Journal feed unsupported image dependency");
    paths.add(url.pathname.slice(1));
  };
  /** @param {import('parse5').DefaultTreeAdapterMap['node']} node */
  function visit(node) {
    if ("tagName" in node && ["img","source"].includes(node.tagName)) for (const attr of node.attrs) {
      if (attr.name === "src") add(attr.value);
      if (attr.name === "srcset") for (const entry of attr.value.split(",")) {
        const match = entry.trim().match(/^(\S+)\s+[1-9]\d*w$/);
        assert.ok(match,"Journal feed unsupported responsive image dependency");
        add(match[1]);
      }
    }
    if ("childNodes" in node) for (const child of node.childNodes) visit(child);
  }
  for (const item of feed.rss.channel.item) visit(parseFragment(item["content:encoded"]));
  return [...paths];
}
