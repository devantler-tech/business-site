import assert from "node:assert/strict";
import fs from "node:fs";
import { mock } from "node:test";
import { syncBuiltinESMExports } from "node:module";
import { capturePublicDiscoveryReference } from "../../src/data/public-discovery-reference.mjs";

const file = new URL("../../.generated/content/pdfs/thesis.pdf",import.meta.url);
const backup = new URL("../../.generated/content/pdfs/thesis.capture-control",import.meta.url);
const substitute = new URL("../../.generated/content/pdfs/nikolai-emil-damm-cv.pdf",import.meta.url);
const reference = new URL("../../.generated/public-discovery.json",import.meta.url);
const originalReference = fs.readFileSync(reference);
const original = fs.readFileSync(file);
const lstat = fs.lstatSync;
let replaced = false;
try {
  mock.method(fs,"lstatSync",/** @type {typeof fs.lstatSync} */ ((path,...args) => {
    const result = lstat(path,...args);
    if (String(path) === String(file) && !replaced) {
      fs.renameSync(file,backup);
      fs.symlinkSync(substitute,file);
      replaced = true;
    }
    return result;
  }));
  syncBuiltinESMExports();
  capturePublicDiscoveryReference();
  assert.ok(replaced,"replacement control must exercise the actual capture path");
  const captured = JSON.parse(fs.readFileSync(reference,"utf8"));
  assert.equal(Buffer.from(captured.assets.find((/** @type {{filename:string}} */ item) => item.filename === "pdfs/thesis.pdf").source,"base64").equals(original),true,"Public discovery must not consume replacement bytes after inspecting an artifact");
} finally {
  mock.restoreAll();
  syncBuiltinESMExports();
  if (replaced) { fs.unlinkSync(file); fs.renameSync(backup,file); }
  fs.writeFileSync(reference,originalReference);
}
try {
  fs.renameSync(file,backup);
  fs.symlinkSync(substitute,file);
  assert.throws(capturePublicDiscoveryReference,/ELOOP|regular file/,"Public discovery rejects a symlink artifact");
} finally {
  fs.unlinkSync(file);
  fs.renameSync(backup,file);
  fs.writeFileSync(reference,originalReference);
}
console.log("Public discovery capture: symlink input rejected and inspected artifact bytes retained across pathname substitution");
