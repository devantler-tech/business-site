import assert from "node:assert/strict";
import fs, { mkdtempSync, mkdirSync, writeFileSync, symlinkSync, rmSync, readFileSync, existsSync, renameSync } from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { createHash } from "node:crypto";
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
    const nonce = "f6e10773-c425-46e1-924f-d44f9cb366c6";
    const manifest = join(root,"inventory.json");
    writeFileSync(manifest,JSON.stringify({version:1,nonce,files:Object.fromEntries(Object.entries(files).filter(([name])=>name!=="CNAME").map(([name,bytes])=>[name,createHash("sha256").update(bytes).digest("hex")]))}));
    const options = {nonce,manifest};
    const check = () => validateSelection(candidate,reference,options);
    assert.throws(check, /missing/);
    for (const [name,bytes] of Object.entries(files)) if (name !== "CNAME") put(candidate,name,bytes);
    check();
    for (const [name,bytes,pattern] of [["publication-source.json","{}",/receipt/],["portal/index.html","private",/private/],["stale.html","stale",/unexpected/]]) {
      put(candidate,name,bytes);
      assert.throws(check,pattern);
      rmSync(join(candidate,name));
    }
    symlinkSync(join(reference,"theme.js"),join(candidate,"linked.js"));
    assert.throws(check, /ELOOP|regular/);
    rmSync(join(candidate,"linked.js"));
    rmSync(join(candidate,"theme.js"));
    assert.throws(check, /dependency/);
    put(candidate,"theme.js","theme");
    put(target,"old-astro.js","stale");
    for (const [name,bytes] of [["obsolete.js","old build"],["stale.bin","unexpected bytes"]]) {
      put(candidate,name,bytes);
      assert.throws(check,/stale|unexpected/);
      rmSync(join(candidate,name));
    }
    put(candidate,"theme.js","old theme");
    assert.throws(check,/stale/);
    put(candidate,"theme.js","theme");
    assert.throws(()=>validateSelection(candidate,reference,{...options,nonce:"4a101825-6956-4e60-81d8-f0bfcc1ac58a"}),/current build/);
    promoteSelection(candidate,reference,target,options);
    assert.equal(existsSync(join(target,"old-astro.js")),false);
    assert.equal(readFileSync(join(target,"CNAME"),"utf8"),"devantler.tech\n");
    assert.equal(existsSync(join(candidate,"CNAME")),false,"selection owns CNAME, not the candidate renderer");
    assert.equal(readFileSync(join(target,"index.html"),"utf8"),files["index.html"]);
  } finally { rmSync(root,{recursive:true}); }
});

test("all resource-link token forms reject a missing local dependency", () => {
  const root = mkdtempSync(join(tmpdir(),"public-link-"));
  try {
    const candidate=join(root,"candidate"),reference=join(root,"reference");
    mkdirSync(candidate); mkdirSync(reference);
    for (const directory of [candidate,reference]) {
      mkdirSync(join(directory,"da")); writeFileSync(join(directory,"da/index.html"),"<html></html>");
    }
    writeFileSync(join(reference,"CNAME"),"devantler.tech\n");
    writeFileSync(join(reference,"index.html"),"<html></html>");
    for (const rel of ["shortcut icon","icon shortcut","APPLE-TOUCH-ICON","preload stylesheet","manifest","mask-icon"]) {
      writeFileSync(join(candidate,"index.html"),`<html><link rel="${rel}" href="/missing.svg"></html>`);
      assert.throws(()=>validateSelection(candidate,reference),/dependency/,rel);
      writeFileSync(join(candidate,"missing.svg"),"<svg></svg>");
      const manifest=join(root,"inventory.json"),nonce="f6e10773-c425-46e1-924f-d44f9cb366c6";
      writeFileSync(manifest,JSON.stringify({version:1,nonce,files:Object.fromEntries(["index.html","da/index.html","missing.svg"].map(name=>[name,createHash("sha256").update(readFileSync(join(candidate,name))).digest("hex")]))}));
      validateSelection(candidate,reference,{manifest,nonce});
      rmSync(join(candidate,"missing.svg"));
    }
    writeFileSync(join(candidate,"index.html"),'<html><link rel="preload" href="https://devantler.tech/missing.css"></html>');
    assert.throws(()=>validateSelection(candidate,reference),/dependency/,"Canonical absolute dependencies are still local");
  } finally { rmSync(root,{recursive:true}); }
});

test("an actual directory replaced after enumeration cannot be followed", t => {
  const root = mkdtempSync(join(tmpdir(),"public-directory-"));
  const candidate=join(root,"candidate"),reference=join(root,"reference"),outside=join(root,"outside");
  try {
    for (const directory of [candidate,reference]) {
      mkdirSync(join(directory,"assets"),{recursive:true}); mkdirSync(join(directory,"da"));
      writeFileSync(join(directory,"index.html"),'<html><script src="/assets/current.js"></script></html>');
      writeFileSync(join(directory,"da/index.html"),"<html></html>");
      writeFileSync(join(directory,"assets/current.js"),"public");
    }
    writeFileSync(join(reference,"CNAME"),"devantler.tech\n");
    mkdirSync(outside); writeFileSync(join(outside,"current.js"),"must not be copied");
    const original=fs.readdirSync;
    t.mock.method(fs,"readdirSync",function(path,...args) {
      const entries=original.call(this,path,...args);
      if (path===candidate) { renameSync(join(candidate,"assets"),join(root,"held")); symlinkSync(outside,join(candidate,"assets"),"dir"); }
      return entries;
    });
    syncBuiltinESMExports();
    assert.throws(()=>validateSelection(candidate,reference),/regular|changed|ELOOP/);
  } finally { t.mock.restoreAll(); syncBuiltinESMExports(); rmSync(root,{recursive:true}); }
});
