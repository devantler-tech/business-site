import { journalPaths } from "../../src/data/journal.mjs";
import { readingPaths, guidePaths } from "../../src/data/journal-reading.mjs";

/** Independent, default-off business release settings remain strict. */
export function parseBooleanRelease(value: string | undefined): boolean {
  if (value === undefined || value === "false") return false;
  if (value === "true") return true;
  throw new Error("Business release setting must be true or false");
}

/** Svelte owns every public route in every build. */
export function frontendEntries(identity?: string): `/${string}`[] {
  return ["/", "/da/", "/about/", "/da/about/", "/projects/", "/da/projects/", ...(parseBooleanRelease(identity) ? ["/company/", "/da/company/"] as const : []), "/projects/active/", "/projects/completed/", "/templates/gitops-tenant-template/", "/blog/rss.xml", "/pdfs/nikolai-emil-damm-cv.pdf", "/pdfs/thesis.pdf", "/favicon.png", "/robots.txt", ...journalPaths() as `/${string}`[], ...readingPaths() as `/${string}`[], ...guidePaths() as `/${string}`[]];
}

/** Every public handler must have an emitted route; only identity-off disclosures may be absent. */
export function checkUnseenRoutes(routes: string[], identity?: string): void {
  const remaining = routes.filter(route => route !== "/[[locale]]/company" || parseBooleanRelease(identity));
  if (remaining.length) throw new Error(`Unexpected unrendered Svelte routes: ${routes.join(", ")}`);
}
