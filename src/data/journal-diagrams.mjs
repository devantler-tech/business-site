import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { createHash } from "node:crypto";

/** Bundle the existing pinned renderer with the candidate's own build tool. */
export async function bundleJournalDiagrams() {
  const require = createRequire(new URL("../../web/package.json", import.meta.url));
  const { build } = /** @type {typeof import("../../web/node_modules/vite/dist/node/index.js")} */ (await import(pathToFileURL(require.resolve("vite")).href));
  const result = await build({
    configFile:false, publicDir:false, base:"./", logLevel:"warn",
    build:{
      write:false, minify:true,
      lib:{entry:new URL("../../web/src/lib/journal-diagrams.js", import.meta.url).pathname,formats:["es"]},
      rolldownOptions:{output:{entryFileNames:"journal-diagrams-[hash].js",chunkFileNames:"diagram-[hash].js"}},
    },
  });
  const results = Array.isArray(result) ? result : [result];
  assert.equal(results.length, 1, "Journal one diagram bundle required");
  assert.ok("output" in results[0], "Journal diagram bundle required");
  const chunks = results[0].output;
  assert.ok(chunks.length && chunks.every(chunk => chunk.type === "chunk"), "Journal diagram bundle contains unsupported assets");
  const names = new Set(chunks.map(chunk => chunk.fileName));
  const entries = chunks.filter(chunk => chunk.type === "chunk" && chunk.isEntry);
  assert.equal(entries.length, 1, "Journal diagram entry required");
  const prefix = "_app/immutable/assets/reading-diagrams/";
  const assets = chunks.map(chunk => {
    assert.ok(chunk.type === "chunk");
    assert.match(chunk.fileName, /^[A-Za-z0-9_-]+\.js$/, "Journal diagram asset name required");
    const dependencies = [...chunk.imports, ...chunk.dynamicImports];
    assert.ok(dependencies.every(name => names.has(name)), "Journal diagram module closure is incomplete or external");
    const bytes = Buffer.from(chunk.code);
    return {filename:prefix + chunk.fileName,source:bytes.toString("base64"),digest:createHash("sha256").update(bytes).digest("hex"),dependencies:dependencies.map(name => prefix + name)};
  });
  return {entry:`/${prefix}${entries[0].fileName}`,assets};
}
