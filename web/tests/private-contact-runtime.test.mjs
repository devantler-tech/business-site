import assert from "node:assert/strict";
import { after, before, test } from "node:test";
import { spawnSync } from "node:child_process";
import { rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { buildContactRenderer } from "../private-contact/build.mjs";

/** @type {Awaited<ReturnType<typeof buildContactRenderer>>} */
let artifact;
before(async () => {
  artifact = await buildContactRenderer();
});
after(() => {
  if (artifact) rmSync(artifact.directory, { recursive: true });
});

function frame(/** @type {unknown} */ value) {
  const payload = Buffer.from(JSON.stringify(value));
  const prefix = Buffer.alloc(4);
  prefix.writeUInt32BE(payload.length);
  return Buffer.concat([prefix, payload]);
}

test("the actual Node bundle refuses truncated, oversized and malformed pipe messages without private diagnostics", () => {
  const tooLarge = Buffer.alloc(4);
  tooLarge.writeUInt32BE(8189);
  for (const input of [
    Buffer.from([0, 0]),
    Buffer.from([0, 0, 0, 0]),
    Buffer.from([0, 0, 0, 2, 255, 255]),
    tooLarge,
    Buffer.from([0, 0, 0, 2, 123]),
    frame({ version: 2, id: "0".repeat(32), contact: {} }),
    frame({ version: 1, id: "private-selector", contact: {} }),
    frame({ version: 1, id: "0".repeat(32), contact: {}, extra: "fictional-private-value" }),
  ]) {
    const child = spawnSync(process.execPath, [artifact.worker], {
      input,
      timeout: 1500,
      maxBuffer: 128 * 1024,
      env: { LC_ALL: "C" },
    });
    assert.equal(child.status, 1, String(child.stderr));
    assert.equal(child.stdout.length, 0);
    assert.equal(child.stderr.length, 0);
  }
});

test("the Go supervisor renders the actual compiled component in a separate process", { timeout: 60000 }, () => {
  const child = spawnSync(
    "go",
    [
      "test",
      "-race",
      "-tags=rendererintegration",
      "./internal/contactrenderer",
      "-run",
      "^TestCompiledComponent$",
      "-count=1",
      "-timeout=30s",
    ],
    {
      cwd: fileURLToPath(new URL("../../server/", import.meta.url)),
      env: {
        ...process.env,
        GOTOOLCHAIN: "local",
        LC_ALL: "C",
        RENDERER_TEST_NODE: process.execPath,
        RENDERER_TEST_ARTIFACT: artifact.worker,
      },
      encoding: "utf8",
      timeout: 60000,
      maxBuffer: 256 * 1024,
    },
  );
  assert.equal(child.status, 0, child.stdout + child.stderr);
  assert.match(child.stdout, /ok\s+github.com\/devantler-tech\/business-site\/server\/internal\/contactrenderer/u);
});
