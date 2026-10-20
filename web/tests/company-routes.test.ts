import test from "node:test";
import assert from "node:assert/strict";
import { frontendEntries, checkUnseenRoutes } from "../src/release-gate.ts";

test("enabled company identity admits both disclosure pages, never just one locale", () => {
  assert.deepEqual(frontendEntries("true", "true").filter(path => path.endsWith("company/")), ["/company/", "/da/company/"]);
});

test("disabled company identity cannot emit disclosure routes", () => {
  for (const identity of [undefined, "false"]) {
    assert.deepEqual(frontendEntries("true", identity).filter(path => path.endsWith("company/")), []);
    assert.doesNotThrow(() => checkUnseenRoutes("true", ["/[[locale]]/company"], identity));
  }
  assert.deepEqual(frontendEntries("false", "true"), []);
  assert.doesNotThrow(() => checkUnseenRoutes("false", ["/[[locale]]/company"], "true"));
});
