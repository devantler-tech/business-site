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
test("actual publisher and runtime consumers install both pinned package graphs", () => {
  const publisher = load(".github/workflows/publish-site.yaml");
  const runtime = load(".github/workflows/ci.yaml").jobs["test-combined-runtime"];
  for (const steps of [publisher.jobs.build.steps,runtime.steps]) {
    const build = steps.findIndex(step => ["npm run build","bash scripts/package-runtime.sh --release"].includes(step.run));
    installation(steps,build);
    for (const mutation of [steps.filter(step => step.run !== "npm --prefix web ci"), steps.map(step => step.run === "npm --prefix web ci" ? {...step,if:"false"} : step), steps.map(step => step.run === "npm --prefix web ci" ? {...step,"continue-on-error":true} : step)]) assert.throws(() => installation(mutation,mutation.findIndex(step => ["npm run build","bash scripts/package-runtime.sh --release"].includes(step.run))));
  }
});
test("publication admits only authorized main sources and explicit publish mode", () => {
  const workflow=load(".github/workflows/publish-site.yaml");
  assert.equal(workflow.on.workflow_dispatch.inputs.frontend,undefined);
  const admission=workflow.jobs.build.steps[0].run;
  const base={...process.env,PUBLICATION_REPOSITORY:"devantler-tech/business-site",PUBLICATION_REF:"refs/heads/main",PUBLICATION_EVENT:"workflow_dispatch",PUBLICATION_ENABLED:"true",SOURCE_REVISION:"a".repeat(40),PUBLICATION_MODE:"preview"};
  const admit=changes=>spawnSync("bash",["-c",admission],{env:{...base,...changes}}).status;
  assert.equal(admit({}),0);
  for(const event of ["push","schedule","workflow_dispatch"]) {
    assert.equal(admit({PUBLICATION_MODE:"publish",PUBLICATION_EVENT:event}),0);
    for(const enabled of ["","false","TRUE"])assert.notEqual(admit({PUBLICATION_MODE:"publish",PUBLICATION_EVENT:event,PUBLICATION_ENABLED:enabled}),0);
  }
  for(const changes of [{PUBLICATION_REPOSITORY:"devantler-tech/monorepo"},{PUBLICATION_REF:"refs/heads/test"},{PUBLICATION_EVENT:"pull_request"},{PUBLICATION_EVENT:"push"},{SOURCE_REVISION:"invalid"}])assert.notEqual(admit(changes),0);
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
