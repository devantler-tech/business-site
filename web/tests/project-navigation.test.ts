import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const reveal = readFileSync(new URL("../../src/components/business/project-reveal.js", import.meta.url), "utf8");
const redirect = readFileSync(new URL("../src/lib/project-redirect.js", import.meta.url), "utf8");

test("the actual bookmark controller opens research on initial and changed hashes", () => {
  const details = { open: false };
  let scrolled = 0;
  let changed = () => {};
  const location = { hash: "#research%20detail" };
  runInNewContext(reveal, {
    location,
    document: { getElementById: (id: string) => id === "research detail" ? { closest: () => details, scrollIntoView: () => { scrolled++; } } : null },
    window: { addEventListener: (name: string, action: () => void) => { assert.equal(name, "hashchange"); changed = action; } },
  });
  assert.equal(details.open, true);
  assert.equal(scrolled, 1);
  details.open = false;
  location.hash = "#missing";
  changed();
  assert.equal(details.open, false);
  location.hash = "#%ZZ";
  assert.doesNotThrow(changed);
  location.hash = "#research%20detail";
  changed();
  assert.equal(details.open, true);
  assert.equal(scrolled, 2);
});

test("the actual legacy redirect retains incoming fragments and uses localized-page-independent fallbacks", () => {
  for (const fallback of ["#open-title", "#research"]) {
    for (const hash of ["", "#-actions-", "#%EF%B8%8F-platform---"]) {
      let destination = "";
      runInNewContext(redirect, {
        document: { currentScript: { dataset: { destination: "/projects/", fallback } } },
        location: { hash, replace: (value: string) => { destination = value; } },
      });
      assert.equal(destination, "/projects/" + (hash || fallback));
    }
  }
});
