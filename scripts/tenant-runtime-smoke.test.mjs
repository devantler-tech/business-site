// Real restricted image + rendered arguments. No Kubernetes/identity-provider claim.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { request } from "node:http";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { validateTenantPackage } from "./check-tenant-package.mjs";
import { containerGroupDisclosureFixture } from "./runtime-disclosure-fixture.mjs";

const root = fileURLToPath(new URL("../", import.meta.url));
const spec = validateTenantPackage(execFileSync("kubectl", ["kustomize", `${root}/deploy`], {
  encoding: "utf8", timeout: 15000,
}));
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
function get(port, route) {
  return new Promise((resolve, reject) => {
    const req = request({ hostname: "127.0.0.1", port, path: route, timeout: 5000 }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (chunk) => { body += chunk; });
      res.on("end", () => resolve({ status: res.statusCode, body }));
    });
    req.on("error", reject);
    req.on("timeout", () => req.destroy(new Error("owned container request timed out")));
    req.end();
  });
}

test("rendered package arguments fail closed without the required disclosure", () => {
  const result = spawnSync("docker", ["run", "--rm", "--network=none", ...restrictions,
    image, ...spec.args, "--check-serving-public"], { encoding: "utf8", timeout: 30000 });
  assert.ifError(result.error);
  assert.equal(result.status, 1);
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, "company disclosure projection is unavailable\n");
});

test("the real image serves bilingual public routes with exactly the package arguments and portal closed", async () => {
  const fixture = containerGroupDisclosureFixture(image, docker);
  let id;
  try {
    assert.equal(fixture.mount.length, 2);
    assert.match(fixture.mount[1], /,target=\/projection,readonly$/);
    const mount = [fixture.mount[0], fixture.mount[1].replace(",target=/projection,", ",target=/run/webapp-inputs,")];
    const version = JSON.parse(docker("run", "--rm", "--network=none", ...restrictions, image, "--version"));
    assert.equal(version.Repository, "devantler-tech/business-site");
    assert.equal(version.PublicProfile, "production-template");
    assert.equal(version.Mode, "release");
    assert.match(version.SourceRevision, /^[a-f0-9]{40}$/);
    if (process.env.EXPECTED_SOURCE_SHA) assert.equal(version.SourceRevision, process.env.EXPECTED_SOURCE_SHA);
    const receipt = JSON.parse(docker("run", "--rm", "--network=none", ...restrictions, ...mount,
      image, ...spec.args, "--check-serving-public"));
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
    assert.deepEqual(await get(port, spec.probePath), { status: 200, body: "ok\n" });
    for (const route of ["/", "/da/", "/company/", "/da/company/"]) {
      const response = await get(port, route);
      assert.equal(response.status, 200, route);
      assert.doesNotMatch(response.body, /__BUSINESS_ADDRESS_LINE_/);
    }
    for (const route of ["/portal/", "/portal/sign-in"]) assert.equal((await get(port, route)).status, 404, route);
    assert.doesNotMatch(docker("logs", id), /Example|1234 Exampletown/);
  } finally {
    try { if (id) docker("rm", "--force", id); }
    finally { fixture.close(); }
  }
});
