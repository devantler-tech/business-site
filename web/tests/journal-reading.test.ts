import test from "node:test";
import assert from "node:assert/strict";

async function adapter() {
  let module;
  await assert.doesNotReject(async () => { module = await import("../../src/data/journal-reading.mjs"); }, "the maintained rendered-body bridge must exist");
  return module!;
}
const post = {title:"A real article",description:"A useful description",date:"2026-01-07T00:00:00.000Z",href:"https://devantler.tech/blog/building-ksail-from-shell-to-dotnet-to-go/"};
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

test("each authored article uses its own canonical route and relative destinations", async () => {
  const { extractReading } = await adapter();
  const html = fixture.replaceAll("building-ksail-from-shell-to-dotnet-to-go", "another-article").replace("<strong>authored</strong>", '<a href="../welcome/">authored</a>');
  const reading = extractReading(html, {...post,href:"https://devantler.tech/blog/another-article/"});
  assert.match(reading.html, /href="https:\/\/devantler.tech\/blog\/welcome\/"/);
});

test("a complete prose-only article needs neither headings nor clipboard controls", async () => {
  const { extractReading } = await adapter();
  const html = fixture.replace(/<h2[\s\S]*?<\/script>/, "");
  const reading = extractReading(html, post);
  assert.deepEqual(reading.headings, []);
  assert.match(reading.html, /Complete <strong>authored<\/strong> prose/);
});

test("the renderer's comment-only shell instructions retain their deliberately empty copy payload", async () => {
  const { extractReading } = await adapter();
  const html = fixture.replaceAll("echo hello", "# instructions").replaceAll("echo world", "# continued instructions").replace('data-code="# instructions&#127;# continued instructions"', 'data-code=""');
  assert.match(extractReading(html, post).html, /data-code=""/);
});

test("every native table gets its own named keyboard-scrollable boundary without changing cells", async () => {
  const { extractReading } = await adapter();
  const table = '<table style="min-width:800px"><thead><tr><th>Comparison</th><th>Details</th></tr></thead><tbody><tr><td>VeryLongUnbrokenValue</td><td>Retained content</td></tr></tbody></table>';
  const html = fixture.replace('<p>Complete', `${table}${table}<p>Complete`);
  const reading = extractReading(html, post);
  assert.equal((reading.html.match(/class="reading-table" tabindex="0" role="region" aria-label="Scrollable table"/g) ?? []).length, 2);
  assert.equal((reading.html.match(new RegExp(table, "g")) ?? []).length, 2, "native table/header/cell semantics and content are retained");
});

const diagrams = ['flowchart LR\n A["First"] --> B["Second"]', 'mindmap\n root((Work))\n  Review\n  Deliver'];
const withDiagrams = fixture.replace("<p>Complete", `<pre class="mermaid">${diagrams[0]}</pre><pre class="mermaid">${diagrams[1]}</pre><p>Complete`);

test("real diagrams are bound to authored definitions independently of ordinary copy controls", async () => {
  const { extractReading } = await adapter();
  const reading = extractReading(withDiagrams, post, diagrams);
  assert.deepEqual(reading.diagrams.map((diagram:{source:string}) => diagram.source), diagrams);
  assert.equal(new Set(reading.diagrams.map((diagram:{id:string}) => diagram.id)).size, 2);
  assert.equal((reading.html.match(/class="reading-diagram"/g) ?? []).length, 2);
  assert.equal((reading.html.match(/<summary>Diagram text<\/summary>/g) ?? []).length, 2);
  assert.equal((reading.html.match(/data-code=/g) ?? []).length, 1, "ordinary code keeps its actual copy payload");
});

for (const [name, html, authored] of [
  ["empty diagram", withDiagrams.replace(diagrams[0], ""), ["", diagrams[1]]],
  ["whitespace-only diagram", withDiagrams.replace(diagrams[0], " \n\t"), [" \n\t", diagrams[1]]],
  ["missing second diagram", withDiagrams.replace(`<pre class="mermaid">${diagrams[1]}</pre>`, ""), diagrams],
  ["changed diagram", withDiagrams.replace('B["Second"]', 'B["Wrong"]'), diagrams],
  ["unrecorded diagram", withDiagrams, []],
  ["unexpected interactive diagram child", withDiagrams.replace('A["First"]', '<button data-code="hello">First</button>'), diagrams],
] as const) test(`diagram projection rejects ${name}`, async () => {
  const { extractReading } = await adapter();
  assert.throws(() => extractReading(html, post, authored), /Journal diagram/);
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
