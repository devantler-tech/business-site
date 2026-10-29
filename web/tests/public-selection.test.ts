import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { selectedFrontend } from "../public-selection.ts";

test("Svelte is selected with absent or obsolete frontend settings; unsupported presentation still fails", () => {
  const directory=mkdtempSync(join(tmpdir(),"public-profile-"));
  const keys=["FEATURE_SVELTE_FRONTEND","FEATURE_PREVIEW_BANNER","FEATURE_CLIENT_PORTFOLIO","FEATURE_FEATURED_PORTFOLIO"];
  const previous=Object.fromEntries(keys.map(key=>[key,process.env[key]]));
  try {
    for(const key of keys) delete process.env[key];
    assert.equal(selectedFrontend(directory),"svelte");
    writeFileSync(join(directory,".env.production"),"FEATURE_SVELTE_FRONTEND=false\n");
    assert.equal(selectedFrontend(directory),"svelte");
    for(const value of ["false","true","obsolete"]) {process.env.FEATURE_SVELTE_FRONTEND=value; assert.equal(selectedFrontend(directory),"svelte");}
    for(const [key,value] of [["FEATURE_PREVIEW_BANNER","true"],["FEATURE_CLIENT_PORTFOLIO","false"],["FEATURE_FEATURED_PORTFOLIO","false"]]) {
      process.env[key]=value;
      assert.throws(()=>selectedFrontend(directory),/unsupported/);
      delete process.env[key];
    }
  } finally {
    for(const key of keys) {if(previous[key]===undefined)delete process.env[key];else process.env[key]=previous[key];}
    rmSync(directory,{recursive:true});
  }
});
