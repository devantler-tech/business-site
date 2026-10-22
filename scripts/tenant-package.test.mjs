// Exercise the rendered application package, not the spelling of its source YAML.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { mkdtempSync, cpSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import * as yaml from "js-yaml";

const root = fileURLToPath(new URL("../", import.meta.url));
const checker = path.join(root, "scripts/check-tenant-package.mjs");
const render = (directory = path.join(root, "deploy")) =>
  execFileSync("kubectl", ["kustomize", directory], { encoding: "utf8", timeout: 15000 });
const check = (input) => spawnSync(process.execPath, [checker], { input, encoding: "utf8", timeout: 5000 });

test("the real Kustomize package renders deterministically and admits the public runtime", () => {
  let first;
  assert.doesNotThrow(() => { first = render(); }, "the source-owned package must exist and render");
  assert.equal(render(), first);
  assert.ok(yaml.loadAll(first).some((d) => d?.kind === "PodDisruptionBudget"), "each workload needs a drain-safe budget");
  const result = check(first);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "tenant package: public-only package accepted\n");
});

test("the rendered network policy explicitly enables egress default deny", () => {
  const network = yaml.loadAll(render()).find((d) => d?.kind === "CiliumNetworkPolicy");
  // Cilium's documented default-deny rule is one empty rule, not an empty list.
  assert.deepEqual(network.spec.egress, [{}]);
});

test("the disruption budget admits two or three replicas", () => {
  for (const replicas of [2, 3]) {
    const docs = yaml.loadAll(render());
    docs.find((d) => d?.kind === "WebApp").spec.replicas = replicas;
    const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
    assert.equal(result.status, 0, result.stderr);
  }
});

// Each mutation names a distinct forbidden capability or runtime failure.
for (const [name, mutate] of [
  ["an extra Secret resource", (d) => d.push({ apiVersion: "v1", kind: "Secret", metadata: { name: "extra" } })],
  ["a foreign namespace", (d) => { d[0].metadata.namespace = "foreign"; }],
  ["a missing namespace", (d) => { delete d[0].metadata.namespace; }],
  ["a mutable image tag", (d) => { d[0].spec.image = "ghcr.io/devantler-tech/business-site:latest"; }],
  ["a foreign image", (d) => { d[0].spec.image = `ghcr.io/other/business-site@sha256:${"a".repeat(64)}`; }],
  ["an unverified same-repository image digest", (d) => { d[0].spec.image = `ghcr.io/devantler-tech/business-site@sha256:${"a".repeat(64)}`; }],
  ["a production hostname", (d) => { d[0].spec.host = "devantler.tech"; }],
  ["an unknown schema field", (d) => { d[0].spec.command = ["/bin/sh"]; }],
  ["portal activation input", (d) => { d[0].spec.env = [{ name: "PORTAL_IDENTITY", value: "true" }]; }],
  ["migration arguments", (d) => { d[0].spec.args = ["--migrate", "--migration-database-file", "/private/db"]; }],
  ["a broken public directory", (d) => { d[0].spec.args[1] = "/missing"; }],
  ["root execution", (d) => { d[0].spec.runAsUser = 0; }],
  ["a foreign primary group", (d) => { d[0].spec.runAsGroup = 65533; }],
  ["a writable root", (d) => { d[0].spec.readOnlyRootFilesystem = false; }],
  ["a wrong probe", (d) => { d[0].spec.probePath = "/portal/sign-in"; }],
  ["a wrong service port", (d) => { d[0].spec.port = 3000; }],
  ["unbounded memory", (d) => { d[0].spec.memoryLimit = "4Gi"; }],
  ["zero replicas", (d) => { d[0].spec.replicas = 0; }],
  ["one replica under a one-unavailable budget", (d) => { d[0].spec.replicas = 1; }],
  ["missing disclosure", (d) => { d[0].spec.secretProjections = []; }],
  ["an extra private key", (d) => { d[0].spec.secretProjections[0].items.push({ key: "db", path: "db" }); }],
  ["a traversal projection", (d) => { d[0].spec.secretProjections[0].items[0].path = "../disclosure"; }],
  ["an additional Secret projection", (d) => { d[0].spec.secretProjections.push({ secretName: "other", items: [] }); }],
  ["world ingress", (d) => { d[1].spec.ingress[0].fromEntities = ["world"]; }],
  ["extra egress", (d) => { d[1].spec.egress = [{ toEntities: ["world"] }]; }],
  ["an empty egress list without default-deny enforcement", (d) => { d[1].spec.egress = []; }],
  ["an omitted egress policy", (d) => { delete d[1].spec.egress; }],
  ["disabled egress default deny", (d) => { d[1].spec.enableDefaultDeny = { egress: false }; }],
  ["an all-pod selector", (d) => { d[1].spec.endpointSelector = {}; }],
  ["a missing disruption budget", (d) => { d.splice(2, 1); }],
  ["a foreign disruption-budget namespace", (d) => { d[2].metadata.namespace = "foreign"; }],
  ["an empty disruption-budget selector", (d) => { d[2].spec.selector = {}; }],
  ["a budget selecting another workload", (d) => { d[2].spec.selector.matchLabels["app.kubernetes.io/name"] = "other"; }],
  ["a drain-blocking budget", (d) => { d[2].spec.maxUnavailable = 0; }],
  ["a percentage budget", (d) => { d[2].spec.maxUnavailable = "50%"; }],
  ["a conflicting minimum-available budget", (d) => { d[2].spec.minAvailable = 1; }],
]) {
  test(`the actual rendered package rejects ${name}`, () => {
    const rendered = yaml.loadAll(render());
    // Each target is selected by kind, independent of Kustomize's sort order.
    const docs = ["WebApp", "CiliumNetworkPolicy", "PodDisruptionBudget"].map((kind) => rendered.find((d) => d.kind === kind));
    mutate(docs);
    const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
    assert.equal(result.status, 1, result.stdout);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /^tenant package: rejected /);
  });
}

