// Real Docker execution, never a mocked daemon or an extracted-binary substitute.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { cpSync, mkdtempSync, readFileSync, readdirSync, rmSync, appendFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { request } from "node:http";
import path from "node:path";
import test from "node:test";
import { containerDisclosureFixture, containerGroupDisclosureFixture } from "./runtime-disclosure-fixture.mjs";

const [image, root] = process.argv.slice(2);
assert.ok(image && root, "Supply the actual image reference and fresh runtime-dist directory.");
const packages = readdirSync(root).filter((p) => p.startsWith("package."));
assert.equal(packages.length, 1, "Exactly one actual source package is required.");
const directory = path.resolve(root, packages[0]);
const receipt = JSON.parse(readFileSync(path.join(directory, "build-receipt.json")));
assert.equal(receipt.Repository, "devantler-tech/business-site");
assert.equal(receipt.Mode, "release");
assert.match(receipt.SourceRevision, /^[0-9a-f]{40}$/);
assert.match(receipt.PublicDigest, /^[0-9a-f]{64}$/);
if (process.env.EXPECTED_SOURCE_SHA) assert.equal(receipt.SourceRevision, process.env.EXPECTED_SOURCE_SHA);
/** Execute the real Docker CLI with bounded time/output and fail on a nonzero exit. */
const docker = (...args) =>
  execFileSync("docker", args, { encoding: "utf8", timeout: 30000, maxBuffer: 4 * 1024 * 1024 }).trim();
/** Read both log streams of an owned container, refusing unreadable or timed-out logs. */
const logs = (id) => {
  const result = spawnSync("docker", ["logs", id], { encoding: "utf8", timeout: 5000 });
  assert.equal(result.status, 0, "Cannot read the owned container logs.");
  return (result.stdout + result.stderr).trim();
};
const bounded = [
  "--read-only",
  "--cap-drop=ALL",
  "--security-opt=no-new-privileges",
  "--pids-limit=128",
  "--memory=256m",
  "--cpus=1",
];

test("actual non-root image snapshots a root-owned matching-group projection on a read-only filesystem", async () => {
  const fixture = containerGroupDisclosureFixture(image, docker);
  let id;
  try {
    const result = spawnSync(
      "docker",
      [
        "run",
        "--rm",
        "--network=none",
        ...bounded,
        ...fixture.mount,
        image,
        "--public",
        "/app/public",
        ...fixture.arguments,
        "--check-serving-public",
      ],
      { encoding: "utf8", timeout: 30000 },
    );
    assert.ifError(result.error);
    if (receipt.PublicProfile === "production-template") {
      assert.equal(result.status, 0);
      const served = JSON.parse(result.stdout);
      assert.equal(served.PublicDigest, receipt.PublicDigest);
      assert.match(served.EffectivePublicDigest, /^[0-9a-f]{64}$/);
      assert.notEqual(served.EffectivePublicDigest, receipt.PublicDigest);
      assert.equal(result.stderr, "");
      assert.doesNotMatch(result.stdout, /Example|\/projection/);
      id = docker(
        "run",
        "--detach",
        ...bounded,
        ...fixture.mount,
        "--publish",
        "127.0.0.1::8080",
        image,
        ...runtimeArguments(fixture),
      );
      await ready(id);
      const inspected = JSON.parse(docker("inspect", id))[0];
      assert.equal(inspected.HostConfig.ReadonlyRootfs, true);
      const port = inspected.NetworkSettings.Ports["8080/tcp"][0].HostPort;
      for (const route of ["/company/", "/da/company/"]) {
        const response = await get(port, route);
        assert.equal(response.status, 200);
        assert.match(response.body, /Example &amp; Partners Road 1/);
        assert.match(response.body, /1234 Exampletown/);
        assert.doesNotMatch(response.body, /__BUSINESS_ADDRESS_LINE_/);
      }
      assert.equal((await get(port, "/portal")).status, 404);
      assert.doesNotMatch(logs(id), /Example|1234 Exampletown|\/projection/);
    } else {
      assert.equal(result.status, 1);
      assert.equal(result.stdout, "");
      assert.equal(result.stderr, "company disclosure projection is unavailable\n");
    }
    const ordinary = spawnSync(
      "docker",
      [
        "run",
        "--rm",
        "--network=none",
        ...bounded,
        ...fixture.mount,
        image,
        "--public",
        "/app/public",
        "--company-disclosure-file",
        "/projection/disclosure",
        "--check-serving-public",
      ],
      { encoding: "utf8", timeout: 30000 },
    );
    assert.ifError(ordinary.error);
    assert.equal(ordinary.status, 1, "ordinary owner-only input must not accept group-readable files");
    assert.equal(ordinary.stdout, "");
    assert.doesNotMatch(ordinary.stderr, /Example|\/projection/);
  } finally {
    try {
      if (id) docker("rm", "--force", id);
    } finally {
      fixture.close();
    }
  }
});

