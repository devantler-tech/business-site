import assert from "node:assert/strict";
import { readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { readReadingReference } from "../../src/data/journal-reading.mjs";

const file = new URL("../../.astro/journal-reading.json", import.meta.url);
const original = readFileSync(file, "utf8");
const valid = JSON.parse(original);
assert.ok(valid.assets.length > 1, "partial asset failure needs a healthy asset beside the rejected one");
try {
  readReadingReference();
  unlinkSync(file);
  assert.throws(() => readReadingReference(), /ENOENT/);
  writeFileSync(file, original.slice(0, -1));
  assert.throws(() => readReadingReference(), SyntaxError);
  /** @type {[string,(value:ReturnType<typeof readReadingReference>)=>void][]} */
  const mutations = [
    ["stale source", value => { value.fingerprint = "stale"; }],
    ["partial body", value => { value.html = "<p>partial output</p>"; }],
    ["missing body", value => { delete value.html; }],
    ["missing asset inventory", value => { value.assets = []; }],
    ["second asset corrupted", value => { value.assets[1].source = "broken"; }],
  ];
  for (const [name, mutate] of mutations) {
    const value = structuredClone(valid);
    mutate(value);
    writeFileSync(file, JSON.stringify(value));
    assert.throws(() => readReadingReference(), name === "stale source" ? /run bash scripts\/build-journal-reference\.sh/ : /Journal/, `reject ${name}`);
  }
} finally { writeFileSync(file, original); }
readReadingReference();
console.log("Journal reference: missing, truncated, stale, partial body and partial asset observations rejected; healthy reference retained");
