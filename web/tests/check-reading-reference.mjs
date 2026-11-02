import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { readReadingReference } from "../../src/data/journal-reading.mjs";

const file = new URL("../../.generated/journal-reading.json", import.meta.url);
const original = readFileSync(file, "utf8");
const valid = JSON.parse(original);
/** @typedef {NonNullable<typeof import("virtual:journal-reading").default>[number]} Reading */
/** @param {ReturnType<typeof readReadingReference>} value @returns {Reading} */
const diagramReading = value => {
  const reading = value.readings.find((/** @type {Reading} */ reading) => reading.diagrams.length);
  assert.ok(reading, "the real reference must include a diagram article");
  return reading;
};
assert.ok(valid.assets.length > 1, "partial asset failure needs a healthy asset beside the rejected one");
/**
 * Recompute the checksum so a mutation reaches the check it is named after,
 * instead of every one stopping at the checksum comparison.
 * @param {Record<string, unknown>} value
 */
const resign = value => {
  const {checksum, ...content} = value;
  value.checksum = createHash("sha256").update(JSON.stringify(content)).digest("hex");
};
try {
  readReadingReference();
  unlinkSync(file);
  assert.throws(() => readReadingReference(), /ENOENT/);
  writeFileSync(file, original.slice(0, -1));
  assert.throws(() => readReadingReference(), SyntaxError);
  /** @type {[string,(value:ReturnType<typeof readReadingReference>)=>void,RegExp,boolean][]} */
  const mutations = [
    ["stale source", value => { value.fingerprint = "stale"; }, /run bash scripts\/build-journal-reference\.sh/, true],
    ["altered body without a matching checksum", value => { value.readings[0].html = "<p>partial output</p>"; }, /content is incomplete or altered/, false],
    ["empty body", value => { value.readings[0].html = " "; }, /complete reading body required/, true],
    ["missing body", value => { delete value.readings[0].html; }, /complete reading body required/, true],
    ["missing article beside healthy articles", value => { value.readings.splice(1, 1); }, /complete reading inventory required/, true],
    ["repeated article", value => { value.readings[1] = value.readings[0]; }, /complete reading inventory required/, true],
    ["missing guide beside healthy articles and guides", value => { value.guides.splice(1, 1); }, /Guide complete reading inventory required/, true],
    ["empty later guide beside a healthy guide", value => { value.guides[1].html = ""; }, /Guide complete reading body required/, true],
    ["missing technical diagram beside healthy Journal diagrams", value => { value.guides[0].html = value.guides[0].html.replace(/<figure class="reading-diagram"[\s\S]*?<\/figure>/, ""); }, /diagram body is incomplete/, true],
    ["missing guide stylesheet beside healthy images", value => { const stylesheet = value.guides.find((/** @type {Reading} */ guide) => guide.path === "/templates/go/").html.match(/href="(\/_app\/immutable\/assets\/reading-[^"]+\.css)"/)[1]; value.assets = value.assets.filter((/** @type {{filename:string}} */ asset) => `/${asset.filename}` !== stylesheet); }, /referenced asset is missing/, true],
    ["empty later article beside a healthy article", value => { value.readings[1].html = ""; }, /complete reading body required/, true],
    ["missing asset inventory", value => { value.assets = []; }, /asset inventory is missing/, true],
    ["second asset corrupted", value => { value.assets[1].source = "broken"; }, /asset digest mismatch/, true],
    ["missing referenced asset beside healthy assets", value => { value.assets.splice(1, 1); }, /referenced asset is missing/, true],
    ["missing diagram beside a healthy one", value => { const reading = diagramReading(value); reading.html = reading.html.replace(/<figure class="reading-diagram"[\s\S]*?<\/figure>/, ""); }, /diagram body is incomplete/, true],
    ["changed diagram source", value => { diagramReading(value).diagrams[1].source = "flowchart LR\nWrong-->Result"; }, /diagram definitions are incomplete/, true],
    ["missing diagram entry", value => { delete diagramReading(value).diagramScript; }, /diagram entry is missing/, true],
    ["missing transitive diagram module", value => { const module = value.assets.find((/** @type {{dependencies?:string[]}} */ asset) => asset.dependencies?.length).dependencies[0]; value.assets = value.assets.filter((/** @type {{filename:string}} */ asset) => asset.filename !== module); }, /diagram referenced module is missing/, true],
  ];
  for (const [name, mutate, failure, signed] of mutations) {
    const value = structuredClone(valid);
    mutate(value);
    if (signed) resign(value);
    writeFileSync(file, JSON.stringify(value));
    assert.throws(() => readReadingReference(), failure, `reject ${name}`);
  }
} finally { writeFileSync(file, original); }
readReadingReference();
console.log("Journal reference: missing, truncated, stale, altered, empty-body and partial asset observations each rejected by their own check; healthy reference retained");
