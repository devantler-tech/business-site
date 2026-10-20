import assert from "node:assert/strict";
import { readFileSync, writeFileSync, unlinkSync } from "node:fs";
import { createHash } from "node:crypto";
import { readPublicDiscoveryReference } from "../../src/data/public-discovery-reference.mjs";

const file = new URL("../../.astro/public-discovery.json",import.meta.url);
const original = readFileSync(file,"utf8"), valid = JSON.parse(original);
const digest = (/** @type {string|Buffer} */ bytes) => createHash("sha256").update(bytes).digest("hex");
try {
  readPublicDiscoveryReference();
  unlinkSync(file);
  assert.throws(readPublicDiscoveryReference,/ENOENT/);
  writeFileSync(file,original.slice(0,-1));
  assert.throws(readPublicDiscoveryReference,SyntaxError);
  /** @type {[string,(value:any)=>void,RegExp][]} */
  const controls = [
    ["stale source",value => { value.fingerprint="stale"; },/reference is stale/],
    ["missing CV",value => { value.assets.shift(); },/complete asset inventory/],
    ["missing thesis beside healthy CV",value => { value.assets.splice(1,1); },/complete asset inventory/],
    ["corrupted later asset",value => { value.assets[1].source=Buffer.from("corrupt").toString("base64"); },/asset digest mismatch/],
    ["invalid PDF with matching digest",value => { const bytes=Buffer.alloc(1500,65); value.assets[0].source=bytes.toString("base64"); value.assets[0].digest=digest(bytes); },/real PDF/],
    ["missing portrait",value => { value.assets.pop(); },/complete asset inventory/],
    ["foreign portrait",value => { value.sharingImage="https://example.com/profile.webp"; },/local maintained sharing portrait/],
    ["unreviewed extra file",value => { value.assets.push({filename:"CNAME",source:"",digest:digest("")}); },/complete asset inventory/],
  ];
  for (const [name,mutate,error] of controls) {
    const value = structuredClone(valid);
    mutate(value);
    const {checksum,...content} = value;
    value.checksum=digest(JSON.stringify(content));
    writeFileSync(file,JSON.stringify(value));
    assert.throws(readPublicDiscoveryReference,error,`reject ${name}`);
  }
  const value = structuredClone(valid);
  value.assets[0].source="altered";
  writeFileSync(file,JSON.stringify(value));
  assert.throws(readPublicDiscoveryReference,/incomplete or altered/);
} finally { writeFileSync(file,original); }
readPublicDiscoveryReference();
console.log("Public discovery reference: missing/truncated input and nine independent stale/closure/PDF/asset controls rejected; healthy reference restored");
