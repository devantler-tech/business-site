import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import fs from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { load } from "js-yaml";
import { checkCohorts, readEvidence, failureDiagnostic } from "./identity-cohorts.mjs";

const workflow = load(readFileSync(".github/workflows/ci.yaml", "utf8"));
const commands = [
  "node --test scripts/identity-cohorts.test.mjs",
  "go test -race -p=1 ./... -list '^(Test|Example|Fuzz)' -count=1 -timeout=90s -json > test-inventory.jsonl",
  "go test -race -coverprofile=coverage-core.out -p=1 ./... -count=1 -timeout=90s -json -skip '^TestWork(Approval|Review)' | tee test-core.jsonl",
  "go test -race -coverprofile=coverage-work.out -p=1 ./... -count=1 -timeout=90s -json -run '^TestWork(Approval|Review)' | tee test-work.jsonl",
  "node ../scripts/identity-cohorts.mjs",
];

/** The observer's self-test needs npm; actual database evidence does not. */
const commandJob = (w, index) => w.jobs[index === 0 ? "test-portal-quality" : "test-portal-identity"];

/** Require every actual cohort command once, unconditionally and in execution order. */
function checkWorkflow(w) {
  const job = w.jobs["test-portal-identity"];
  const quality = w.jobs["test-portal-quality"];
  assert.ok(quality);
  assert.equal(quality.if, undefined);
  assert.ok(quality["continue-on-error"] == null || quality["continue-on-error"] === false);
  assert.deepEqual(w.jobs["test-combined-runtime"].needs, ["test-portal-quality", "test-portal-identity"]);
  assert.equal(job.if, undefined);
  assert.ok(job["continue-on-error"] == null || job["continue-on-error"] === false);
  let previous = job.steps.findIndex((s) => s.run === "bash scripts/check-go-contact.sh");
  assert.ok(previous >= 0);
  for (const [i, command] of commands.entries()) {
    const owner = commandJob(w, i);
    const steps = [quality, job].flatMap((portal) => portal.steps).filter((s) => s.run === command);
    assert.equal(steps.length, 1, `exactly one required invocation: ${command}`);
    const step = steps[0];
    const index = owner.steps.indexOf(step);
    if (i === 0) {
      const install = quality.steps.findIndex(s => s.run === "npm ci");
      assert.ok(install >= 0 && index > install);
    } else {
      assert.ok(index > previous);
      previous = index;
    }
    assert.equal(step.if, undefined);
    assert.ok(step["continue-on-error"] == null || step["continue-on-error"] === false);
    assert.equal(step["working-directory"] ?? owner.defaults?.run?.["working-directory"], i === 0 ? "." : "server");
    if (i === 2 || i === 3) assert.equal(step.shell, "bash", "tee must preserve go failure through pipefail");
  }
}

test("both complete ordinary cohorts and their inventory/result checks are mandatory", () => checkWorkflow(workflow));
for (let i = 0; i < commands.length; i++) {
  for (const [fault, mutate] of [
    [
      "omitted",
      (w, step) => {
        const job = commandJob(w, i);
        job.steps = job.steps.filter((s) => s !== step);
      },
    ],
    [
      "duplicated",
      (w, step) => {
        commandJob(w, i).steps.push(structuredClone(step));
      },
    ],
    [
      "duplicated in the other portal job",
      (w, step) => {
        const other = w.jobs[i === 0 ? "test-portal-identity" : "test-portal-quality"];
        other.steps.push(structuredClone(step));
      },
    ],
    [
      "moved to the other portal job",
      (w, step) => {
        const owner = commandJob(w, i);
        owner.steps = owner.steps.filter((s) => s !== step);
        const other = w.jobs[i === 0 ? "test-portal-identity" : "test-portal-quality"];
        other.steps.push(step);
      },
    ],
    [
      "conditional",
      (_w, step) => {
        step.if = "false";
      },
    ],
    [
      "error tolerant",
      (_w, step) => {
        step["continue-on-error"] = true;
      },
    ],
    [
      "altered command",
      (_w, step) => {
        step.run += " || true";
      },
    ],
  ])
    test(`workflow refuses ${fault} cohort boundary ${i}`, () => {
      const changed = structuredClone(workflow);
      const step = commandJob(changed, i).steps.find((s) => s.run === commands[i]);
      assert.ok(step);
      mutate(changed, step);
      assert.throws(() => checkWorkflow(changed));
    });
}
for (const [name, from, to] of [
  ["narrow package set", "./...", "./internal/identity"],
  ["missing race detector", "-race ", ""],
  ["parallel packages", "-p=1", "-p=2"],
  ["cached tests", "-count=1", "-count=2"],
  ["larger deadline", "-timeout=90s", "-timeout=120s"],
  ["wrong family selector", "^TestWork(Approval|Review)", "^TestWorkApproval"],
])
  test(`workflow refuses ${name}`, () => {
    const changed = structuredClone(workflow);
    changed.jobs["test-portal-identity"].steps.find((s) => s.run === commands[3]).run = commands[3].replace(from, to);
    assert.throws(() => checkWorkflow(changed));
  });