for (const [scenario, owner, group, mode] of [
  ["foreign owner", 65533, 65532, 0o440],
  ["foreign group", 65532, 65533, 0o440],
  ["world read", 0, 65532, 0o444],
  ["group write", 0, 65532, 0o460],
  ["executable", 0, 65532, 0o540],
]) {
  test(`actual restricted image refuses ${scenario} company projection`, () => {
    const fixture = containerGroupDisclosureFixture(image, docker, owner, group, mode);
    try {
      const result = spawnSync(
        "docker",
        [
          "run",
          "--rm",
          "--network=none",
          ...bounded,
          ...fixture.mount,
          image,
          "--public",
          "/app/public",
          ...fixture.arguments,
          "--check-serving-public",
        ],
        { encoding: "utf8", timeout: 30000 },
      );
      assert.ifError(result.error);
      assert.equal(result.status, 1);
      assert.equal(result.stdout, "");
      assert.equal(result.stderr, "company disclosure projection is unavailable\n");
    } finally {
      fixture.close();
    }
  });
}

test("restricted image exposes only an explicitly configured migration job", () => {
  const result = spawnSync(
    "docker",
    [
      "run",
      "--rm",
      "--network=none",
      ...bounded,
      image,
      "--migrate",
      "--migration-database-file",
      "/missing/private-connection",
    ],
    { encoding: "utf8", timeout: 30000 },
  );
  assert.ifError(result.error);
  assert.equal(result.status, 1);
  assert.equal(result.stdout, "");
  assert.equal(result.stderr, "portal schema migration failed\n");
});

/** Preserve ordinary container controls while projecting only fictional input. */
function projection() {
  return receipt.PublicProfile === "production-template"
    ? containerDisclosureFixture(image, docker)
    : { mount: [], arguments: [], close() {} };
}

/** Keep explicit public/listener arguments when replacing the image's CMD. */
function runtimeArguments(fixture) {
  return ["--public", "/app/public", "--listen", "0.0.0.0:8080", ...fixture.arguments];
}

/** Preserve raw traversal/encoding inputs: fetch() would normalize some away. */
function get(port, route, method = "GET") {
  return new Promise((resolve, reject) => {
    const req = request({ hostname: "127.0.0.1", port, path: route, method, timeout: 5000 }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (b) => {
        body += b;
      });
      res.on("end", () => resolve({ status: res.statusCode, headers: res.headers, body }));
    });
    req.on("error", reject);
    req.on("timeout", () => req.destroy(new Error("Container request timed out.")));
    req.end();
  });
}

