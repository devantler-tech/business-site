import test from "node:test";
import assert from "node:assert/strict";

const posts = [{title:"An authored article",description:"A useful description",date:"2026-01-07T00:00:00.000Z",href:"https://devantler.tech/blog/example/"}];
const fixture = `<?xml version="1.0"?><rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:fh="http://purl.org/syndication/history/1.0"><channel><title>Devantler Tech | Journal</title><description>Authored work</description><link>https://devantler.tech/</link><language>en</language><fh:complete/><atom:link rel="self" href="https://devantler.tech/blog/rss.xml"/><item><title>An authored article</title><link>${posts[0].href}</link><guid isPermaLink="true">${posts[0].href}</guid><description>A useful description</description><pubDate>Wed, 07 Jan 2026 00:00:00 GMT</pubDate><content:encoded><![CDATA[<p>Complete authored content, including æ, ø and å.</p><img src="https://devantler.tech/_astro/cover.hash.webp" srcset="/_astro/cover.small.webp 640w, /_astro/cover.large.webp 1280w">]]></content:encoded><category>software</category></item></channel></rss>`;

async function adapter() {
  return import("../../src/data/journal-feed.mjs");
}

test("the complete feed retains channel, authored identity, content and local image variants", async () => {
  const {validateJournalFeed,feedAssetPaths,assertFeedParity} = await adapter();
  const feed = validateJournalFeed(fixture,posts);
  assert.equal(feed.rss.channel.item.length,1);
  assert.match(feed.rss.channel.item[0]["content:encoded"],/æ, ø and å/);
  assert.deepEqual(feedAssetPaths(feed),["_astro/cover.hash.webp","_astro/cover.small.webp","_astro/cover.large.webp"]);
  assert.doesNotThrow(() => assertFeedParity(fixture,fixture,posts));
});

for (const [name,mutate] of [
  ["missing item",(xml:string) => xml.replace(/<item>[\s\S]*?<\/item>/,"")],
  ["changed GUID",(xml:string) => xml.replace('<guid isPermaLink="true">','<guid isPermaLink="true">changed')],
  ["changed date",(xml:string) => xml.replace("Wed, 07 Jan 2026", "Thu, 08 Jan 2026")],
  ["invalid date",(xml:string) => xml.replace("Wed, 07 Jan 2026 00:00:00 GMT", "not a date")],
  ["lost full content",(xml:string) => xml.replace("Complete authored content", "Excerpt only")],
  ["wrong item destination",(xml:string) => xml.replace(`<link>${posts[0].href}</link>`,"<link>https://example.com/</link>")],
  ["wrong self destination",(xml:string) => xml.replace('href="https://devantler.tech/blog/rss.xml"','href="https://example.com/rss.xml"')],
  ["missing language",(xml:string) => xml.replace("<language>en</language>","")],
  ["wrong content namespace",(xml:string) => xml.replace("http://purl.org/rss/1.0/modules/content/", "https://example.com/not-content/")],
  ["malformed XML",(xml:string) => xml.replace("</channel>","")],
  ["entity declaration",(xml:string) => xml.replace("<rss ",'<!DOCTYPE rss [<!ENTITY source "private">]><rss ')],
] as const) test(`feed parity rejects ${name}`, async () => {
  const {assertFeedParity} = await adapter();
  const changed = mutate(fixture);
  assert.notEqual(changed,fixture);
  assert.throws(() => assertFeedParity(changed,fixture,posts),/Journal feed/);
});

for (const [name,href] of [
  ["external image","https://example.com/cover.webp"],
  ["non-image dependency","https://devantler.tech/_astro/payload.js"],
  ["query-bearing dependency","https://devantler.tech/_astro/cover.webp?other=1"],
] as const) test(`feed asset closure rejects ${name} without a request`, async () => {
  const {validateJournalFeed,feedAssetPaths} = await adapter();
  const changed = fixture.replace("https://devantler.tech/_astro/cover.hash.webp",href);
  assert.throws(() => feedAssetPaths(validateJournalFeed(changed,posts)),/Journal feed unsupported image dependency/);
});