test("a local Kustomize overlay cannot smuggle an unsupported resource into validation", () => {
  const fixture = mkdtempSync(path.join(tmpdir(), "tenant-package-"));
  try {
    cpSync(path.join(root, "deploy"), fixture, { recursive: true });
    writeFileSync(path.join(fixture, "extra.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: extra\n");
    const config = yaml.load(readFileSync(path.join(root, "deploy/kustomization.yaml"), "utf8"));
    config.resources.push("extra.yaml");
    writeFileSync(path.join(fixture, "kustomization.yaml"), yaml.dump(config));
    assert.equal(check(render(fixture)).status, 1);
  } finally {
    rmSync(fixture, { recursive: true });
  }
});

for (const input of ["", "null\n", "apiVersion: v1\napiVersion: v2\n", "---\n", "a".repeat(262145)]) {
  test(`incomplete/ambiguous input is refused (${input.length} bytes)`, () => {
    const result = check(input);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /^tenant package: rejected /);
  });
}

/** The real CI execution must be unconditional and covered by both aggregates. */
function assertCI(workflow) {
  const job = workflow.jobs["test-combined-runtime"];
  assert.equal(job.if, undefined);
  for (const command of [
    "node --test scripts/tenant-package.test.mjs",
    "node scripts/tenant-runtime-smoke.test.mjs business-site:production-test",
  ]) {
    const steps = job.steps.filter((step) => step.run === command);
    assert.equal(steps.length, 1);
    assert.equal(steps[0].if, undefined);
    assert.ok(steps[0]["continue-on-error"] == null || steps[0]["continue-on-error"] === false);
    assert.equal(steps[0]["working-directory"], ".");
  }
  for (const aggregate of ["validation", "status"]) {
    assert.ok(workflow.jobs[aggregate].needs.includes("test-combined-runtime"));
    assert.equal(workflow.jobs[aggregate].if, "always()");
  }
}

test("actual tenant package and image checks cannot silently become optional in CI", () => {
  const workflow = yaml.load(readFileSync(path.join(root, ".github/workflows/ci.yaml"), "utf8"));
  assertCI(workflow);
  for (const field of ["if", "continue-on-error", "working-directory"]) {
    const changed = structuredClone(workflow);
    const step = changed.jobs["test-combined-runtime"].steps.find((s) => s.run?.startsWith("node scripts/tenant-runtime-smoke"));
    step[field] = field === "working-directory" ? "server" : true;
    assert.throws(() => assertCI(changed));
  }
  const missing = structuredClone(workflow);
  missing.jobs["test-combined-runtime"].steps = missing.jobs["test-combined-runtime"].steps.filter((s) => !s.run?.includes("tenant-runtime-smoke"));
  assert.throws(() => assertCI(missing));
});
