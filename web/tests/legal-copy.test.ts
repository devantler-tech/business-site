import test from "node:test";
import assert from "node:assert/strict";
import { legalPage, type LegalPage } from "../../src/components/business/legal-copy.ts";

const flat = (page: LegalPage) => [page.intro, ...page.sections.flatMap(section => [section.title, ...section.blocks.flatMap(block =>
  typeof block === "string" ? [block] : "list" in block ? [...block.list] : block.links.map(link => link.label))])].join("\n");

test("the portal's privacy text appears only when FEATURE_PORTAL_PRIVACY is on", () => {
  for (const locale of ["en", "da"] as const) {
    const off = flat(legalPage("privacy", locale, false)), on = flat(legalPage("privacy", locale, true));
    for (const phrase of ["portal.devantler.tech", "accounts.devantler.tech", "GitHub", "Amazon Web Services"]) {
      assert.ok(!off.includes(phrase), `${locale} unreleased portal text leaked: ${phrase}`);
      assert.ok(on.includes(phrase), `${locale} released portal text missing: ${phrase}`);
    }
    for (const page of [off, on]) assert.match(page, /Cloudflare/);
    assert.match(on, locale === "da" ? /i 3 år efter/ : /for 3 years after/, `${locale} portal retention names its period`);
  }
});

test("the terms do not depend on the portal flag", () => {
  for (const locale of ["en", "da"] as const) assert.deepEqual(legalPage("terms", locale, true), legalPage("terms", locale, false));
});

test("add-on terms follow the coordinated add-on release", () => {
  for (const locale of ["en", "da"] as const) {
    const heading = locale === "da" ? "Ønsker og tilvalg" : "Feature requests and add-ons";
    const published = legalPage("terms", locale, false);
    const off = legalPage("terms", locale, true, false);
    const on = legalPage("terms", locale, false, true);
    assert.deepEqual(off, published, "portal privacy cannot activate add-on terms");
    assert.ok(!flat(published).includes(heading), `${locale} draft add-on terms leaked`);
    assert.ok(flat(on).includes(heading), `${locale} enabled add-on terms missing`);
    assert.match(flat(on), locale === "da" ? /mål, ikke garantier/ : /targets, not guarantees/);
    assert.deepEqual(on.sections.filter(section => section.title !== heading), published.sections,
      "activation changes only the add-on section, retaining the published hosting terms");
    assert.deepEqual(legalPage("privacy", locale, true, true), legalPage("privacy", locale, true),
      "add-on terms cannot change portal privacy");
  }
});
