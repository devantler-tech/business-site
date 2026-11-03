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
  assert.ok(cards.length >= 3 && cards.length <= 6, "a curated showcase of up to six tools, without filler");
  assert.deepEqual(cards.map(([, name, stars]) => [name, Number(stars)]), expected.map(({ repository, stars }) => [repository, stars]));
  assert.ok(html.includes(`datetime="${publicStarSnapshot.fetchedAt}"`), "keep the actual complete snapshot timestamp machine-readable");
  assert.ok(html.includes(`rel="canonical" href="https://devantler.tech/${prefix}projects/"`));
  assert.ok(html.includes('href="/projects/" lang="en"') && html.includes('href="/da/projects/" lang="da"'));
  for (const id of ["main", "technical-title", "technical-projects", "more-public-products", "open-title", "family-title", "-self-hosted-personal-apps", "️-platform---", "research", "-data-product-", "-exploration-of-state-of-the-art-technology-architectures-and-tools-to-create-future-proof-data-spaces", "-reusable-workflows-", "-actions-", "️-world-at-ruin--", "-unifi-provider---"]) {
    assert.ok(html.includes(`id="${id}"`), `missing retained destination ${id}`);
  }
  assert.ok(html.includes('data-ksail-workflow') && html.includes('data-ksail-detail'));
  assert.ok(html.includes('PolyForm Shield'));
  assert.ok(html.includes('href="https://github.com/orgs/devantler-tech/repositories"'));
  assert.ok(html.includes('href="https://ascoachingogvaner.dk/"'));
  const hosting = html.match(/<article\b[^>]*data-hosting-project[^>]*>([\s\S]*?)<\/article>/)?.[1];
  assert.ok(hosting?.includes(`href="/${prefix}#services"`), "the hosting card leads to the actual hosting offer");
  const checked = html.match(/<time datetime="([^"]+)">([^<]+)<\/time>/);
  assert.ok(checked && checked[1] === publicStarSnapshot.fetchedAt, "the ranking keeps its exact machine-readable observation");
  assert.ok(!/UTC|\d{2}:\d{2}/.test(checked[2]), "the visible ranking check is a quiet date, not an operational timestamp");
  assert.ok(html.includes('href="/pdfs/thesis.pdf"'));
  assert.ok(/<details\b[^>]*>[\s\S]*?<summary\b[^>]*>[\s\S]*?id="-data-product-"/.test(html), "research remains in a native disclosure");
  assert.ok(!/href="#research"|<section\b[^>]*aria-labelledby="research-title"/.test(html), "Research is not promoted in the section index or as a feature");
  const archive = html.match(/<details\b([^>]*\bid="research"[^>]*)>([\s\S]*?)<\/details>/);
  assert.ok(archive, "Historical bookmarks retain a native disclosure");
  assert.ok(!/\bopen(?:\s|=|$)/.test(archive[1]), "Historical references start closed");
  assert.ok(archive[2].includes(locale === "da" ? "Tidligere projektlinks" : "Earlier project links"));
  assert.ok(!/Family project|family member|Familieprojekt|familieprojekter|Wedding App|github\.com\/[^"]+\/blob\//.test(html));
  assert.equal(/mailto:|CVR|46830385/.test(html),process.env.FEATURE_COMPANY_IDENTITY === "true");
  assert.ok(!/sveltekit.start|type="module"/.test(html));
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
console.log(`Svelte Projects output: ${enabled ? "curated tools, bilingual visitor paths and legacy bookmarks" : "all new routes absent"} verified`);
