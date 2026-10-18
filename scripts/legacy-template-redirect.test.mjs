import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import test from "node:test";
import { parse } from "parse5";

const root = new URL("../dist/", import.meta.url);
const target = "/templates/platform-tenant-template/";
const bookmarks = ["#getting-started", "#what-the-template-owns-vs-what-you-own"];
const page = readFileSync(new URL("templates/gitops-tenant-template/index.html", root), "utf8");
const guide = readFileSync(new URL("templates/platform-tenant-template/index.html", root), "utf8");
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
const attr = (node, name) => node?.attrs?.find(attribute => attribute.name === name)?.value;

/** Execute the actual emitted controller, or the old static refresh when no controller exists. */
function destination(html, hash) {
  const nodes = all(parse(html));
  const script = nodes.find(node => node.tagName === "script");
  if (!script) {
    const refresh = nodes.find(node => node.tagName === "meta" && attr(node, "http-equiv") === "refresh");
    return attr(refresh, "content")?.split(/url=/i)[1];
  }
  assert.equal(attr(script, "src"), undefined, "the redirect needs no external controller");
  let result;
  runInNewContext((script.childNodes ?? []).map(node => node.value ?? "").join(""), {
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
  assert.match(html, /<noscript><meta http-equiv="refresh" content="0;url=\/templates\/platform-tenant-template\/#_top"/);
  for (const hash of ["", ...bookmarks]) assert.equal(destination(html, hash), `${target}${hash || "#_top"}`);
}

test("actual generated navigation retains real guide bookmarks and ordinary entry", () => {
  const headings = new Set(all(parse(guide)).map(node => attr(node, "id")));
  for (const hash of bookmarks) {
    assert.ok(headings.has(hash.slice(1)), "the destination heading actually exists");
    assert.equal(destination(page, hash), `${target}${hash}`, "incoming section must survive the legacy redirect");
  }
  assert.equal(destination(page, ""), `${target}#_top`);
});

test("actual generated redirect retains canonical, noindex and no-script continuation", () => check(page));

test("independent broken-output controls fail beside the unchanged real guide", () => {
  check(page);
  assert.throws(() => check(undefined), /must be emitted/);
  assert.throws(() => check(page.replace("location.hash", "''")));
  assert.throws(() => check(page.replace('name="robots"', 'name="missing-robots"')));
  assert.throws(() => check(page.replace("<noscript>", "<div>")));
  assert.throws(() => check(page.replace('rel="canonical"', 'rel="alternate"')));
});
