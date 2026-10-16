import { readFile, readdir } from "node:fs/promises";
import assert from "node:assert/strict";

const enabled = process.argv[2] === "enabled";
assert.ok(enabled || process.argv[2] === "disabled", "declare enabled or disabled output");
const root = new URL("../build/", import.meta.url);
const pages = [
  ["index.html", "en", "I build websites"],
  ["da/index.html", "da", "Jeg bygger hjemmesider"],
  ["about/index.html", "en", "About Devantler Tech"],
  ["da/about/index.html", "da", "Om Devantler Tech"],
];
for (const [path, locale, title] of pages) {
  if (!enabled) {
    await assert.rejects(readFile(new URL(path, root)), { code: "ENOENT" });
    continue;
  }
  let html = "";
  await assert.doesNotReject(async () => { html = await readFile(new URL(path, root), "utf8"); }, `missing enabled candidate route: ${path}`);
  assert.ok(html.includes(`<html lang="${locale}">`));
  assert.ok(html.includes(`<h1`) && html.includes(title));
  assert.ok(html.includes("Nikolai Emil Damm"));
  const isHome = path === "index.html" || path === "da/index.html";
  const suffix = isHome ? "" : "about/";
  assert.ok(html.includes(`rel="canonical" href="https://devantler.tech/${locale === "da" ? "da/" : ""}${suffix}"`));
  for (const language of ["en", "da", "x-default"]) assert.ok(html.includes(`hreflang="${language}"`));
  assert.ok(html.includes('href="#main"') && html.includes('id="main"'));
  assert.ok(html.includes('id="theme-select"'));
  assert.ok(!html.includes('href="https://devantler.tech/blog/"'), "migrated Journal archive actions stay local");
  if (!isHome) assert.ok(html.includes("/pdfs/nikolai-emil-damm-cv.pdf"));
  if (isHome) {
    for (const section of ["hero-title", "quality-title", "services", "work", "process", "contact"]) {
      assert.ok(html.includes(`id="${section}"`), `missing homepage destination ${section}`);
    }
    assert.ok(html.indexOf('id="quality-title"') < html.indexOf('id="services"'), "quality precedes prices");
    for (const [id, setup, monthly] of [["website", 2995, 99], ["app", 7995, 299], ["service", 4995, 199]]) {
      assert.match(html, new RegExp(`data-offer="${id}"[^>]*data-setup="${setup}"[^>]*data-monthly="${monthly}"`));
    }
    assert.ok(html.includes('href="https://ascoachingogvaner.dk/"'));
    assert.ok(html.includes(`href="/${locale === "da" ? "da/" : ""}projects/"`));
    assert.ok(html.includes(`href="/${locale === "da" ? "da/" : ""}about/"`));
    assert.ok(html.includes('href="/" lang="en"') && html.includes('href="/da/" lang="da"'));
    assert.ok(!/Family project|family member|Familieprojekt|familiemedlem|Wedding App|github\.com/.test(html));
    assert.ok(!/mailto:|CVR|46830385/.test(html), "candidate must not activate company disclosure");
    assert.ok(html.includes('href="https://www.linkedin.com/in/nikolai-emil-damm-14a786150/"'));
    assert.ok(html.includes("<picture") && html.includes('type="image/avif"') && html.includes('type="image/webp"'));
  }
  const scripts = [...html.matchAll(/<script\b[^>]*src="([^"]+)"[^>]*>/g)];
  assert.equal(scripts.length, 1, "only the existing local theme controller, no application hydration");
  assert.match(scripts[0][1], /^\/_app\/immutable\/assets\/theme\.[A-Za-z0-9_-]+\.js$/);
  await readFile(new URL(scripts[0][1].slice(1), root));
  assert.ok(!html.includes('type="module"') && !html.includes("sveltekit.start"));
  assert.ok(!html.includes("data:text/javascript") && !/<script[^>]*src="https?:/.test(html));
}
/** @param {URL} dir @returns {Promise<URL[]>} */
async function walk(dir) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const url = new URL(entry.name + (entry.isDirectory() ? "/" : ""), dir);
    if (entry.isDirectory()) files.push(...(await walk(url)));
    else files.push(url);
  }
  return files;
}
for (const file of await walk(root)) {
  assert.ok(!file.pathname.includes("/portal/"), "private routes cannot enter this static artifact");
  if (file.pathname.endsWith(".html")) {
    const html = await readFile(file, "utf8");
    assert.ok(!/fixture-only|PORTAL_|portal_identity_test|COMPANY_POSTAL_ADDRESS/.test(html));
  }
}
console.log(`Svelte frontend output: ${enabled ? "enabled bilingual routes" : "no candidate routes"} verified`);
