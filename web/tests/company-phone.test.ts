import test from "node:test";
import assert from "node:assert/strict";
import { traderLine } from "../../src/components/business/legal-copy.ts";
import { telHref } from "../../src/components/business/company-copy.ts";

const facts = { cvr: "46830385", owner: "Nikolai Emil Damm", email: "ned@devantler.tech" };

test("the terms name a phone number only once one is set", () => {
  for (const phone of [undefined, "", "  "]) {
    assert.doesNotMatch(traderLine("en", { ...facts, phone }), /Phone/);
    assert.doesNotMatch(traderLine("da", { ...facts, phone }), /Telefon/);
  }
  assert.match(traderLine("en", { ...facts, phone: "+45 12 34 56 78" }), /Email: ned@devantler\.tech\. Phone: \+45 12 34 56 78\.$/);
  assert.match(traderLine("da", { ...facts, phone: "+45 12 34 56 78" }), /E-mail: ned@devantler\.tech\. Telefon: \+45 12 34 56 78\.$/);
});

test("the phone link dials the number without spacing", () => {
  assert.equal(telHref("+45 12 34 56 78"), "tel:+4512345678");
  assert.equal(telHref("12-34 56.78"), "tel:12345678");
});