test("workflow refuses a pipeline without explicit bash pipefail", () => {
  const changed = structuredClone(workflow);
  delete changed.jobs["test-portal-identity"].steps.find((s) => s.run === commands[2]).shell;
  assert.throws(() => checkWorkflow(changed));
});

const pkg = "example.test/identity";
const names = ["TestBroker", "TestWorkApproval", "TestWorkReview", "ExampleBroker", "FuzzBroker"];
const ordinary = names.filter((n) => !n.includes("/"));
const event = (Action, extra = {}) => ({ Action, Package: pkg, ...extra });
const inventory = [event("start"), ...ordinary.map((n) => event("output", { Output: `${n}\n` })), event("pass")];
/** Construct complete synthetic top-level execution events for one selected family. */
function execution(selected) {
  return [
    event("start"),
    ...selected.flatMap((n) => [event("run", { Test: n }), event("pass", { Test: n })]),
    event("pass"),
  ];
}
const core = execution(ordinary.filter((n) => !/^TestWork(Approval|Review)/.test(n)));
const work = execution(ordinary.filter((n) => /^TestWork(Approval|Review)/.test(n)));
const coverage = "mode: atomic\nexample.test/identity/broker.go:1.1,2.2 1 1\n";
/** Serialize independent inventory, execution and coverage fixtures for the real verifier. */
function check(i = inventory, c = core, w = work, profiles = [coverage, coverage]) {
  return checkCohorts(
    [i, c, w].map((events) => events.map((e) => JSON.stringify(e)).join("\n") + "\n"),
    profiles,
  );
}
test("actual compiled inventory includes examples and fuzz seeds exactly once across complementary cohorts", () => {
  assert.deepEqual(check(), { packages: 1, tests: 5, core: 3, work: 2 });
});
test("all subtests stay within their selected parent and packages without test files remain explicit", () => {
  const empty = ["start", "skip"].map((Action) => ({ Action, Package: "example.test/no-tests" }));
  const nested = [event("run", { Test: "TestBroker/subcase" }), event("pass", { Test: "TestBroker/subcase" })];
  assert.deepEqual(
    check([...inventory, ...empty], [...core.slice(0, -1), ...nested, event("pass"), ...empty], [...work, ...empty]),
    { packages: 2, tests: 5, core: 3, work: 2 },
  );
});
for (const [fault, change] of [
  ["empty inventory", () => check([])],
  ["duplicate inventory", () => check([...inventory.slice(0, -1), inventory[1], event("pass")])],
  ["omitted cohort", () => check(inventory, core, [])],
  ["duplicated cohort", () => check(inventory, core, core)],
  ["omitted test", () => check(inventory, execution(ordinary.slice(0, 1)), work)],
  ["invented test", () => check(inventory, execution(["TestInvented"]), work)],
  ["wrong family", () => check(inventory, execution(ordinary), work)],
  ["duplicated test", () => check(inventory, core, execution(["TestWorkApproval", "TestWorkApproval"]))],
  [
    "test never started",
    () =>
      check(
        inventory,
        core.filter((e) => e.Action !== "run"),
        work,
      ),
  ],
  [
    "unfinished test",
    () =>
      check(
        inventory,
        core.filter((e) => !(e.Action === "pass" && e.Test)),
        work,
      ),
  ],
  [
    "failed test",
    () =>
      check(
        inventory,
        core.map((e) => (e.Test && e.Action === "pass" ? { ...e, Action: "fail" } : e)),
        work,
      ),
  ],
  [
    "skipped test",
    () =>
      check(
        inventory,
        core.map((e) => (e.Test && e.Action === "pass" ? { ...e, Action: "skip" } : e)),
        work,
      ),
  ],
  [
    "skipped subtest",
    () => check(inventory, [...core.slice(0, -1), event("skip", { Test: "TestBroker/subcase" }), event("pass")], work),
  ],
  ["failed package", () => check(inventory, [...core.slice(0, -1), event("fail")], work)],
  ["unfinished package", () => check(inventory, core.slice(0, -1), work)],
  [
    "foreign package",
    () =>
      check(
        inventory,
        core.map((e) => ({ ...e, Package: "foreign" })),
        work,
      ),
  ],
  ["one coverage profile", () => check(inventory, core, work, [coverage])],
  ["wrong coverage mode", () => check(inventory, core, work, [coverage.replace("atomic", "set"), coverage])],
  ["empty coverage", () => check(inventory, core, work, ["mode: atomic\n", coverage])],
])
  test(`result validation refuses ${fault}`, () => assert.throws(change));