/** Wait only for this owned process, bounded at ten seconds. */
async function ready(id) {
  for (let i = 0; i < 50; i++) {
    const state = JSON.parse(docker("inspect", id))[0].State;
    assert.ok(state.Running, "Actual container exited before readiness.");
    if (logs(id).includes("runtime ready;")) return;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  assert.fail("Actual container did not become ready in ten seconds.");
}

test("actual image receipt, restricted runtime, visitor bytes and private/traversal boundaries", async () => {
  const config = JSON.parse(docker("image", "inspect", image))[0].Config;
  assert.equal(config.User, "65532:65532");
  assert.equal(config.WorkingDir, "/app");
  assert.deepEqual(config.Entrypoint, ["/app/business-site"]);
  assert.deepEqual(config.Cmd, ["--public", "/app/public", "--listen", "0.0.0.0:8080"]);
  assert.equal(config.Labels["org.opencontainers.image.revision"], receipt.SourceRevision);
  assert.ok(!(config.Env ?? []).some((e) => e.startsWith("PORTAL_")));
  assert.deepEqual(JSON.parse(docker("run", "--rm", "--network=none", ...bounded, image, "--version")), receipt);
  const fixture = projection();
  let id;
  try {
    const served = JSON.parse(
      docker(
        "run",
        "--rm",
        "--network=none",
        ...bounded,
        ...fixture.mount,
        image,
        "--public",
        "/app/public",
        ...fixture.arguments,
        "--check-serving-public",
      ),
    );
    assert.equal(served.PublicDigest, receipt.PublicDigest);
    assert.match(served.EffectivePublicDigest, /^[0-9a-f]{64}$/);
    assert.equal(served.EffectivePublicDigest === receipt.PublicDigest, receipt.PublicProfile === "default");
    id = docker(
      "run",
      "--detach",
      ...bounded,
      ...fixture.mount,
      "--publish",
      "127.0.0.1::8080",
      image,
      ...runtimeArguments(fixture),
    );
    await ready(id);
    const inspected = JSON.parse(docker("inspect", id))[0];
    assert.equal(inspected.HostConfig.ReadonlyRootfs, true);
    const port = inspected.NetworkSettings.Ports["8080/tcp"][0].HostPort;
    for (const route of ["/", "/da/", "/projects/", "/da/projects/", "/blog/"]) {
      const response = await get(port, route);
      assert.equal(response.status, 200, route);
      assert.equal(response.body, readFileSync(path.join(directory, "public", route, "index.html"), "utf8"), route);
      assert.equal(response.headers["x-content-type-options"], "nosniff");
      const head = await get(port, route, "HEAD");
      assert.equal(head.status, 200);
      assert.equal(head.body, "");
    }
    if (receipt.PublicProfile === "production-template") {
      for (const route of ["/company/", "/da/company/"]) {
        const response = await get(port, route);
        assert.equal(response.status, 200);
        assert.match(response.body, /Example &amp; Partners Road 1/);
        assert.match(response.body, /1234 Exampletown/);
        assert.match(response.body, /46830385/);
        assert.doesNotMatch(response.body, /__BUSINESS_ADDRESS_LINE_/);
      }
    }
    for (const route of [
      "/portal",
      "/portal/sign-in",
      "/portal/operator",
      "/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals",
      "/server/go.mod",
      "/MIGRATIONS.md",
      "/server/MIGRATIONS.md",
      "/testdata/provision.sql",
      "/.env",
      "/.git/config",
      "/%70ortal/sign-in",
      "/portal%2Fsign-in",
      "/assets/../portal/sign-in",
      "/assets/%2e%2e/.env",
      "/%252e%252e/.env",
      "/portal\\sign-in",
    ]) {
      const response = await get(port, route);
      assert.equal(response.status, 404, route);
      assert.equal(response.headers.location, undefined);
      assert.equal(response.headers["cache-control"], "no-store");
      assert.doesNotMatch(response.body, /postgres|secret|token|\/Users\//);
    }
    assert.equal((await get(port, "/", "POST")).status, 405);
    assert.deepEqual(await get(port, "/healthz", "HEAD").then((r) => [r.status, r.body]), [200, ""]);
    assert.equal((await get(port, "/healthz")).body, "ok\n");
    assert.match(logs(id), /^runtime ready; listen=(?:0\.0\.0\.0|\[::\]):8080; private routes remain independently gated$/);
    const started = Date.now();
    docker("stop", "--time=12", id);
    assert.ok(Date.now() - started < 13000, "SIGTERM must drain within the bound.");
    assert.equal(JSON.parse(docker("inspect", id))[0].State.ExitCode, 0);
  } finally {
    try {
      if (id) docker("rm", "--force", id);
    } finally {
      fixture.close();
    }
  }
});

test("actual container starts without network or portal dependencies", async () => {
  const fixture = projection();
  let id;
  try {
    id = docker("run", "--detach", "--network=none", ...bounded, ...fixture.mount, image, ...runtimeArguments(fixture));
    await ready(id);
    assert.match(logs(id), /^runtime ready; listen=(?:0\.0\.0\.0|\[::\]):8080; private routes remain independently gated$/);
  } finally {
    try {
      if (id) docker("stop", "--time=12", id);
    } finally {
      try {
        if (id) docker("rm", "--force", id);
      } finally {
        fixture.close();
      }
    }
  }
});

test("production image refuses absent disclosure before listening", () => {
  if (receipt.PublicProfile !== "production-template") return;
  const result = spawnSync("docker", ["run", "--rm", "--network=none", ...bounded, image], {
    encoding: "utf8",
    timeout: 30000,
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /company disclosure is unavailable/);
  assert.doesNotMatch(result.stdout + result.stderr, /runtime ready|Example|\/projection/);
});

test("actual container refuses modified public bytes before readiness", async () => {
  const temporary = mkdtempSync(path.join(tmpdir(), "business-container-tamper-"));
  let id;
  try {
    cpSync(path.join(directory, "public"), path.join(temporary, "public"), { recursive: true });
    appendFileSync(path.join(temporary, "public/index.html"), "\n<!-- tampered fixture -->");
    id = docker(
      "create",
      "--network=none",
      ...bounded,
      "--mount",
      `type=bind,source=${temporary}/public,target=/app/public,readonly`,
      image,
    );
    docker("start", id);
    assert.equal(docker("wait", id), "1");
    assert.equal(logs(id), "public artifact does not match this runtime package");
  } finally {
    if (id) docker("rm", "--force", id);
    rmSync(temporary, { recursive: true });
  }
});
