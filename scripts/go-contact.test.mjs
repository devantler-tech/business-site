import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

const complete = "--- PASS: TestGoContactAuthority (0.01s)\n--- PASS: TestGoContactHTTP (0.01s)\nPASS\n";

function run(output, status = 0) {
  const dir = mkdtempSync(join(tmpdir(), "go-contact-control-"));
  try {
    writeFileSync(join(dir, "go"), '#!/bin/bash\nprintf "%s\\n" "$@" > "$CONTACT_ARGS"\nprintf "%s" "$CONTACT_OUTPUT"\nexit "$CONTACT_STATUS"\n');
    chmodSync(join(dir, "go"), 0o700);
    const args = join(dir, "args");
    const result = spawnSync("bash", ["scripts/check-go-contact.sh"], {
      env: { ...process.env, PATH: dir + ":" + process.env.PATH, CONTACT_ARGS: args, CONTACT_OUTPUT: output, CONTACT_STATUS: String(status) },
      encoding: "utf8", timeout: 10000,
    });
    assert.ifError(result.error);
    assert.equal(result.signal, null);
    return { ...result, args: existsSync(args) ? readFileSync(args, "utf8") : "" };
  } finally {
    rmSync(dir, { recursive: true });
  }
}

test("the selected integration requires both actual successful Go journeys", () => {
  const result = run(complete);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.equal(result.args, "-C\nserver\ntest\n-v\n-race\n-tags=contactintegration\n./internal/identity\n-run\n^TestGoContact\n-count=1\n-timeout=90s\n");
});

for (const [name, output, status] of [
  ["empty success", "", 0],
  ["no selected tests", "testing: warning: no tests to run\nPASS\n", 0],
  ["missing HTTP journey", "--- PASS: TestGoContactAuthority (0.01s)\nPASS\n", 0],
  ["missing authority journey", "--- PASS: TestGoContactHTTP (0.01s)\nPASS\n", 0],
  ["only a nested lookalike", "    --- PASS: TestGoContactAuthority (0.01s)\n    --- PASS: TestGoContactHTTP (0.01s)\nPASS\n", 0],
  ["plausible complete output before failure", complete, 1],
]) {
  test(`the integration caller rejects ${name}`, () => assert.notEqual(run(output, status).status, 0));
}
