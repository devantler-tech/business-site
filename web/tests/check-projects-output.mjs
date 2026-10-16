import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import catalogue from "../../src/data/public-products.json" with { type: "json" };
import { publicStarSnapshot, selectPublicTools } from "../../src/data/public-products.ts";

const enabled = process.argv[2] === "enabled";
assert.ok(enabled || process.argv[2] === "disabled");
const root = new URL("../build/", import.meta.url);
const expected = selectPublicTools(catalogue, publicStarSnapshot);
for (const locale of ["en", "da"]) {
  const prefix = locale === "da" ? "da/" : "";
  const path = `${prefix}projects/index.html`;
  if (!enabled) {
    await assert.rejects(readFile(new URL(path, root)), { code: "ENOENT" });
    continue;
  }
  let html = "";
  await assert.doesNotReject(async () => { html = await readFile(new URL(path, root), "utf8"); }, `missing enabled Projects route: ${path}`);
  const cards = [...html.matchAll(/data-public-product="([^"]+)" data-stars="(\d+)"/g)];
  assert.equal(cards.length, 6);
  assert.deepEqual(cards.map(([, name, stars]) => [name, Number(stars)]), expected.map(({ repository, stars }) => [repository, stars]));
  assert.ok(html.includes(`datetime="${publicStarSnapshot.fetchedAt}"`), "show the actual complete snapshot timestamp");
  assert.ok(html.includes(`rel="canonical" href="https://devantler.tech/${prefix}projects/"`));
  assert.ok(html.includes('href="/projects/" lang="en"') && html.includes('href="/da/projects/" lang="da"'));
  for (const id of ["main", "technical-title", "technical-projects", "more-public-products", "open-title", "family-title", "-self-hosted-personal-apps", "️-platform---", "research", "-data-product-", "-exploration-of-state-of-the-art-technology-architectures-and-tools-to-create-future-proof-data-spaces", "-reusable-workflows-", "-actions-", "️-world-at-ruin--", "-unifi-provider---"]) {
    assert.ok(html.includes(`id="${id}"`), `missing retained destination ${id}`);
  }
  assert.ok(html.includes('data-ksail-workflow') && html.includes('data-ksail-detail'));
  assert.ok(html.includes('PolyForm Shield'));
  assert.ok(html.includes('href="https://github.com/orgs/devantler-tech/repositories"'));
  assert.ok(html.includes('href="https://ascoachingogvaner.dk/"'));
  assert.ok(html.includes('href="https://devantler.tech/pdfs/thesis.pdf"'));
  assert.ok(/<details\b[^>]*>[\s\S]*?<summary\b[^>]*>[\s\S]*?id="-data-product-"/.test(html), "research remains in a native disclosure");
  assert.ok(!/Family project|family member|Familieprojekt|familieprojekter|Wedding App|github\.com\/[^"]+\/blob\//.test(html));
  assert.ok(!/mailto:|CVR|46830385|sveltekit.start|type="module"/.test(html));
  const scripts = [...html.matchAll(/<script\b[^>]*src="([^"]+)"/g)].map(([, src]) => src);
  assert.equal(scripts.length, 2, "theme plus the existing bookmark disclosure controller only");
  for (const src of scripts) {
    assert.match(src, /^\/_app\/immutable\/assets\/(theme|project-reveal)\.[A-Za-z0-9_-]+\.js$/);
    await readFile(new URL(src.slice(1), root));
  }
}
for (const section of ["active", "completed"]) {
  const file = new URL(`projects/${section}/index.html`, root);
  if (!enabled) { await assert.rejects(readFile(file), { code: "ENOENT" }); continue; }
  const html = await readFile(file, "utf8");
  const anchor = section === "active" ? "open-title" : "research";
  assert.ok(html.includes(`href="/projects/#${anchor}"`));
  assert.ok(html.includes(`data-fallback="#${anchor}"`) && html.includes('data-destination="/projects/"'));
}
console.log(`Svelte Projects output: ${enabled ? "six tools, bilingual visitor paths and legacy bookmarks" : "all new routes absent"} verified`);
