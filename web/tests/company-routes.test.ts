import test from "node:test";
import assert from "node:assert/strict";
import { frontendEntries, checkUnseenRoutes } from "../src/public-routes.ts";

test("enabled company identity admits both disclosure pages, never just one locale", () => {
  assert.deepEqual(frontendEntries("true").filter(path => path.endsWith("company/")), ["/company/", "/da/company/"]);
});

test("disabled company identity cannot emit disclosure routes", () => {
  for (const identity of [undefined, "false"]) {
    assert.deepEqual(frontendEntries(identity).filter(path => path.endsWith("company/")), []);
    assert.doesNotThrow(() => checkUnseenRoutes(["/[[locale]]/company"], identity));
  }
  assert.throws(() => checkUnseenRoutes(["/[[locale]]/company"], "true"), /Unexpected/);
});

test("privacy and terms pages follow the company identity release in both locales", () => {
  for (const kind of ["privacy", "terms"]) {
    assert.deepEqual(frontendEntries("true").filter(path => path.endsWith(`/${kind}/`)), [`/${kind}/`, `/da/${kind}/`]);
    for (const identity of [undefined, "false"]) {
      assert.deepEqual(frontendEntries(identity).filter(path => path.endsWith(`/${kind}/`)), []);
      assert.doesNotThrow(() => checkUnseenRoutes([`/[[locale]]/${kind}`], identity));
    }
    assert.throws(() => checkUnseenRoutes([`/[[locale]]/${kind}`], "true"), /Unexpected/);
  }
});
