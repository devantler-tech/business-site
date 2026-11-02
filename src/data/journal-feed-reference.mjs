import assert from "node:assert/strict";
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { readingFingerprint } from "./journal-reading.mjs";
import { readJournal } from "./journal.mjs";
import { validateJournalFeed, feedAssetPaths } from "./journal-feed.mjs";

const root = new URL("../../",import.meta.url);
const generated = new URL(".generated/journal-feed.json",root);
/** @param {string|Buffer} bytes */
const digest = bytes => createHash("sha256").update(bytes).digest("hex");

/** Capture the real XML and its image dependency closure within the existing renderer boundary. */
export function captureJournalFeedReference() {
  const fingerprint = readingFingerprint();
  assert.equal(readFileSync(new URL(".generated/journal-reading-input.txt",root),"utf8"),fingerprint,"Journal feed source changed during rendering");
  const xml = readFileSync(new URL(".generated/content/blog/rss.xml",root),"utf8");
  const paths = feedAssetPaths(validateJournalFeed(xml,readJournal()));
  const assets = paths.map(filename => {
    const bytes = readFileSync(new URL(`.generated/content/${filename}`,root));
    return {filename,source:bytes.toString("base64"),digest:digest(bytes)};
  });
  assert.equal(readingFingerprint(),fingerprint,"Journal feed source changed during capture");
  const value = {version:1,fingerprint,xml,assets};
  writeFileSync(generated,JSON.stringify({...value,checksum:digest(JSON.stringify(value))}));
}

/** Read only a complete current feed; missing images never become a partial successful artifact. */
export function readJournalFeedReference() {
  const value = JSON.parse(readFileSync(generated,"utf8"));
  assert.ok(value.version === 1 && value.fingerprint === readingFingerprint(),"Journal feed reference is stale; run bash scripts/build-journal-reference.sh");
  const {checksum,...content} = value;
  assert.equal(checksum,digest(JSON.stringify(content)),"Journal feed reference is incomplete or altered");
  const paths = feedAssetPaths(validateJournalFeed(value.xml,readJournal()));
  assert.ok(Array.isArray(value.assets),"Journal feed asset inventory required");
  assert.deepEqual(value.assets.map((/** @type {{filename:string}} */ asset) => asset.filename),paths,"Journal feed complete asset inventory required");
  for (const asset of value.assets) assert.equal(digest(Buffer.from(asset.source,"base64")),asset.digest,"Journal feed asset digest mismatch");
  return value;
}
