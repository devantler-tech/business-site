import assert from "node:assert/strict";
import test from "node:test";
import { artifact, assertDownloads, assertSitemap, assertSharing } from "./discovery-output.mjs";

const enabled = process.argv[2] === "enabled";
assert.ok(enabled || process.argv[2] === "disabled","Declare discovery enabled or disabled");
const candidate = artifact("web/build");
if (!enabled) {
  for (const path of candidate.keys()) assert.ok(!/^(?:pdfs\/|_astro\/|favicon\.png$|robots\.txt$|sitemap[^/]*\.xml$)/.test(path),`Disabled public discovery leaked ${path}`);
  console.log("Public discovery disabled: downloads, XML, robots, favicon and sharing assets absent");
} else {
  const baseline = artifact("dist");
  test("actual candidate independently retains the authoritative downloads", () => assertDownloads(candidate,baseline));
  test("actual candidate discovers exactly its admitted canonical pages", () => { assertSitemap(candidate,baseline); });
  test("actual candidate retains complete bilingual business sharing", () => assertSharing(candidate,baseline));
}
