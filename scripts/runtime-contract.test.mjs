import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import test from "node:test";
import * as yaml from "js-yaml";

const workflow = yaml.load(readFileSync(".github/workflows/ci.yaml", "utf8"));
test("generated lint reports stay ignored without excluding source", () => {
  for (const path of [
    "megalinter-reports/megalinter.log",
    "megalinter-reports/updated_sources/megalinter-reports/sbom/trivy.json",
  ]) {
    const ignored = spawnSync(
      "git",
      ["-c", "core.excludesFile=/dev/null", "check-ignore", "--no-index", "--verbose", "--", path],
      { encoding: "utf8" },
    );
    assert.ifError(ignored.error);
    assert.equal(ignored.status, 0, `${path} must be ignored by the repository`);
    assert.match(ignored.stdout, /^\.gitignore:\d+:\/megalinter-reports\//);
  }
  for (const path of [
    "server/internal/identity/portal_links_test.go",
    "server/DEPENDENCIES.md",
    "scripts/runtime-contract.test.mjs",
  ]) {
    const source = spawnSync("git", ["-c", "core.excludesFile=/dev/null", "check-ignore", "--no-index", "--", path], {
      encoding: "utf8",
    });
    assert.ifError(source.error);
    assert.equal(source.status, 1, `${path} must remain eligible for validation and fixes`);
    assert.equal(source.stdout, "");
  }
});
test("the shipped source does not track generated lint reports", () => {
  const tracked = spawnSync("git", ["ls-files", "-z", "--", "megalinter-reports"], { encoding: "utf8" });
  assert.ifError(tracked.error);
  assert.equal(tracked.status, 0);
  assert.equal(tracked.stdout, "", "lint artifacts belong in the workflow artifact, not the application source");
});
/** Reject missing, conditional or error-tolerant runtime checks in the actual CI workflow. */
function assertRuntime(workflow) {
  const job = workflow.jobs["test-portal-identity"];
  assert.equal(job.if, undefined);
  const steps = job.steps;
  const node = steps.findIndex((s) => s.uses?.startsWith("actions/setup-node@"));
  const install = steps.findIndex((s) => s.run === "npm ci" && s["working-directory"] === ".");
  const browser = steps.findIndex(
    (s) => s.run === "node --test scripts/portal-request-limits.test.mjs" && s["working-directory"] === ".",
  );
  const build = steps.findIndex(
    (s) => s.run === "bash scripts/package-runtime.sh --release" && s["working-directory"] === ".",
  );
  const inventory = steps.findIndex(
    (s) => s.run === "bash scripts/runtime-dependencies.test.sh" && s["working-directory"] === ".",
  );
  const smoke = steps.findIndex(
    (s) => s.run === "node scripts/runtime-smoke.test.mjs runtime-dist" && s["working-directory"] === ".",
  );
  const extract = steps.findIndex(
    (s) =>
      s.run?.includes('tar -xzf runtime-dist/business-site-runtime.tar.gz -C "$extracted"') &&
      s["working-directory"] === ".",
  );
  const uploadIndex = steps.findIndex((s) => s.with?.name === "business-site-runtime");
  assert.equal(steps[node]?.with?.["node-version-file"], ".node-version");
  assert.equal(steps[node]?.with?.["node-version"], undefined);
  assert.ok(node >= 0 && install > node && build > install && smoke > build);
  assert.ok(inventory > install && inventory < build, "verify compiled-module inventory before packaging");
  assert.ok(
    browser > install && browser < build,
    "run the actual served browser controls with explicitly configured Node before packaging",
  );
  assert.ok(extract > smoke && uploadIndex > extract, "test the extracted archive before upload");
  assert.match(steps[extract]?.run ?? "", /node scripts\/runtime-smoke\.test\.mjs "\$extracted"/);
  for (const index of [node, install, browser, inventory, build, smoke, extract, uploadIndex]) {
    assert.equal(steps[index].if, undefined, "required runtime steps cannot be conditional");
    assert.ok(
      steps[index]["continue-on-error"] == null || steps[index]["continue-on-error"] === false,
      "required runtime failures cannot be ignored",
    );
  }
  assert.ok(workflow.jobs.status.needs.includes("test-portal-identity"));
  assert.match(workflow.jobs.status.steps[0].with["job-results"], /needs\.test-portal-identity\.result/);
  const upload = steps.find((s) => s.with?.name === "business-site-runtime");
  assert.equal(upload?.with?.["if-no-files-found"], "error");
  assert.equal(upload?.with?.path, "runtime-dist/business-site-runtime.tar.gz");
}
test("CI builds and exercises the actual combined package unconditionally", () => assertRuntime(workflow));

/** The extra production profile must be built and tested, not a metadata promise. */
function assertProductionProfile(w) {
  const steps = w.jobs["test-portal-identity"].steps;
  const build = steps.findIndex((s) => s.run === "bash scripts/package-runtime.sh --release --production-template --svelte");
  const smoke = steps.findIndex((s) => s.run === "node scripts/runtime-smoke.test.mjs runtime-production-dist");
  const extract = steps.findIndex((s) =>
    s.run?.includes('tar -xzf runtime-production-dist/business-site-runtime.tar.gz -C "$extracted"'),
  );
  const image = steps.findIndex((s) => s.run?.includes("--tag business-site:production-test"));
  const exercise = steps.findIndex(
    (s) => s.run === "node scripts/container-smoke.test.mjs business-site:production-test runtime-production-dist",
  );
  assert.ok(build >= 0 && smoke > build && extract > smoke && image > extract && exercise > image);
  assert.match(steps[extract].run, /node scripts\/runtime-smoke\.test\.mjs "\$extracted"/);
  assert.equal(steps[build].env.GH_TOKEN, "${{ github.token }}");
  for (const i of [build, smoke, extract, image, exercise]) {
    assert.equal(steps[i].if, undefined);
    assert.ok(steps[i]["continue-on-error"] == null || steps[i]["continue-on-error"] === false);
    assert.equal(steps[i]["working-directory"], ".");
  }
  const archive = steps.find((s) => s.run?.includes("docker save business-site:production-test"));
  assert.match(archive?.run ?? "", /runtime-production-dist\/business-site-runtime.tar.gz/);
  assert.ok(steps.indexOf(archive) > exercise);
  assert.equal(archive.if, undefined);
  assert.ok(archive["continue-on-error"] == null || archive["continue-on-error"] === false);
}
test("CI exercises the actual production package and image", () => {
  assertProductionProfile(workflow);
  for (const change of [
    (w) => {
      w.jobs["test-portal-identity"].steps.find(
        (s) => s.run === "bash scripts/package-runtime.sh --release --production-template --svelte",
      ).if = "false";
    },
    (w) => {
      w.jobs["test-portal-identity"].steps.find(
        (s) => s.run === "node scripts/runtime-smoke.test.mjs runtime-production-dist",
      )["continue-on-error"] = true;
    },
    (w) => {
      delete w.jobs["test-portal-identity"].steps.find(
        (s) => s.run === "bash scripts/package-runtime.sh --release --production-template --svelte",
      ).env.GH_TOKEN;
    },
    (w) => {
      w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter(
        (s) => s.run !== "node scripts/container-smoke.test.mjs business-site:production-test runtime-production-dist",
      );
    },
    (w) => {
      w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter(
        (s) => !s.run?.includes("tar -xzf runtime-production-dist/"),
      );
    },
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run?.includes("tar -xzf runtime-production-dist/")).if =
        "false";
    },
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run?.includes("tar -xzf runtime-production-dist/"))[
        "continue-on-error"
      ] = true;
    },
  ]) {
    const invalid = structuredClone(workflow);
    change(invalid);
    assert.throws(() => assertProductionProfile(invalid));
  }
});
for (const [name, mutate] of [
  [
    "missing compiled inventory checks",
    (w) => {
      w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter(
        (s) => s.run !== "bash scripts/runtime-dependencies.test.sh",
      );
    },
  ],
  [
    "conditional compiled inventory checks",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "bash scripts/runtime-dependencies.test.sh").if =
        "false";
    },
  ],
  [
    "ignored compiled inventory failures",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "bash scripts/runtime-dependencies.test.sh")[
        "continue-on-error"
      ] = true;
    },
  ],
  [
    "conditional build",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "bash scripts/package-runtime.sh --release").if =
        "false";
    },
  ],
  [
    "ignored smoke failure",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "node scripts/runtime-smoke.test.mjs runtime-dist")[
        "continue-on-error"
      ] = true;
    },
  ],
  [
    "skipped dependency install",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "npm ci").if = "false";
    },
  ],
  [
    "missing browser controls",
    (w) => {
      w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter(
        (s) => s.run !== "node --test scripts/portal-request-limits.test.mjs",
      );
    },
  ],
  [
    "conditional browser controls",
    (w) => {
      w.jobs["test-portal-identity"].steps.find(
        (s) => s.run === "node --test scripts/portal-request-limits.test.mjs",
      ).if = "false";
    },
  ],
  [
    "ignored browser failure",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run === "node --test scripts/portal-request-limits.test.mjs")[
        "continue-on-error"
      ] = true;
    },
  ],
  [
    "missing build",
    (w) => {
      w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter(
        (s) => s.run !== "bash scripts/package-runtime.sh --release",
      );
    },
  ],
  [
    "unaggregated runtime",
    (w) => {
      w.jobs.status.needs = w.jobs.status.needs.filter((n) => n !== "test-portal-identity");
    },
  ],
  [
    "unarchived upload",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.with?.name === "business-site-runtime").with.path =
        "runtime-dist";
    },
  ],
  [
    "skipped archive extraction",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run?.includes("tar -xzf")).if = "false";
    },
  ],
  [
    "ignored extracted-package failure",
    (w) => {
      w.jobs["test-portal-identity"].steps.find((s) => s.run?.includes("tar -xzf"))["continue-on-error"] = true;
    },
  ],
]) {
  test(`runtime contract rejects ${name}`, () => {
    const changed = structuredClone(workflow);
    mutate(changed);
    assert.throws(() => assertRuntime(changed));
  });
}
