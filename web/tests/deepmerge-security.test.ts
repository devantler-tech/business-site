import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";

// Resolve from the real consumer, rather than testing an unrelated direct copy.
const consumer = createRequire(import.meta.resolve("@sveltejs/vite-plugin-svelte"));
const merge = consumer("deepmerge");
const poisoned = JSON.parse('{"__proto__":{"admin":true}}');

test("the real Svelte consumer receives the reviewed source and complete terms", () => {
  const installed = dirname(consumer.resolve("deepmerge"));
  const manifest = consumer("deepmerge/package.json");
  assert.equal(manifest.name, "@devantler-tech/deepmerge-build-patch");
  assert.equal(manifest.version, "4.3.1-devantler.1");
  assert.equal(
    createHash("sha256")
      .update(readFileSync(join(installed, "LICENSE")))
      .digest("hex"),
    "6cfc4687cb2f2d86f4a77e6b526290d3878e5e512f3fec2f4cb36a9cb36f798b",
  );
  for (const file of ["index.cjs", "LICENSE", "NOTICE.md"]) {
    assert.deepEqual(
      readFileSync(join(installed, file)),
      readFileSync(new URL(`../vendor/deepmerge/${file}`, import.meta.url)),
    );
  }
});

function safeObject(value: Record<PropertyKey, unknown>) {
  assert.equal(Object.getPrototypeOf(value), Object.prototype);
  assert.equal(value.admin, undefined);
  for (const key of ["__proto__", "constructor", "prototype"]) {
    assert.equal(Object.hasOwn(value, key), false, `retained forbidden key ${key}`);
  }
  assert.equal(Reflect.get({}, "admin"), undefined);
}

test("a null-prototype target cannot admit a poisoned source prototype", () => {
  safeObject(merge(Object.create(null), poisoned));
});

test("a null placeholder cannot admit a nested poisoned source prototype", () => {
  safeObject(merge({ session: null }, { session: poisoned }).session);
});

test("an own target prototype key cannot poison its copied destination", () => {
  safeObject(merge(poisoned, { ordinary: true }));
});

for (const key of ["__proto__", "constructor", "prototype"]) {
  for (const side of ["source", "target"]) {
    test(`rejects ${key} in the ${side} copy path`, () => {
      const input = JSON.parse(`{"${key}":{"admin":true}}`);
      safeObject(side === "source" ? merge({}, input) : merge(input, {}));
    });
  }
}

test("nested array members retain ordinary data without dangerous keys", () => {
  const result = merge({ members: [{ name: "first" }] }, { members: [poisoned, { name: "last" }] });
  assert.equal(result.members.length, 3);
  assert.equal(result.members[0].name, "first");
  assert.equal(result.members[2].name, "last");
  for (const member of result.members) safeObject(member);
});

test("normal configuration merging preserves symbols, cloning and array replacement", () => {
  const symbol = Symbol("configuration");
  const original = { nested: { first: true }, list: ["original"], [symbol]: "kept" };
  const addition = { nested: { second: true }, list: ["replacement"] };
  const result = merge(original, addition, { arrayMerge: (_target: unknown[], source: unknown[]) => source });
  assert.deepEqual(result.nested, { first: true, second: true });
  assert.deepEqual(result.list, ["replacement"]);
  assert.equal(result[symbol], "kept");
  assert.notEqual(result.nested, original.nested);
  assert.deepEqual(original.nested, { first: true });
  assert.deepEqual(merge.all([{ first: true }, { second: true }]), { first: true, second: true });
});

test("custom merging and clone:false preserve their existing API behavior", () => {
  const shared = { ordinary: true };
  assert.equal(merge({}, { shared }, { clone: false }).shared, shared);
  const result = merge(
    { nested: { count: 1 } },
    { nested: { count: 2 } },
    {
      customMerge: (key: string) =>
        key === "nested"
          ? (left: { count: number }, right: { count: number }) => ({ count: left.count + right.count })
          : undefined,
    },
  );
  assert.deepEqual(result, { nested: { count: 3 } });
});
