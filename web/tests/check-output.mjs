import { readFile, readdir } from "node:fs/promises";
import assert from "node:assert/strict";
import { parse } from "parse5";

/**
 * @param {import("parse5").DefaultTreeAdapterTypes.Node} node
 * @returns {import("parse5").DefaultTreeAdapterTypes.Element[]}
 */
function scriptsIn(node) {
  if ("tagName" in node && node.tagName === "script") return [node];
  return "childNodes" in node ? node.childNodes.flatMap(scriptsIn) : [];
}

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
    assert.doesNotMatch(html, /earlier research|tidligere forskning/i, "Home introduces selected professional work");
    for (const section of ["hero-title", "quality-title", "services", "work", "process", "contact"]) {
      assert.ok(html.includes(`id="${section}"`), `missing homepage destination ${section}`);
    }
    assert.ok(html.indexOf('id="quality-title"') < html.indexOf('id="services"'), "quality precedes prices");
    for (const [id, setup, monthly] of [["website",6000,300],["app",15000,800],["service",9500,500]]) {
      assert.match(html, new RegExp(`data-offer="${id}"[^>]*data-setup="${setup}"[^>]*data-monthly="${monthly}"`));
    }
    const services = html.match(/<section\b[^>]*id="services"[^>]*>([\s\S]*?)<\/section>/)?.[1] ?? "";
    const location = services.match(/<p\b[^>]*data-hosting-location[^>]*>([\s\S]*?)<\/p>/)?.[1];
    assert.ok(location, "the verified hosting location sits with the hosting offer");
    assert.match(location, locale === "da" ? /i EU på servere hos Hetzner/ : /in the EU, on Hetzner servers/);
    assert.match(location, locale === "da" ? /andre leverandører og placeringer/ : /other providers and locations/, "connected services are not claimed to stay in the EU");
    assert.doesNotMatch(location, /Denmark|Danmark|GDPR|certif|complian|all (?:customer )?data|alle data/i, "no residency, compliance or certification claim");
    assert.ok(html.includes('href="https://ascoachingogvaner.dk/"'));
    assert.ok(html.includes(`href="/${locale === "da" ? "da/" : ""}projects/"`));
    assert.ok(html.includes(`href="/${locale === "da" ? "da/" : ""}about/"`));
    assert.ok(html.includes('href="/" lang="en"') && html.includes('href="/da/" lang="da"'));
    assert.ok(!/Family project|family member|Familieprojekt|familiemedlem|Wedding App|github\.com/.test(html));
    assert.equal(/mailto:|CVR|46830385/.test(html),process.env.FEATURE_COMPANY_IDENTITY === "true", "company disclosure follows the declared build profile");
    assert.ok(html.includes('href="https://www.linkedin.com/in/nikolai-emil-damm-14a786150/"'));
    assert.ok(html.includes("<picture") && html.includes('type="image/avif"') && html.includes('type="image/webp"'));
  }
  const scripts = scriptsIn(parse(html));
  const expected = [/^\/_app\/immutable\/assets\/theme\.[A-Za-z0-9_-]+\.js$/];
  assert.equal(scripts.length, expected.length, "only the local controllers for this route");
  for (const [index, script] of scripts.entries()) {
    const src = script.attrs.find(attribute => attribute.name === "src")?.value;
    assert.ok(src, "every script has a local asset");
    assert.match(src, expected[index]);
    const type = script.attrs.find(attribute => attribute.name === "type")?.value;
    assert.equal(type, index === 1 ? "module" : undefined, "only the collector controller is a module");
    await readFile(new URL(src.slice(1), root));
  }
  assert.doesNotMatch(html, /sveltekit\.start|data:text\/javascript/, "no application hydration or inline program URLs");
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
