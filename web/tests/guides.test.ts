import test from "node:test";
import assert from "node:assert/strict";
import { frontendEntries } from "../src/public-routes.ts";

// Losing any established guide route must fail independently of healthy Journal routes.
test("enabled visitors can finish every existing technical guide locally", () => {
  const entries = frontendEntries("true");
  for (const path of ["/agentic-engineering/", "/templates/", "/templates/go/", "/templates/dotnet/", "/templates/platform-template/", "/templates/application-template/"]) {
    assert.ok(entries.includes(path as `/${string}`), `missing ${path}`);
  }
  assert.ok(!entries.some(path => path.startsWith("/da/templates/") || path === "/da/agentic-engineering/"), "do not invent translated guides");
});

test("the old GitOps and platform tenant bookmarks remain reachable in every public build", () => {
  for (const legacy of ["/templates/gitops-tenant-template/", "/templates/platform-tenant-template/"] as const) {
    assert.ok(frontendEntries("true").includes(legacy), `missing legacy template redirect ${legacy}`);
    assert.ok(frontendEntries("false").includes(legacy));
    assert.ok(frontendEntries().includes(legacy));
  }
});

test("technical bodies preserve their committed date without inventing Journal metadata", async () => {
  const { extractReading } = await import("../../src/data/journal-reading.mjs");
  const html = '<title>Guide | Devantler Tech</title><meta name="description" content="Instructions"><link rel="canonical" href="https://devantler.tech/templates/go/"><div class="sl-markdown-content"><p>Complete instructions.</p><h2 id="start">Start</h2><a href="../dotnet/">Other guide</a></div><time datetime="2026-10-08T22:15:01.000Z"></time>';
  const guide = extractReading(html, {title:"Guide",description:"Instructions",href:"https://devantler.tech/templates/go/"}, [], "guide");
  assert.equal(guide.modified, "2026-10-08T22:15:01.000Z");
  assert.deepEqual(guide.headings, [{id:"start",title:"Start",level:2}]);
  assert.deepEqual(guide.adjacent, []);
  const adjacent = extractReading(html + '<a rel="next" href="/templates/dotnet/">.NET guide</a>', {title:"Guide",description:"Instructions",href:"https://devantler.tech/templates/go/"}, [], "guide");
  assert.deepEqual(adjacent.adjacent, [{rel:"next",href:"https://devantler.tech/templates/dotnet/",title:".NET guide"}]);
  assert.match(guide.html, /https:\/\/devantler.tech\/templates\/dotnet\//);
  assert.throws(() => extractReading(html.replace('id="start"', ''), {title:"Guide",description:"Instructions",href:"https://devantler.tech/templates/go/"}, [], "guide"), /bookmark/);
});
