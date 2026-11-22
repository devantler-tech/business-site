import test from "node:test";
import assert from "node:assert/strict";
import { frontendEntries } from "../src/public-routes.ts";
import { retiredGuides, retiredGuidePaths, retiredGuideCanonical } from "../src/retired-guides.ts";

const formerGuides = ["/agentic-engineering/", "/templates/", "/templates/dotnet/", "/templates/gitops-tenant-template/", "/templates/go/", "/templates/platform-template/", "/templates/platform-tenant-template/"];

// Losing any old guide address must fail independently of healthy Journal routes (#697).
test("every retired guide address stays reachable in every public build", () => {
  assert.deepEqual([...retiredGuidePaths].sort(), formerGuides);
  for (const identity of ["true", "false", undefined]) {
    const entries = frontendEntries(identity);
    for (const path of formerGuides) assert.ok(entries.includes(path as `/${string}`), `missing ${path}`);
    assert.ok(!entries.some(path => path.startsWith("/da/templates/") || path === "/da/agentic-engineering/"), "do not invent translated guides");
  }
});

test("retired guides forward to where their content now lives", () => {
  assert.equal(retiredGuides["/agentic-engineering/"].destination, "/blog/how-my-agentic-engineer-turns-problems-into-proved-working-solutions/");
  assert.equal(retiredGuides["/templates/"].destination, "/projects/");
  for (const path of ["/templates/dotnet/", "/templates/go/", "/templates/gitops-tenant-template/", "/templates/platform-tenant-template/"] as const) {
    assert.equal(retiredGuides[path].destination, "https://github.com/devantler-tech/application-template");
  }
  assert.equal(retiredGuides["/templates/platform-template/"].destination, "https://github.com/devantler-tech/platform-template");
  for (const path of retiredGuidePaths) assert.match(retiredGuides[path].fallback, /^#[a-z_-]+$/, `${path} needs a fallback section`);
});

test("a redirect is canonical to a local destination, never to another site", () => {
  assert.equal(retiredGuideCanonical("/templates/"), "https://devantler.tech/projects/");
  assert.equal(retiredGuideCanonical("/templates/go/"), "https://devantler.tech/templates/go/");
  for (const path of retiredGuidePaths) assert.equal(new URL(retiredGuideCanonical(path)).origin, "https://devantler.tech");
});
