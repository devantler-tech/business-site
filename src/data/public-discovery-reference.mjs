import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync, lstatSync, openSync, fstatSync, closeSync, constants } from "node:fs";
import { parse } from "parse5";
import { readingFingerprint } from "./journal-reading.mjs";

const root = new URL("../../",import.meta.url);
const generated = new URL(".generated/public-discovery.json",root);
/** @param {string|Buffer} bytes */
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
/** @param {import('parse5').DefaultTreeAdapterMap['node']} node @returns {import('parse5').DefaultTreeAdapterMap['element'][]} */
const all = node => [...("tagName" in node ? [node] : []),...("childNodes" in node ? node.childNodes.flatMap(all) : [])];
/** @param {import('parse5').DefaultTreeAdapterMap['element']} node @param {string} name */
const attr = (node,name) => node.attrs.find(item => item.name === name)?.value;

/** Inspect and consume the same file; never follow a replaced final pathname. @param {URL} file */
function artifactBytes(file) {
  assert.ok(Number.isInteger(constants.O_NOFOLLOW),"Public discovery requires no-follow artifact reads");
  const descriptor = openSync(file,constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try {
    const info = fstatSync(descriptor);
    assert.ok(info.isFile(),"Public discovery artifact must be a regular file");
    const bytes = readFileSync(descriptor);
    const after = lstatSync(file);
    assert.ok(after.isFile() && after.dev === info.dev && after.ino === info.ino,"Public discovery artifact changed while it was read");
    return bytes;
  } finally { closeSync(descriptor); }
}

/** Validate the entire app-owned closure before any asset can be emitted. @param {any} value */
function validate(value) {
  assert.ok(value.version === 1 && value.fingerprint === readingFingerprint(),"Public discovery reference is stale; run bash scripts/build-journal-reference.sh");
  const {checksum,...content} = value;
  assert.equal(checksum,digest(JSON.stringify(content)),"Public discovery reference is incomplete or altered");
  assert.match(value.sharingImage ?? "",/^https:\/\/devantler\.tech\/_astro\/profile\.[A-Za-z0-9_-]+\.webp$/,"Public discovery requires the local maintained sharing portrait");
  const paths = ["pdfs/nikolai-emil-damm-cv.pdf","pdfs/thesis.pdf","favicon.png","robots.txt",new URL(value.sharingImage).pathname.slice(1)];
  assert.ok(Array.isArray(value.assets),"Public discovery asset inventory required");
  assert.deepEqual(value.assets.map((/** @type {{filename:string}} */ asset) => asset.filename),paths,"Public discovery complete asset inventory required");
  for (const asset of value.assets) {
    assert.equal(typeof asset.source,"string","Public discovery asset bytes required");
    const bytes = Buffer.from(asset.source,"base64");
    assert.equal(bytes.toString("base64"),asset.source,"Public discovery complete base64 required");
    assert.equal(digest(bytes),asset.digest,"Public discovery asset digest mismatch");
    if (asset.filename.endsWith(".pdf")) {
      assert.ok(bytes.length > 1000,"Public discovery PDF is incomplete");
      assert.match(bytes.subarray(0,8).toString(),/^%PDF-1\./,"Public discovery requires a real PDF");
      assert.match(bytes.subarray(-40).toString(),/%%EOF\s*$/,"Public discovery PDF is incomplete");
    }
    if (asset.filename === "favicon.png") assert.equal(bytes.subarray(0,8).toString("hex"),"89504e470d0a1a0a","Public discovery PNG required");
    if (asset.filename.endsWith(".webp")) assert.ok(bytes.subarray(0,4).toString() === "RIFF" && bytes.subarray(8,12).toString() === "WEBP","Public discovery WebP required");
    if (asset.filename === "robots.txt") assert.match(bytes.toString(),/^Sitemap: https:\/\/devantler\.tech\/sitemap-index\.xml$/m,"Public discovery robots relationship required");
  }
  return value;
}

/** Capture the freshly generated CV and the exact maintained public dependencies, not all .generated/content/public. */
export function capturePublicDiscoveryReference() {
  const fingerprint = readingFingerprint();
  assert.equal(readFileSync(new URL(".generated/journal-reading-input.txt",root),"utf8"),fingerprint,"Public discovery source changed during rendering");
  const images = all(parse(artifactBytes(new URL(".generated/content/index.html",root)).toString())).filter(node => node.tagName === "meta" && attr(node,"property") === "og:image");
  assert.equal(images.length,1,"Public discovery requires one authoritative sharing image");
  const sharingImage = attr(images[0],"content");
  assert.match(sharingImage ?? "",/^https:\/\/devantler\.tech\/_astro\/profile\.[A-Za-z0-9_-]+\.webp$/,"Public discovery sharing image must remain local");
  const paths = ["pdfs/nikolai-emil-damm-cv.pdf","pdfs/thesis.pdf","favicon.png","robots.txt",new URL(/** @type {string} */ (sharingImage)).pathname.slice(1)];
  const assets = paths.map(filename => {
    const file = new URL(`.generated/content/${filename}`,root);
    const bytes = artifactBytes(file);
    return {filename,source:bytes.toString("base64"),digest:digest(bytes)};
  });
  assert.equal(readingFingerprint(),fingerprint,"Public discovery source changed during capture");
  const content = {version:1,fingerprint,sharingImage,assets};
  const value = {...content,checksum:digest(JSON.stringify(content))};
  validate(value);
  writeFileSync(generated,JSON.stringify(value));
}

/** Missing, stale, partial or changed captures never become a successful candidate. */
export function readPublicDiscoveryReference() { return validate(JSON.parse(readFileSync(generated,"utf8"))); }
