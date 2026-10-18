import { journalPaths } from "../../src/data/journal.mjs";
import { readingPaths, guidePaths } from "../../src/data/journal-reading.mjs";

/** Validate the framework-native build-time release setting. */
export function parseFrontendRelease(value: string | undefined): boolean {
  if (value === undefined || value === "false") return false;
  if (value === "true") return true;
  throw new Error("FEATURE_SVELTE_FRONTEND must be true or false");
}

/** Enumerate only the reviewed public candidate routes. */
export function frontendEntries(value: string | undefined): `/${string}`[] {
  return parseFrontendRelease(value) ? ["/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/", "/projects/active/", "/projects/completed/", "/blog/rss.xml", ...journalPaths() as `/${string}`[], ...readingPaths() as `/${string}`[], ...guidePaths() as `/${string}`[]] : [];
}

/** Fail for missing enabled/new routes; allow only the deliberately disabled candidate. */
export function checkUnseenRoutes(value: string | undefined, routes: string[]): void {
  const enabled = parseFrontendRelease(value);
  const disabled = ["/[[locale]]", "/[[locale]]/about", "/[[locale]]/projects", "/projects/[legacy]", "/blog/[[page]]", "/blog/[kind]/[slug]", "/blog/rss.xml", "/_astro/[image]", "/agentic-engineering", "/templates/[[guide]]"];
  if (enabled || routes.some((route) => !disabled.includes(route))) {
    throw new Error(`Unexpected unrendered Svelte routes: ${routes.join(", ")}`);
  }
}
