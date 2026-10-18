import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, symlinkSync, rmSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { validateSelection, promoteSelection } from "./public-build.mjs";

test("selection rejects an empty or partial artifact rather than reusing the reference", () => {
  const root = mkdtempSync(join(tmpdir(), "public-selection-"));
  try {
    const reference = join(root,"reference"), candidate = join(root,"candidate"), target = join(root,"dist");
    mkdirSync(reference); mkdirSync(candidate); mkdirSync(target);
    const files = {"index.html":'<html><link rel="canonical" href="https://devantler.tech/"><script src="/theme.js"></script></html>', "da/index.html":'<html><link rel="canonical" href="https://devantler.tech/da/"></html>', "theme.js":"theme", "CNAME":"devantler.tech\n"};
    const put = (directory, name, bytes) => { mkdirSync(join(directory,name,".."),{recursive:true}); writeFileSync(join(directory,name),bytes); };
    for (const [name,bytes] of Object.entries(files)) put(reference,name,bytes);
    assert.throws(() => validateSelection(candidate,reference), /missing/);
    for (const [name,bytes] of Object.entries(files)) if (name !== "CNAME") put(candidate,name,bytes);
    validateSelection(candidate,reference);
    for (const [name,bytes,pattern] of [["publication-source.json","{}",/receipt/],["portal/index.html","private",/private/],["stale.html","stale",/unexpected/]]) {
      put(candidate,name,bytes);
      assert.throws(() => validateSelection(candidate,reference),pattern);
      rmSync(join(candidate,name));
    }
    symlinkSync(join(reference,"theme.js"),join(candidate,"linked.js"));
    assert.throws(() => validateSelection(candidate,reference), /regular/);
    rmSync(join(candidate,"linked.js"));
    rmSync(join(candidate,"theme.js"));
    assert.throws(() => validateSelection(candidate,reference), /dependency/);
    put(candidate,"theme.js","theme");
    put(target,"old-astro.js","stale");
    promoteSelection(candidate,reference,target);
    assert.equal(existsSync(join(target,"old-astro.js")),false);
    assert.equal(readFileSync(join(target,"CNAME"),"utf8"),"devantler.tech\n");
    assert.equal(existsSync(join(candidate,"CNAME")),false,"selection owns CNAME, not the candidate renderer");
    assert.equal(readFileSync(join(target,"index.html"),"utf8"),files["index.html"]);
  } finally { rmSync(root,{recursive:true}); }
});
