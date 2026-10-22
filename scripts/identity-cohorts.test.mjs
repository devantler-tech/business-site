import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { load } from "js-yaml";
import { checkCohorts } from "./identity-cohorts.mjs";

const workflow = load(readFileSync(".github/workflows/ci.yaml", "utf8"));
const commands = [
  "node --test scripts/identity-cohorts.test.mjs",
  "go test -race -p=1 ./... -list '^(Test|Example|Fuzz)' -count=1 -timeout=90s -json > test-inventory.jsonl",
  "go test -race -coverprofile=coverage-core.out -p=1 ./... -count=1 -timeout=90s -json -skip '^TestWork(Approval|Review)' | tee test-core.jsonl",
  "go test -race -coverprofile=coverage-work.out -p=1 ./... -count=1 -timeout=90s -json -run '^TestWork(Approval|Review)' | tee test-work.jsonl",
  "node ../scripts/identity-cohorts.mjs",
];

function checkWorkflow(w) {
  const job = w.jobs["test-portal-identity"];
  assert.equal(job.if, undefined);
  assert.ok(job["continue-on-error"] == null || job["continue-on-error"] === false);
  let previous = job.steps.findIndex((s) => s.run === "bash scripts/check-go-contact.sh");
  assert.ok(previous >= 0);
  for (const [i, command] of commands.entries()) {
    const steps = job.steps.filter((s) => s.run === command);
    assert.equal(steps.length, 1, `exactly one required invocation: ${command}`);
    const step = steps[0];
    const index = job.steps.indexOf(step);
    assert.ok(index > previous);
    previous = index;
    assert.equal(step.if, undefined);
    assert.ok(step["continue-on-error"] == null || step["continue-on-error"] === false);
    assert.equal(step["working-directory"] ?? job.defaults?.run?.["working-directory"], i === 0 ? "." : "server");
    if (i === 2 || i === 3) assert.equal(step.shell, "bash", "tee must preserve go failure through pipefail");
  }
}

test("both complete ordinary cohorts and their inventory/result checks are mandatory", () => checkWorkflow(workflow));
for (let i = 0; i < commands.length; i++) {
  for (const [fault, mutate] of [
    [
      "omitted",
      (w, step) => {
        w.jobs["test-portal-identity"].steps = w.jobs["test-portal-identity"].steps.filter((s) => s !== step);
      },
    ],
    [
      "duplicated",
      (w, step) => {
        w.jobs["test-portal-identity"].steps.push(structuredClone(step));
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
      const step = changed.jobs["test-portal-identity"].steps.find((s) => s.run === commands[i]);
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
test("malformed or truncated test output is not evidence", () => {
  for (const text of ["{", "{}\n", core.map((e) => JSON.stringify(e)).join("\n") + "\n{"]) {
    assert.throws(() =>
      checkCohorts([inventory.map((e) => JSON.stringify(e)).join("\n"), text, ""], [coverage, coverage]),
    );
  }
});
