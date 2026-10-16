import test from "node:test";
import assert from "node:assert/strict";

async function adapter() {
  let module;
  await assert.doesNotReject(async () => { module = await import("../../src/data/journal-reading.mjs"); }, "the maintained rendered-body bridge must exist");
  return module!;
}
const post = {title:"A real article",description:"A useful description",date:"2026-01-07T00:00:00.000Z"};
const fixture = `<html><head><title>A real article | Devantler Tech</title><meta name="description" content="A useful description"><link rel="canonical" href="https://devantler.tech/blog/building-ksail-from-shell-to-dotnet-to-go/"></head><body><main><h1>A real article</h1><div class="sl-markdown-content"><p>Complete <strong>authored</strong> prose.</p><h2 id="the-problem">The problem</h2><div class="expressive-code"><pre><code><div class="ec-line">echo hello</div><div class="ec-line">echo world</div></code></pre><div class="copy"><button data-code="echo hello&#127;echo world" data-copied="Copied!" title="Copy to clipboard"></button><div aria-live="polite"></div></div></div><script type="module" src="/_astro/ec.fixture.js"></script></div><time datetime="2026-01-07T00:00:00.000Z">Jan 7, 2026</time><time datetime="2026-10-09T00:09:33.000Z">Oct 9, 2026</time><a rel="prev" href="/blog/newer/">A newer article</a><a rel="next" href="/blog/older/">An older article</a></main></body></html>`;

test("the reading bridge preserves actual prose, bookmarks, dates and title-labelled neighbours", async () => {
  const { extractReading } = await adapter();
  const reading = extractReading(fixture, post);
  assert.match(reading.html, /Complete <strong>authored<\/strong> prose/);
  assert.deepEqual(reading.headings, [{id:"the-problem",title:"The problem",level:2}]);
  assert.equal(reading.modified, "2026-10-09T00:09:33.000Z");
  assert.deepEqual(reading.adjacent, [{rel:"prev",href:"https://devantler.tech/blog/newer/",title:"A newer article"},{rel:"next",href:"https://devantler.tech/blog/older/",title:"An older article"}]);
  assert.ok(!reading.html.includes("<script"), "the body cannot bring the old framework runtime into the candidate");
});

for (const [name, mutate] of [
  ["missing body", (v:string) => v.replace("sl-markdown-content", "missing-body")],
  ["wrong article", (v:string) => v.replace("A real article |", "Another article |")],
  ["missing bookmark", (v:string) => v.replace('id="the-problem"', '')],
  ["duplicate bookmark", (v:string) => v.replace("</h2>", '</h2><h3 id="the-problem">Duplicate</h3>')],
  ["missing copy payload", (v:string) => v.replace('data-code="echo hello&#127;echo world"', '')],
  ["empty copy payload", (v:string) => v.replace("echo hello&#127;echo world", "")],
  ["unreviewed script", (v:string) => v.replace("/_astro/ec.fixture.js", "/unreviewed.js")],
  ["missing committed date", (v:string) => v.replace('datetime="2026-10-09T00:09:33.000Z"', '')],
] as const) test(`incomplete or unsupported reading input fails closed: ${name}`, async () => {
  const { extractReading } = await adapter();
  assert.throws(() => extractReading(mutate(fixture), post), /Journal/);
});
