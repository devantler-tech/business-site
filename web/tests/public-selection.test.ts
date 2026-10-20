import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { selectedFrontend } from "../public-selection.ts";

test("one framework-native production selection rejects ambiguous flags and respects explicit overrides", () => {
  const directory = mkdtempSync(join(tmpdir(),"frontend-selection-"));
  const before = process.env.FEATURE_SVELTE_FRONTEND;
  const presentation = ["FEATURE_PREVIEW_BANNER","FEATURE_CLIENT_PORTFOLIO","FEATURE_FEATURED_PORTFOLIO"];
  const previous = Object.fromEntries(presentation.map(key => [key,process.env[key]]));
  try {
    delete process.env.FEATURE_SVELTE_FRONTEND;
    for (const key of presentation) delete process.env[key];
    assert.equal(selectedFrontend(directory),"astro");
    writeFileSync(join(directory,".env.production"),"FEATURE_SVELTE_FRONTEND=true\n");
    assert.equal(selectedFrontend(directory),"svelte");
    process.env.FEATURE_PREVIEW_BANNER="true";
    assert.throws(() => selectedFrontend(directory), /unsupported/);
    delete process.env.FEATURE_PREVIEW_BANNER;
    for (const key of presentation.slice(1)) {
      process.env[key]="false";
      assert.throws(() => selectedFrontend(directory),/unsupported/);
      delete process.env[key];
    }
    process.env.FEATURE_SVELTE_FRONTEND="false";
    assert.equal(selectedFrontend(directory),"astro");
    for (const value of ["", "yes", "TRUE", "1"]) {
      process.env.FEATURE_SVELTE_FRONTEND=value;
      assert.throws(() => selectedFrontend(directory), /must be true or false/);
    }
  } finally {
    if (before === undefined) delete process.env.FEATURE_SVELTE_FRONTEND;
    else process.env.FEATURE_SVELTE_FRONTEND=before;
    for (const key of presentation) {
      if (previous[key] === undefined) delete process.env[key]; else process.env[key]=previous[key];
    }
    rmSync(directory,{recursive:true});
  }
});
