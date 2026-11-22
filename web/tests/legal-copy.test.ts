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

test("the terms describe the portal agreement and follow-up in both languages", () => {
  const cases = [
    { locale: "en", title: "Proposal and agreement", initial: /by email or phone.*invite you to the client portal/s,
      acceptance: /review and accept.*proposal/, order: /place.*your order.*after accepting the proposal/,
      confirmation: /confirm.*by email before work starts/, followUp: /progress and submit further requests in the portal/ },
    { locale: "da", title: "Tilbud og aftale", initial: /på e-mail eller telefon.*inviterer jeg dig til kundeportalen/s,
      acceptance: /læse og acceptere.*tilbud/, order: /afgiver din bestilling.*efter at have accepteret tilbuddet/,
      confirmation: /bekræfter.*på e-mail, inden arbejdet begynder/, followUp: /følge med i dit projekt og sende nye forespørgsler i portalen/ },
  ] as const;
  for (const item of cases) {
    const agreement = legalPage("terms", item.locale, true).sections.find(section => section.title === item.title);
    assert.ok(agreement);
    const [initial, proposal, order, followUp] = agreement.blocks.map(block => typeof block === "string" ? block : "");
    assert.match(initial, item.initial);
    assert.match(proposal, item.acceptance);
    assert.match(order, item.order);
    assert.match(order, item.confirmation);
    assert.match(followUp, item.followUp);
    assert.doesNotMatch(followUp, /mail|phone|telefon/);
  }
});

test("the privacy notice explains how to load and hide the optional review collector", () => {
  for (const locale of ["en", "da"] as const) {
    const notice = flat(legalPage("privacy", locale, true));
    assert.match(notice, /Trustpilot/);
    assert.match(notice, /IP-adresse|IP address/);
    assert.match(notice, locale === "da" ? /Skjul Trustpilot/ : /Hide Trustpilot/);
  }
});
