import assert from "node:assert/strict";
import { mkdtemp, writeFile, rm, mkdir, symlink, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import snapshot from "../../src/data/github-stars.json" with { type: "json" };

const root = fileURLToPath(new URL("../../", import.meta.url));
const scratch = await mkdtemp(join(tmpdir(), "svelte-ranking-"));
const fresh = structuredClone(snapshot);
fresh.fetchedAt = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
fresh.observedAt = fresh.fetchedAt.slice(0, 10);
Object.assign(fresh.repositories, {
  "provider-upjet-unifi": 900, "kyverno-policies": 800, khoist: 700,
  "agent-plugins": 600, "agent-skills": 500, ksail: 400, ".github": 300,
  "world-at-ruin": 10000, actions: 9000,
});

/**
 * Run the actual candidate build without replacing its filesystem/ranking boundary.
 * @param {string} filename
 */
function build(filename) {
  const run = spawnSync("npm", ["--prefix", "web", "run", "build"], {
    cwd: root, env: { ...process.env, FEATURE_SVELTE_FRONTEND: "true", PUBLIC_STARS_SNAPSHOT: filename },
    encoding: "utf8", timeout: 45000, maxBuffer: 4 * 1024 * 1024,
  });
  assert.ifError(run.error);
  return run;
}

try {
  const valid = join(scratch, "fresh.json");
  await writeFile(valid, JSON.stringify(fresh));
  const stale = structuredClone(fresh);
  stale.fetchedAt = new Date(Date.now() - 7200000).toISOString().replace(/\.\d{3}Z$/, "Z");
  stale.observedAt = stale.fetchedAt.slice(0, 10);
  const future = structuredClone(fresh);
  future.fetchedAt = new Date(Date.now() + 600000).toISOString().replace(/\.\d{3}Z$/, "Z");
  future.observedAt = future.fetchedAt.slice(0, 10);
  const incomplete = structuredClone(fresh);
  Reflect.deleteProperty(incomplete.repositories, "ksail");
  const missingMetadata = structuredClone(fresh);
  Reflect.deleteProperty(missingMetadata.metadata, "ksail");
  /** @type {[string, string | null][]} */
  const inputs = [
    ["missing.json", null], ["invalid.json", "{"],
    ["stale.json", JSON.stringify(stale)], ["future.json", JSON.stringify(future)],
    ["incomplete.json", JSON.stringify(incomplete)], ["metadata.json", JSON.stringify(missingMetadata)],
  ];
  for (const [name, bytes] of inputs) {
    const path = join(scratch, name);
    if (bytes !== null) await writeFile(path, bytes);
    const run = build(path);
    assert.notEqual(run.status, 0, `invalid snapshot accepted: ${name}`);
    assert.match(run.stdout + run.stderr, /build-local public ranking is incomplete, invalid or stale/, `unexpected failure: ${name}`);
    console.log(`Rejected actual-build ranking input: ${name}`);
  }
  await mkdir(join(scratch, "directory"));
  await symlink(valid, join(scratch, "symlink"));
  for (const name of ["directory", "symlink"]) {
    const run = build(join(scratch, name));
    assert.notEqual(run.status, 0);
    assert.match(run.stdout + run.stderr, /build-local public ranking is incomplete, invalid or stale/);
    console.log(`Rejected actual-build non-regular ranking input: ${name}`);
  }
  const success = build(valid);
  assert.equal(success.status, 0, success.stdout + success.stderr);
  for (const locale of ["", "da/"]) {
    const html = await readFile(resolve(root, "web/build", locale, "projects/index.html"), "utf8");
    const names = [...html.matchAll(/data-public-product="([^"]+)"/g)].map(([, name]) => name);
    assert.deepEqual(names, ["provider-upjet-unifi", "kyverno-policies", "khoist", "agent-plugins", "agent-skills", "ksail"]);
    assert.ok(html.includes(`datetime="${fresh.fetchedAt}"`));
  }
  console.log("Fresh complete build-local snapshot changes the real EN/DA six-tool output; high-star ineligible entries remain excluded.");
} finally {
  await rm(scratch, { recursive: true, force: true });
}
