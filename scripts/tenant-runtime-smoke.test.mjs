// Real restricted image + rendered arguments. No Kubernetes/identity-provider claim.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { request } from "node:http";
import { fileURLToPath } from "node:url";
import test from "node:test";
import * as yaml from "js-yaml";
import { validateTenantPackage } from "./check-tenant-package.mjs";
import { containerGroupDisclosureFixture, runtimeSecretFixture } from "./runtime-disclosure-fixture.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const rendered = execFileSync("kubectl", ["kustomize", `${root}/deploy`], {
  encoding: "utf8", timeout: 15000,
});
const spec = validateTenantPackage(rendered);
// CI substitutes its newly built production image, just as the later verified
// publisher pins a new digest. With no argument, exercise the committed digest.
const image = process.argv[2] ?? spec.image;
const docker = (...args) => execFileSync("docker", args, {
  encoding: "utf8", timeout: 30000, maxBuffer: 1024 * 1024,
}).trim();
const restrictions = ["--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges",
  "--pids-limit=128", `--memory=${parseInt(spec.memoryLimit, 10)}m`,
  `--memory-reservation=${parseInt(spec.memoryRequest, 10)}m`,
  `--cpus=${parseInt(spec.cpuLimit, 10) / 1000}`, `--user=${spec.runAsUser}:${spec.runAsGroup}`];

/** Keep requests on the isolated loopback publication of this owned container. */
function get(port, route, host) {
  return new Promise((resolve, reject) => {
    const headers = host ? { Host: host } : {};
    const req = request({ hostname: "127.0.0.1", port, path: route, headers, timeout: 5000 }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (chunk) => { body += chunk; });
      res.on("end", () => resolve({ status: res.statusCode, body, location: res.headers.location }));
    });
    req.on("error", reject);
    req.on("timeout", () => req.destroy(new Error("owned container request timed out")));
    req.end();
  });
}

// The public receipt check reports the public snapshot only. Keep the full
// package arguments for serving, and omit each portal input pair for the receipt.
const portalFlags = new Set([
  "--portal-settings-projection",
  "--portal-upstream-logout-url",
  "--portal-upstream-logout-client-id",
]);
const publicArgs = spec.args.filter((argument, index) => !portalFlags.has(argument) && !portalFlags.has(spec.args[index - 1]));

/** Mount one synthetic projection at the package's fixed input directory. */
const atInputs = (fixture) => [fixture.mount[0], fixture.mount[1].replace(",target=/projection,", ",target=/run/webapp-inputs,")];

test("rendered package arguments fail closed without the required disclosure", () => {
  for (const args of [spec.args, [...publicArgs, "--check-serving-public"]]) {
    const result = spawnSync("docker", ["run", "--rm", "--network=none", ...restrictions,
      image, ...args], { encoding: "utf8", timeout: 30000 });
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.equal(result.stderr, "company disclosure projection is unavailable\n");
  }
});

// A missing Secret exercises the argument parser before any database access.
test("the image accepts both migration jobs and the retention job arguments", () => {
  const docs = yaml.loadAll(rendered);
  const migrations = docs.filter((d) => d?.kind === "Job")
    .map((job) => [job.spec.template.spec.containers[0].args, "portal schema migration failed\n"]);
  const cron = docs.find((d) => d?.kind === "CronJob").spec.jobTemplate.spec.template.spec.containers[0].args;
  for (const [args, failure] of [...migrations, [cron, "portal retention purge failed\n"]]) {
    const result = spawnSync("docker", ["run", "--rm", "--network=none", ...restrictions, image, ...args], { encoding: "utf8", timeout: 30000 });
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.equal(result.stdout + result.stderr, failure, args.join(" "));
  }
});

// Serving needs the platform's whole runtime Secret, so it must exist before this package.
test("rendered package arguments fail closed without the portal settings item", () => {
  const fixture = containerGroupDisclosureFixture(image, docker);
  try {
    const result = spawnSync("docker", ["run", "--rm", "--network=none", ...restrictions,
      ...atInputs(fixture), image, ...spec.args], { encoding: "utf8", timeout: 30000 });
    assert.ifError(result.error);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.equal(result.stderr, "portal settings projection is unavailable\n");
  } finally {
    fixture.close();
  }
});

