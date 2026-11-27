import { journalPaths } from "../../src/data/journal.mjs";
import { readingPaths } from "../../src/data/journal-reading.mjs";
import { retiredGuidePaths } from "./retired-guides.ts";

/** Independent, default-off business release settings remain strict. */
export function parseBooleanRelease(value: string | undefined): boolean {
  if (value === undefined || value === "false") return false;
  if (value === "true") return true;
  throw new Error("Business release setting must be true or false");
}

/** Pages that name the registered business follow the company-identity release. */
export const identityEntries = ["/company/", "/da/company/", "/privacy/", "/da/privacy/", "/terms/", "/da/terms/"] as const;
const identityRoutes = ["/[[locale]]/company", "/[[locale]]/privacy", "/[[locale]]/terms"];

/** Svelte owns every public route in every build. */
export function frontendEntries(identity?: string): `/${string}`[] {
  return ["/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/", ...(parseBooleanRelease(identity) ? identityEntries : []), "/projects/active/", "/projects/completed/", ...retiredGuidePaths, "/blog/rss.xml", "/pdfs/nikolai-emil-damm-cv.pdf", "/pdfs/thesis.pdf", "/favicon.png", "/robots.txt", ...journalPaths() as `/${string}`[], ...readingPaths() as `/${string}`[]];
}

/** Every public handler must have an emitted route; only identity-off business pages may be absent. */
export function checkUnseenRoutes(routes: string[], identity?: string): void {
  const remaining = routes.filter(route => !identityRoutes.includes(route) || parseBooleanRelease(identity));
  if (remaining.length) throw new Error(`Unexpected unrendered Svelte routes: ${routes.join(", ")}`);
}
