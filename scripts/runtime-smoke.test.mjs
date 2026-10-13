// Exercise the actual packaged executable. No broker, database or customer data.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { createServer } from "node:net";
import path from "node:path";
import test from "node:test";
import { once } from "node:events";
import { disclosureFixture, privateDisclosureFixture } from "./runtime-disclosure-fixture.mjs";

const root = process.argv[2];
if (!root) throw new Error("Supply runtime-dist containing the actual built package.");
const packages = readdirSync(root).filter((p) => p.startsWith("package."));
assert.equal(packages.length, 1, "Use a fresh output directory: exactly one actual package is required.");
const directory = path.resolve(root, packages[0]);
const binary = path.join(directory, "business-site");

test("packaged source identity, EN/DA public paths and reserved private routes", async () => {
  const identity = JSON.parse(execFileSync(binary, ["--version"], { encoding: "utf8" }));
  const receipt = JSON.parse(readFileSync(path.join(directory, "build-receipt.json")));
  assert.deepEqual(identity, receipt);
  assert.equal(identity.Repository, "devantler-tech/business-site");
  assert.match(identity.SourceRevision, /^[0-9a-f]{40}$/);
  assert.match(identity.PublicDigest, /^[0-9a-f]{64}$/);
  assert.ok(["release", "development"].includes(identity.Mode));
  assert.ok(["default", "production-template"].includes(identity.PublicProfile));
  const projection = identity.PublicProfile === "production-template" ? privateDisclosureFixture() : undefined;
  try {
    const disclosureArgs = projection ? ["--company-disclosure-file", projection.filename] : [];
    const served = JSON.parse(
      execFileSync(binary, ["--public", path.join(directory, "public"), ...disclosureArgs, "--check-serving-public"], {
        encoding: "utf8",
      }),
    );
    assert.equal(served.PublicDigest, identity.PublicDigest);
    assert.match(served.EffectivePublicDigest, /^[0-9a-f]{64}$/);
    assert.equal(served.EffectivePublicDigest === identity.PublicDigest, !projection);
    const address = createServer();
    address.listen(0, "127.0.0.1");
    await once(address, "listening");
    const port = address.address().port;
    await new Promise((resolve) => address.close(resolve));
    const env = { ...process.env };
    for (const name of Object.keys(env)) if (name.startsWith("PORTAL_")) delete env[name];
    const child = spawn(
      binary,
      ["--public", path.join(directory, "public"), ...disclosureArgs, "--listen", `127.0.0.1:${port}`],
      {
        env,
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let logs = "";
    child.stdout.on("data", (b) => {
      logs += b;
    });
    child.stderr.on("data", (b) => {
      logs += b;
    });
    const exited = once(child, "exit");
    try {
      await new Promise((resolve, reject) => {
        const timeout = setTimeout(() => reject(new Error("Actual package did not become ready.")), 10000);
        child.once("error", (e) => {
          clearTimeout(timeout);
          reject(e);
        });
        child.stdout.on("data", () => {
          if (logs.includes("runtime ready;")) {
            clearTimeout(timeout);
            resolve();
          }
        });
        child.once("exit", () => {
          clearTimeout(timeout);
          reject(new Error("Actual package exited before readiness."));
        });
      });
      const origin = `http://127.0.0.1:${port}`;
      for (const route of ["/", "/da/", "/projects/", "/blog/"]) {
        const response = await fetch(origin + route, { redirect: "manual" });
        assert.equal(response.status, 200, route);
        assert.match(await response.text(), /<html/);
        assert.equal(response.headers.get("x-content-type-options"), "nosniff");
        const head = await fetch(origin + route, { method: "HEAD" });
        assert.equal(head.status, 200);
        assert.equal(await head.text(), "");
      }
      if (projection) {
        for (const route of ["/company/", "/da/company/"]) {
          const response = await fetch(origin + route, { redirect: "manual" });
          assert.equal(response.status, 200);
          const body = await response.text();
          assert.ok(body.includes("46830385") && body.includes("ned@devantler.tech"));
          assert.ok(body.includes("Example &amp; Partners Road 1") && body.includes("1234 Exampletown"));
          assert.doesNotMatch(body, /__BUSINESS_ADDRESS_LINE_/);
        }
        for (const route of ["/", "/da/", "/projects/", "/da/projects/"])
          assert.ok(!(await (await fetch(origin + route)).text()).includes(disclosureFixture.split("\n")[1]));
      }
      for (const route of [
        "/portal",
        "/portal/sign-in",
        "/portal/operator",
        "/portal/operator/requests/ABCDEFGHIJKLMNOPQRSTUVWXYZ/proposals",
        "/server/go.mod",
        "/.env",
        "/does-not-exist",
        "/%70ortal/sign-in",
        "/portal%2Fsign-in",
      ]) {
        const response = await fetch(origin + route, { redirect: "manual" });
        assert.equal(response.status, 404, route);
        assert.equal(response.headers.get("location"), null);
        assert.equal(response.headers.get("cache-control"), "no-store");
        assert.doesNotMatch(await response.text(), /postgres|secret|token|\/Users/);
      }
      assert.equal((await fetch(origin, { method: "POST" })).status, 405);
      assert.equal(await (await fetch(origin + "/healthz")).text(), "ok\n");
      assert.equal(logs.trim(), "runtime ready; private routes remain independently gated");
    } finally {
      child.kill("SIGTERM");
      const timer = setTimeout(() => child.kill("SIGKILL"), 12000);
      const [code, signal] = await exited;
      clearTimeout(timer);
      assert.equal(code, 0, `package shutdown code=${code} signal=${signal}`);
    }
  } finally {
    projection?.close();
  }
});