test("the real image serves both hosts with exactly the package arguments and the portal closed", async () => {
  const fixture = containerGroupDisclosureFixture(image, docker, 0, 65532, 0o440, runtimeSecretFixture);
  let id;
  try {
    assert.equal(fixture.mount.length, 2);
    assert.match(fixture.mount[1], /,target=\/projection,readonly$/);
    const mount = atInputs(fixture);
    const version = JSON.parse(docker("run", "--rm", "--network=none", ...restrictions, image, "--version"));
    assert.equal(version.Repository, "devantler-tech/business-site");
    assert.equal(version.PublicProfile, "production-template");
    assert.equal(version.Mode, "release");
    assert.match(version.SourceRevision, /^[a-f0-9]{40}$/);
    if (process.env.EXPECTED_SOURCE_SHA) assert.equal(version.SourceRevision, process.env.EXPECTED_SOURCE_SHA);
    const receipt = JSON.parse(docker("run", "--rm", "--network=none", ...restrictions, ...mount,
      image, ...publicArgs, "--check-serving-public"));
    assert.equal(receipt.SourceRevision, version.SourceRevision);
    assert.equal(receipt.PublicDigest, version.PublicDigest);
    assert.notEqual(receipt.EffectivePublicDigest, receipt.PublicDigest);
    id = docker("run", "--detach", ...restrictions, ...mount, "--publish", "127.0.0.1::8080", image, ...spec.args);
    let ready = false;
    for (let attempt = 0; attempt < 50; attempt++) {
      const state = JSON.parse(docker("inspect", id))[0].State;
      assert.equal(state.Running, true);
      if (docker("logs", id).includes("runtime ready;")) { ready = true; break; }
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    assert.equal(ready, true, "owned runtime readiness deadline");
    const container = JSON.parse(docker("inspect", id))[0];
    assert.deepEqual(container.Config.Cmd, spec.args);
    assert.equal(container.Config.User, "65532:65532");
    assert.equal(container.HostConfig.ReadonlyRootfs, true);
    assert.equal(container.HostConfig.Memory, 256 * 1024 * 1024);
    assert.equal(container.HostConfig.NanoCpus, 500000000);
    assert.ok(!(container.Config.Env ?? []).some((value) => value.startsWith("PORTAL_")));
    const port = container.NetworkSettings.Ports["8080/tcp"][0].HostPort;
    const probe = await get(port, spec.probePath);
    assert.deepEqual([probe.status, probe.body], [200, "ok\n"]);
    // devantler.tech serves the public pages; the portal host only redirects them.
    for (const route of ["/", "/da/", "/company/", "/da/company/"]) {
      const response = await get(port, route, "devantler.tech");
      assert.equal(response.status, 200, route);
      assert.doesNotMatch(response.body, /__BUSINESS_ADDRESS_LINE_/);
      if (route === "/") continue;
      const away = await get(port, route, "portal.devantler.tech");
      assert.deepEqual([away.status, away.location], [302, `https://devantler.tech${route}`], route);
    }
    // The portal lives at the root of its host; /portal… addresses move there without the prefix.
    const toPortal = await get(port, "/portal/sign-in", "devantler.tech");
    assert.deepEqual([toPortal.status, toPortal.location], [302, "https://portal.devantler.tech/sign-in"]);
    for (const [route, location] of [["/portal/", "/"], ["/portal/sign-in", "/sign-in"]]) {
      const old = await get(port, route, "portal.devantler.tech");
      assert.deepEqual([old.status, old.location], [301, location], route);
    }
    // The portal stays off in the package: its root addresses are closed.
    for (const route of ["/", "/sign-in"]) assert.equal((await get(port, route, "portal.devantler.tech")).status, 404, route);
    // Each page keeps the one address GitHub Pages gave it, and a miss shows the built 404 page (#583).
    const www = await get(port, "/about/", "www.devantler.tech");
    assert.deepEqual([www.status, www.location], [301, "https://devantler.tech/about/"]);
    const folder = await get(port, "/about", "devantler.tech");
    assert.deepEqual([folder.status, folder.location], [301, "/about/"]);
    const missing = await get(port, "/this-page-does-not-exist/", "devantler.tech");
    assert.equal(missing.status, 404);
    assert.match(missing.body, /<title>Page not found \| Devantler Tech<\/title>/);
    assert.doesNotMatch(docker("logs", id), /Example|1234 Exampletown/);
  } finally {
    try { if (id) docker("rm", "--force", id); }
    finally { fixture.close(); }
  }
});
