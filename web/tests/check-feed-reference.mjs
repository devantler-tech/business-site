import assert from "node:assert/strict";
import {readFileSync,writeFileSync,unlinkSync} from "node:fs";
import {createHash} from "node:crypto";
import {readJournalFeedReference} from "../../src/data/journal-feed-reference.mjs";

const file = new URL("../../.astro/journal-feed.json",import.meta.url);
const original = readFileSync(file,"utf8");
const valid = JSON.parse(original);
assert.ok(valid.assets.length > 1,"partial closure needs a healthy image beside the rejected image");
try {
  readJournalFeedReference();
  unlinkSync(file);
  assert.throws(readJournalFeedReference,/ENOENT/);
  writeFileSync(file,original.slice(0,-1));
  assert.throws(readJournalFeedReference,SyntaxError);
  /** @type {[string,(value:ReturnType<typeof readJournalFeedReference>)=>void,RegExp,boolean][]} */
  const mutations = [
    ["stale source",value => {value.fingerprint = "stale";},/Journal feed reference is stale/,true],
    ["altered XML without checksum",value => {value.xml += "altered";},/Journal feed reference is incomplete or altered/,false],
    ["missing item",value => {value.xml = value.xml.replace(/<item>[\s\S]*?<\/item>/,"");},/Journal feed complete authored item/,true],
    ["missing full content",value => {value.xml = value.xml.replace(/<content:encoded>[\s\S]*?<\/content:encoded>/,"");},/Journal feed complete description\/content/,true],
    ["missing later asset",value => {value.assets.splice(1,1);},/Journal feed complete asset inventory/,true],
    ["corrupted later asset",value => {value.assets[1].source = "corrupt";},/Journal feed asset digest mismatch/,true],
    ["asset outside closure",value => {value.assets[1].filename = "other/file.webp";},/Journal feed complete asset inventory/,true],
  ];
  for (const [name,mutate,failure,resign] of mutations) {
    const value = structuredClone(valid);
    mutate(value);
    if (resign) {
      const {checksum,...content} = value;
      value.checksum = createHash("sha256").update(JSON.stringify(content)).digest("hex");
    }
    writeFileSync(file,JSON.stringify(value));
    assert.throws(readJournalFeedReference,failure,`reject ${name}`);
  }
} finally {writeFileSync(file,original);}
readJournalFeedReference();
console.log("Journal feed reference: missing/truncated input and seven independent stale/incomplete-content/asset controls rejected; healthy reference restored");
