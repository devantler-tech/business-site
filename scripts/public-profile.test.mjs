import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import snapshot from "../src/data/github-stars.json" with { type: "json" };

test("actual public catalogue uses a complete fresh build-local ranking without modifying source", () => {
  const directory = mkdtempSync(path.join(tmpdir(), "public-profile-"));
  try {
    const filename = path.join(directory, "stars.json");
    const fresh = structuredClone(snapshot);
    fresh.fetchedAt = new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
    fresh.observedAt = fresh.fetchedAt.slice(0, 10);
    fresh.repositories.ksail = 123456;
    writeFileSync(filename, JSON.stringify(fresh));
    const program = `import { publicProducts } from './src/data/public-products.ts'; console.log(publicProducts.find(p => p.repository === 'ksail').stars);`;
    const env = { ...process.env, PUBLIC_STARS_SNAPSHOT: filename };
    assert.equal(
      execFileSync(process.execPath, ["--input-type=module", "-e", program], {
        env,
        encoding: "utf8",
        timeout: 10000,
      }).trim(),
      "123456",
    );
    for (const mutate of [
      (s) => {
        delete s.repositories.ksail;
      },
      (s) => {
        s.repositories.ksail = -1;
      },
      (s) => {
        delete s.metadata.ksail;
      },
      (s) => {
        s.fetchedAt = "2020-01-01T00:00:00Z";
        s.observedAt = "2020-01-01";
      },
      (s) => {
        s.fetchedAt = "2099-01-01T00:00:00Z";
        s.observedAt = "2099-01-01";
      },
    ]) {
      const invalid = structuredClone(fresh);
      mutate(invalid);
      writeFileSync(filename, JSON.stringify(invalid));
      assert.notEqual(
        spawnSync(process.execPath, ["--input-type=module", "-e", program], { env, timeout: 10000 }).status,
        0,
      );
    }
    writeFileSync(filename, "not-json");
    assert.notEqual(
      spawnSync(process.execPath, ["--input-type=module", "-e", program], { env, timeout: 10000 }).status,
      0,
    );
  } finally {
    rmSync(directory, { recursive: true });
  }
});
