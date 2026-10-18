import test from "node:test";
import assert from "node:assert/strict";
import { parseFrontendRelease, frontendEntries, checkUnseenRoutes } from "../src/release-gate.ts";

test("the candidate stays off unless explicitly enabled", () => {
  for (const value of [undefined, "false"]) {
    assert.equal(parseFrontendRelease(value), false);
    assert.deepEqual(frontendEntries(value), []);
  }
  assert.equal(parseFrontendRelease("true"), true);
  assert.deepEqual(frontendEntries("true").filter(path => !path.startsWith("/blog/")), ["/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/", "/projects/active/", "/projects/completed/", "/templates/gitops-tenant-template/", "/agentic-engineering/", "/templates/dotnet/", "/templates/go/", "/templates/", "/templates/platform-template/", "/templates/platform-tenant-template/"]);
  assert.ok(frontendEntries("true").includes("/blog/"), "enabled artifact must admit the real Journal archive");
  assert.ok(frontendEntries("true").includes("/blog/rss.xml"), "the existing feed path stays local");
  assert.ok(frontendEntries("true").includes("/blog/authors/nikolai-emil-damm/"), "readers can browse the complete author collection locally");
  assert.ok(frontendEntries("true").includes("/blog/tags/kubernetes/"), "the real topic inventory is admitted");
  assert.ok(frontendEntries("true").includes("/blog/building-ksail-from-shell-to-dotnet-to-go/"), "readers can complete the admitted article locally");
  assert.ok(frontendEntries("true").includes("/blog/welcome/"), "the oldest prose-only article stays local too");
});

test("only the deliberately disabled candidate may be absent from the artifact", () => {
  for (const value of [undefined, "false"]) {
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]/about"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]", "/[[locale]]/about"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/[[locale]]/projects", "/projects/[legacy]"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/blog/[[page]]"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/blog/[kind]/[slug]"]));
    assert.doesNotThrow(() => checkUnseenRoutes(value, ["/agentic-engineering", "/templates/[[guide]]", "/templates/gitops-tenant-template"]));
    assert.throws(() => checkUnseenRoutes(value, ["/unreviewed"]), /Unexpected/);
    assert.throws(() => checkUnseenRoutes(value, ["/[[locale]]/about", "/unreviewed"]), /Unexpected/);
  }
  assert.throws(() => checkUnseenRoutes("true", ["/[[locale]]/about"]), /Unexpected/);
  assert.throws(() => checkUnseenRoutes("true", ["/[[locale]]"]), /Unexpected/);
  assert.throws(() => checkUnseenRoutes("true", ["/[[locale]]/projects"]), /Unexpected/);
  assert.throws(() => checkUnseenRoutes("", ["/[[locale]]/about"]), /true or false/);
});

test("ambiguous release values fail closed instead of enabling output", () => {
  for (const value of ["", "1", "TRUE", " true", "false "]) {
    assert.throws(() => parseFrontendRelease(value), /true or false/);
    assert.throws(() => frontendEntries(value), /true or false/);
  }
});
