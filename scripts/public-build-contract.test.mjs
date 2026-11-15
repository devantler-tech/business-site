import assert from "node:assert/strict";
import { readFileSync, mkdtempSync, mkdirSync, writeFileSync, copyFileSync, existsSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import test from "node:test";
import * as yaml from "js-yaml";

const load = file => yaml.load(readFileSync(file,"utf8"));
function installation(steps, build) {
  const install = steps.findIndex(step => step.run === "npm --prefix web ci");
  assert.ok(install >= 0 && install < build,"Install candidate dependencies before the common build");
  assert.equal(steps[install].if,undefined);
  assert.ok(steps[install]["continue-on-error"] == null || steps[install]["continue-on-error"] === false);
}
// The production runtime is the only public deployment; its release admission
// is exercised by runtime-release.test.sh and runtime-publisher-contract.test.sh.
test("actual production runtime builder installs both pinned package graphs", () => {
  const steps = load(".github/workflows/ci.yaml").jobs["test-combined-runtime"].steps;
  const build = steps.findIndex(step => step.run === "bash scripts/package-runtime.sh --release");
  installation(steps,build);
  for (const mutation of [steps.filter(step => step.run !== "npm --prefix web ci"), steps.map(step => step.run === "npm --prefix web ci" ? {...step,if:"false"} : step), steps.map(step => step.run === "npm --prefix web ci" ? {...step,"continue-on-error":true} : step)]) assert.throws(() => installation(mutation,mutation.findIndex(step => step.run === "bash scripts/package-runtime.sh --release")));
});
test("the sole public Go package retains extracted-archive and container proof", () => {
  const steps=load(".github/workflows/ci.yaml").jobs["test-combined-runtime"].steps;
  const build=steps.findIndex(step=>step.run==="bash scripts/package-runtime.sh --release");
  const smoke=steps.findIndex(step=>step.run==="node scripts/runtime-smoke.test.mjs runtime-dist");
  const archive=steps.findIndex(step=>step.run?.includes('tar -xzf runtime-dist/business-site-runtime.tar.gz')&&step.run?.includes('node scripts/runtime-smoke.test.mjs "$extracted"'));
  const container=steps.findIndex(step=>step.run==="node scripts/container-smoke.test.mjs business-site:test runtime-dist");
  assert.ok(build>=0&&smoke>build&&archive>smoke&&container>archive);
  for(const index of [build,smoke,archive,container]) {assert.equal(steps[index].if,undefined);assert.ok(!steps[index]["continue-on-error"]);}
});
test("the actual common shell removes reference output when selected building fails", () => {
  const directory = mkdtempSync(join(tmpdir(),"selected-failure-"));
  try {
    mkdirSync(join(directory,"scripts")); mkdirSync(join(directory,"bin"));
    copyFileSync("scripts/build-business-site.sh",join(directory,"scripts/build-business-site.sh"));
    writeFileSync(join(directory,"bin/node"),'#!/bin/sh\nif [ "$1" = web/public-selection.ts ]; then printf "svelte\\n"; fi\n',{mode:0o700});
    writeFileSync(join(directory,"bin/bash"),'#!/bin/sh\nif [ "$1" = scripts/build-journal-reference.sh ]; then mkdir dist; touch dist/index.html; exit 42; fi\n',{mode:0o700});
    const result = spawnSync("/bin/bash",["scripts/build-business-site.sh"],{cwd:directory,env:{...process.env,PATH:`${directory}/bin:${process.env.PATH}`},timeout:10000});
    assert.equal(result.status,42,"Retain the actual reference failure rather than turning cleanup into success");
    assert.equal(existsSync(join(directory,"dist")),false,"A failed selected build cannot leave a publishable-looking reference");
  } finally { rmSync(directory,{recursive:true}); }
});
