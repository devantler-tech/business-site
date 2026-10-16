import test from "node:test";
import assert from "node:assert/strict";
import { parseFrontendRelease, frontendEntries, checkUnseenRoutes } from "../src/release-gate.ts";

test("the candidate stays off unless explicitly enabled", () => {
  for (const value of [undefined, "false"]) {
    assert.equal(parseFrontendRelease(value), false);
    assert.deepEqual(frontendEntries(value), []);
  }
  assert.equal(parseFrontendRelease("true"), true);
  assert.deepEqual(frontendEntries("true"), ["/", "/da/", "/about/", "/da/about/"]);
});

test("only the deliberately disabled candidate may be absent from the artifact", () => {
  for (const value of [undefined, "false"]) {
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]/about"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]", "/[[locale]]/about"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]"]));
    assert.throws(() => checkUnseenRoutes(value, ["/unreviewed"]), /Unexpected/);
    assert.throws(() => checkUnseenRoutes(value, ["/[[locale]]/about", "/unreviewed"]), /Unexpected/);
  }
  assert.throws(() => checkUnseenRoutes("true", ["/[[locale]]/about"]), /Unexpected/);
  assert.throws(() => checkUnseenRoutes("true", ["/[[locale]]"]), /Unexpected/);
  assert.throws(() => checkUnseenRoutes("", ["/[[locale]]/about"]), /true or false/);
});

test("ambiguous release values fail closed instead of enabling output", () => {
  for (const value of ["", "1", "TRUE", " true", "false "]) {
    assert.throws(() => parseFrontendRelease(value), /true or false/);
    assert.throws(() => frontendEntries(value), /true or false/);
  }
});
