import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import test from "node:test";
import { parse } from "parse5";

const root = new URL("../dist/", import.meta.url);
const target = "/templates/application-template/";
// Headings the former platform tenant template guide published; old bookmarks must still land.
const bookmarks = ["#whats-inside", "#what-the-template-owns-vs-what-you-own", "#getting-started", "#links"];
const legacyNames = ["gitops-tenant-template", "platform-tenant-template"];
const pages = new Map(legacyNames.map(name => [name, readFileSync(new URL(`templates/${name}/index.html`, root), "utf8")]));
const guide = readFileSync(new URL(`${target.slice(1)}index.html`, root), "utf8");
const sitemap = readFileSync(new URL("sitemap-0.xml", root), "utf8");
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
const attr = (node, name) => node?.attrs?.find(attribute => attribute.name === name)?.value;

/** Execute the actual emitted local controller with the page's own data attributes. */
function destination(html, hash) {
  const script = all(parse(html)).find(node => node.tagName === "script" && attr(node, "data-destination") !== undefined);
  assert.ok(script, "the redirect carries its bookmark controller");
  const src = attr(script, "src");
  assert.match(src ?? "", /^\/_app\/immutable\/assets\/project-redirect\.[A-Za-z0-9_-]+\.js$/, "the controller is the local bundled asset");
  let result;
  runInNewContext(readFileSync(new URL(src.slice(1), root), "utf8"), {
    document: { currentScript: { dataset: { destination: attr(script, "data-destination"), fallback: attr(script, "data-fallback") } } },
    location: { hash, replace: value => { result = value; } },
  });
  return result;
}

/** Check the generated route beside the independently maintained, healthy destination guide. */
function check(html) {
  assert.equal(typeof html, "string", "legacy redirect must be emitted beside the guide");
  const nodes = all(parse(html));
  assert.equal(attr(nodes.find(node => node.tagName === "link" && attr(node, "rel") === "canonical"), "href"), `https://devantler.tech${target}`);
  assert.equal(attr(nodes.find(node => node.tagName === "meta" && attr(node, "name") === "robots"), "content"), "noindex");
  assert.ok(nodes.some(node => node.tagName === "a" && attr(node, "href") === `${target}#_top`), "native continuation stays useful without JavaScript");
  assert.match(html, /<noscript><meta http-equiv="refresh" content="0;url=\/templates\/application-template\/#_top"/);
  for (const hash of ["", ...bookmarks]) assert.equal(destination(html, hash), `${target}${hash || "#_top"}`);
}

for (const [name, page] of pages) {
  test(`actual generated ${name} navigation retains real guide bookmarks and ordinary entry`, () => {
    const headings = new Set(all(parse(guide)).map(node => attr(node, "id")));
    for (const hash of bookmarks) {
      assert.ok(headings.has(hash.slice(1)), "the destination heading actually exists");
      assert.equal(destination(page, hash), `${target}${hash}`, "incoming section must survive the legacy redirect");
    }
    assert.equal(destination(page, ""), `${target}#_top`);
  });

  test(`actual generated ${name} redirect retains canonical, noindex and no-script continuation`, () => check(page));
}

/** Preserve only the canonical guide's discovery entry. */
function checkSitemap(xml) {
  const locations = new Set([...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map(match => match[1]));
  assert.ok(locations.has(`https://devantler.tech${target}`));
  for (const name of legacyNames) {
    assert.ok(![...locations].some(url => new URL(url).pathname.replace(/\/$/, "") === `/templates/${name}`));
  }
}

test("the sitemap advertises the canonical guide, not the noindex redirects", () => checkSitemap(sitemap));

test("independent broken-output controls fail beside the unchanged real guide", () => {
  for (const page of pages.values()) {
    check(page);
    assert.throws(() => check(undefined), /must be emitted/);
    assert.throws(() => check(page.replace("data-destination=", "data-missing-destination=")));
    assert.throws(() => check(page.replace(/data-fallback="[^"]*"/, 'data-fallback=""')));
    assert.throws(() => check(page.replace('name="robots"', 'name="missing-robots"')));
    assert.throws(() => check(page.replace("<noscript>", "<div>")));
    assert.throws(() => check(page.replace('rel="canonical"', 'rel="alternate"')));
  }
  checkSitemap(sitemap);
  assert.throws(() => checkSitemap(sitemap.replace(`https://devantler.tech${target}`, "https://devantler.tech/missing/")));
  for (const name of legacyNames) {
    assert.throws(() => checkSitemap(sitemap.replace("</urlset>", `<url><loc>https://devantler.tech/templates/${name}/</loc></url></urlset>`)));
  }
});