/** Serialize one independent Go event stream, including its final line terminator. */
function jsonLines(events) {
  return events.map((e) => JSON.stringify(e)).join("\n") + "\n";
}

/** Assert malformed evidence refusal without changing the other inputs. */
function assertMalformed(text) {
  assert.throws(() => checkCohorts([jsonLines(inventory), text, jsonLines(work)], [coverage, coverage]));
}

test("malformed or truncated test output is not evidence", () => {
  for (const text of ["{", "{}\n", core.map((e) => JSON.stringify(e)).join("\n") + "\n{"]) {
    assertMalformed(text);
  }
});
test("malformed-input refusal control fails if the suspect stream is actually valid", () => {
  assert.throws(() => assertMalformed(jsonLines(core)), assert.AssertionError);
});

test("public diagnostics identify a fixed reason but never include assertion input diffs", () => {
  const reason = "test cohort must exactly match compiled inventory";
  const error = new assert.AssertionError({
    actual: ["FICTIONAL_PRIVATE_SENTINEL"],
    expected: [],
    operator: "deepStrictEqual",
    message: reason,
  });
  assert.match(error.message, /FICTIONAL_PRIVATE_SENTINEL/);
  assert.equal(failureDiagnostic(error), "Ordinary Go cohort evidence is incomplete or failed: " + reason);
});
test("parser, OS and unknown/generated assertion diagnostics remain generic", () => {
  const generic = "Ordinary Go cohort evidence is incomplete or failed.";
  for (const error of [
    new SyntaxError("FICTIONAL_PRIVATE_SENTINEL"),
    new Error("FICTIONAL_PRIVATE_SENTINEL"),
    new assert.AssertionError({ actual: "FICTIONAL_PRIVATE_SENTINEL", expected: "other", operator: "strictEqual" }),
    new assert.AssertionError({ message: "FICTIONAL_PRIVATE_SENTINEL" }),
  ])
    assert.equal(failureDiagnostic(error), generic);
});

