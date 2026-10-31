import assert from "node:assert/strict";
import { closeSync, constants, fstatSync, openSync, readSync } from "node:fs";
import { pathToFileURL } from "node:url";

const workFamily = /^TestWork(Approval|Review)/;
const privateFamily = /^Test(Private|Operator|Proposal|Sharing|Consent|Acceptance|Invoice|Credit)/;
const testName = /^(Test|Example|Fuzz)\S*$/;

/** Read complete go test JSON, retaining package completion and top-level test events. */
function parse(text, listing = false) {
  assert.ok(text.length > 0 && text.length <= 16 * 1024 * 1024, "bounded nonempty test output required");
  const packages = new Map();
  for (const line of text.trimEnd().split("\n")) {
    const e = JSON.parse(line);
    assert.ok(
      e && typeof e.Package === "string" && e.Package.length > 0 && typeof e.Action === "string",
      "invalid test event",
    );
    assert.ok(!["fail", "build-fail"].includes(e.Action), "a failed package/test is not evidence");
    if (!packages.has(e.Package))
      packages.set(e.Package, { started: 0, finished: 0, skipped: false, tests: new Map() });
    const p = packages.get(e.Package);
    if (listing && e.Action === "output" && typeof e.Output === "string") {
      const name = e.Output.trimEnd();
      if (testName.test(name)) {
        assert.ok(!name.includes("/") && !p.tests.has(name), "duplicate or nested inventory name");
        p.tests.set(name, { run: 0, pass: 0 });
      }
    }
    if (e.Test !== undefined) {
      assert.equal(listing, false, "listing unexpectedly executed a test");
      assert.ok(typeof e.Test === "string" && testName.test(e.Test), "invalid executed test name");
      assert.notEqual(e.Action, "skip", "a skipped test is not examined input");
      if (e.Test.includes("/")) continue; // Parent execution includes all its subtests.
      if (!p.tests.has(e.Test)) p.tests.set(e.Test, { run: 0, pass: 0 });
      if (e.Action === "run" || e.Action === "pass") p.tests.get(e.Test)[e.Action]++;
    } else if (e.Action === "start") p.started++;
    else if (e.Action === "pass" || e.Action === "skip") {
      p.finished++;
      p.skipped = e.Action === "skip";
    }
  }
  assert.ok(packages.size > 0, "complete package inventory required");
  for (const p of packages.values()) {
    assert.equal(p.started, 1, "package must start once");
    assert.equal(p.finished, 1, "package must finish once");
    if (p.skipped) assert.equal(p.tests.size, 0, "only packages without tests may be skipped");
  }
  return packages;
}

/** Verify all real complementary executions against the current compiled module inventory. */
export function checkCohorts(outputs, coverage) {
  assert.equal(outputs.length, 4);
  assert.equal(coverage.length, 3, "retain all three distinct coverage profiles");
  for (const profile of coverage) {
    assert.match(
      profile,
      /^mode: atomic\n(?:[^\n]+:\d+\.\d+,\d+\.\d+ \d+ \d+\n)+$/,
      "complete race/atomic coverage profile required",
    );
  }
  const inventory = parse(outputs[0], true);
  const results = outputs.slice(1).map((output) => parse(output));
  const counts = [0, 0, 0];
  for (const [cohort, result] of results.entries()) {
    assert.deepEqual([...result.keys()].sort(), [...inventory.keys()].sort(), "every module package must be examined");
    for (const [name, p] of inventory) {
      const actual = result.get(name);
      const selected = [...p.tests.keys()].filter((test) => {
        const family = workFamily.test(test) ? 1 : privateFamily.test(test) ? 2 : 0;
        return family === cohort;
      });
      assert.deepEqual(
        [...actual.tests.keys()].sort(),
        selected.sort(),
        "test cohort must exactly match compiled inventory",
      );
      if (actual.skipped) assert.ok(p.skipped, "a package with compiled tests cannot be skipped");
      for (const state of actual.tests.values())
        assert.deepEqual(state, { run: 1, pass: 1 }, "each selected test must run and pass exactly once");
      counts[cohort] += selected.length;
    }
  }
  assert.ok(
    counts.every((n) => n > 0),
    "all three ordinary families must be nonempty",
  );
  return {
    packages: inventory.size,
    tests: counts[0] + counts[1] + counts[2],
    core: counts[0],
    work: counts[1],
    private: counts[2],
  };
}

/** Read bounded regular-file evidence for the command-line verifier. */
export function readEvidence(path) {
  const limit = 16 * 1024 * 1024;
  assert.ok(
    Number.isInteger(constants.O_NOFOLLOW) && Number.isInteger(constants.O_NONBLOCK),
    "safe evidence-open flags required",
  );
  // Open first without following a final symlink or waiting on a FIFO. Check and
  // read this same descriptor; a later pathname replacement cannot change it.
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try {
    const info = fstatSync(fd);
    assert.ok(info.isFile() && info.size > 0 && info.size <= limit, "bounded regular test evidence required");
    const bytes = Buffer.alloc(limit + 1);
    let used = 0;
    while (used < bytes.length) {
      const count = readSync(fd, bytes, used, bytes.length - used, null);
      if (count === 0) break;
      used += count;
    }
    assert.ok(used > 0 && used <= limit, "test evidence exceeded its byte bound");
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, used));
  } finally {
    closeSync(fd);
  }
}

/** Produce a public failure diagnostic without reflecting evidence or file paths. */
export function failureDiagnostic(error) {
  // Node appends input diffs even to explicitly supplied AssertionError messages.
  // Admit only a closed fixed first line; never print the complete message.
  const candidate = error instanceof assert.AssertionError ? error.message.split("\n", 1)[0] : undefined;
  const reason = [
    "bounded nonempty test output required",
    "invalid test event",
    "a failed package/test is not evidence",
    "duplicate or nested inventory name",
    "listing unexpectedly executed a test",
    "invalid executed test name",
    "a skipped test is not examined input",
    "complete package inventory required",
    "package must start once",
    "package must finish once",
    "only packages without tests may be skipped",
    "retain all three distinct coverage profiles",
    "complete race/atomic coverage profile required",
    "every module package must be examined",
    "test cohort must exactly match compiled inventory",
    "a package with compiled tests cannot be skipped",
    "each selected test must run and pass exactly once",
    "all three ordinary families must be nonempty",
    "safe evidence-open flags required",
    "bounded regular test evidence required",
    "test evidence exceeded its byte bound",
  ].find((fixed) => fixed === candidate);
  const prefix = "Ordinary Go cohort evidence is incomplete or failed";
  return reason ? `${prefix}: ${reason}` : `${prefix}.`;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const result = checkCohorts(
      ["test-inventory.jsonl", "test-core.jsonl", "test-work.jsonl", "test-private.jsonl"].map(readEvidence),
      ["coverage-core.out", "coverage-work.out", "coverage-private.out"].map(readEvidence),
    );
    console.log(
      `Complete ordinary Go suite: ${result.tests} tests in ${result.packages} packages; core=${result.core}, work=${result.work}, private=${result.private}; separate atomic coverage profiles retained.`,
    );
  } catch (error) {
    console.error(failureDiagnostic(error));
    process.exitCode = 1;
  }
}
