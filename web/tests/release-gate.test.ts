import test from "node:test";
import assert from "node:assert/strict";
import { parseBooleanRelease, frontendEntries, checkUnseenRoutes } from "../src/public-routes.ts";

test("ordinary builds admit the complete public site with company identity separately controlled", () => {
  const routes = frontendEntries();
  for (const path of ["/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/", "/blog/", "/blog/rss.xml", "/blog/welcome/", "/blog/authors/nikolai-emil-damm/", "/blog/tags/kubernetes/", "/agentic-engineering/", "/templates/", "/templates/gitops-tenant-template/", "/pdfs/nikolai-emil-damm-cv.pdf", "/pdfs/thesis.pdf", "/favicon.png", "/robots.txt"]) assert.ok(routes.includes(path as `/${string}`));
  assert.ok(!routes.includes("/company/"));
  assert.ok(frontendEntries("true").includes("/company/"));
  assert.ok(frontendEntries("true").includes("/da/company/"));
  assert.equal(new Set(routes).size,routes.length);
});

test("a missing public route fails the build while disabled company pages may remain absent", () => {
  assert.doesNotThrow(()=>checkUnseenRoutes(["/[[locale]]/company"]));
  for (const route of ["/[[locale]]", "/[[locale]]/about", "/[[locale]]/projects", "/blog/[[page]]", "/agentic-engineering", "/unreviewed"]) assert.throws(()=>checkUnseenRoutes([route]), /Unexpected/);
  assert.throws(()=>checkUnseenRoutes(["/[[locale]]/company"],"true"), /Unexpected/);
});

test("business release flags reject ambiguous values", () => {
  assert.equal(parseBooleanRelease(undefined),false);
  assert.equal(parseBooleanRelease("false"),false);
  assert.equal(parseBooleanRelease("true"),true);
  for (const value of ["", "1", "TRUE", " true", "false "]) assert.throws(()=>parseBooleanRelease(value),/true or false/);
});