test("actual CLI diagnostics identify refusals without echoing evidence or private paths", (t) => {
  const dir = join(evidenceFile(t), "..");
  for (const [name, contents] of Object.entries({
    "test-inventory.jsonl": jsonLines(inventory),
    "test-core.jsonl": jsonLines(core),
    "test-work.jsonl": jsonLines(work),
    "coverage-core.out": coverage,
    "coverage-work.out": coverage,
  }))
    fs.writeFileSync(join(dir, name), contents);
  /** Execute the real CLI in an owned private fixture directory. */
  const run = () => {
    const result = spawnSync(process.execPath, [fileURLToPath(new URL("./identity-cohorts.mjs", import.meta.url))], {
      cwd: dir,
      encoding: "utf8",
      timeout: 3000,
    });
    assert.ifError(result.error);
    return result;
  };
  assert.equal(run().status, 0, "all unrelated inputs must be valid first");
  fs.writeFileSync(
    join(dir, "test-core.jsonl"),
    jsonLines(core.map((e) => ({ ...e, Package: "example.test/FICTIONAL_PRIVATE_SENTINEL" }))),
  );
  const assertion = run();
  assert.equal(assertion.status, 1);
  assert.equal(
    assertion.stderr.trim(),
    "Ordinary Go cohort evidence is incomplete or failed: every module package must be examined",
  );
  fs.writeFileSync(join(dir, "test-core.jsonl"), '{"FICTIONAL_PRIVATE_SENTINEL":');
  const syntax = run();
  assert.equal(syntax.status, 1);
  assert.equal(syntax.stderr.trim(), "Ordinary Go cohort evidence is incomplete or failed.");
  fs.unlinkSync(join(dir, "test-core.jsonl"));
  const missing = run();
  assert.equal(missing.status, 1);
  assert.equal(missing.stderr.trim(), "Ordinary Go cohort evidence is incomplete or failed.");
  for (const result of [assertion, syntax, missing]) {
    assert.doesNotMatch(result.stdout + result.stderr, /FICTIONAL_PRIVATE_SENTINEL/);
    assert.ok(!result.stderr.includes(dir));
  }
});

/** Own and clean one private temporary file tree for descriptor tests. */
function evidenceFile(t) {
  const dir = fs.mkdtempSync(join(tmpdir(), "identity-evidence-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const path = join(dir, "evidence");
  fs.writeFileSync(path, "original evidence");
  return path;
}

/** Inject a deterministic race after the actual metadata API, restoring both APIs afterward. */
function afterMetadata(change, run) {
  const originals = { statSync: fs.statSync, fstatSync: fs.fstatSync };
  const restore = () => {
    Object.assign(fs, originals);
    syncBuiltinESMExports();
  };
  for (const name of Object.keys(originals))
    fs[name] = (...args) => {
      const result = originals[name](...args);
      restore();
      change();
      return result;
    };
  syncBuiltinESMExports();
  try {
    run();
  } finally {
    restore();
  }
}

test("evidence reads the opened file, not a replacement swapped after metadata inspection", (t) => {
  const path = evidenceFile(t);
  afterMetadata(
    () => {
      fs.renameSync(path, path + ".original");
      fs.writeFileSync(path, "unchecked replacement");
    },
    () => assert.equal(readEvidence(path), "original evidence"),
  );
});
test("evidence refuses symlinks instead of checking and following their targets", (t) => {
  const path = evidenceFile(t);
  fs.symlinkSync(path, path + ".link");
  assert.throws(() => readEvidence(path + ".link"));
});
test("evidence enforces its byte bound even when a checked file grows before the read", (t) => {
  const path = evidenceFile(t);
  afterMetadata(
    () => fs.truncateSync(path, 16 * 1024 * 1024 + 1),
    () => assert.throws(() => readEvidence(path)),
  );
});
test("evidence admits an ordinary file and refuses empty, oversized and directory inputs", (t) => {
  const path = evidenceFile(t);
  assert.equal(readEvidence(path), "original evidence");
  fs.truncateSync(path, 0);
  assert.throws(() => readEvidence(path));
  fs.truncateSync(path, 16 * 1024 * 1024 + 1);
  assert.throws(() => readEvidence(path));
  assert.throws(() => readEvidence(join(path, "..")));
});

test("evidence refuses a FIFO without blocking on its open", (t) => {
  const path = evidenceFile(t) + ".fifo";
  const made = spawnSync("mkfifo", [path], { encoding: "utf8", timeout: 1500 });
  assert.ifError(made.error);
  assert.equal(made.status, 0);
  const result = spawnSync(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      "const {readEvidence}=await import(process.argv[1]); try {readEvidence(process.argv[2]); process.exit(1);} catch {process.exit(0);}",
      new URL("./identity-cohorts.mjs", import.meta.url).href,
      path,
    ],
    { encoding: "utf8", timeout: 1500 },
  );
  assert.ifError(result.error);
  assert.equal(result.status, 0);
});

test("evidence rejects malformed UTF-8 rather than replacing invalid bytes", (t) => {
  const path = evidenceFile(t);
  fs.writeFileSync(path, Buffer.from([0xff]));
  assert.throws(() => readEvidence(path));
});
