/** Validate the framework-native build-time release setting. */
export function parseFrontendRelease(value: string | undefined): boolean {
  if (value === undefined || value === "false") return false;
  if (value === "true") return true;
  throw new Error("FEATURE_SVELTE_FRONTEND must be true or false");
}

/** Enumerate only the reviewed public candidate routes. */
export function frontendEntries(value: string | undefined): `/${string}`[] {
  return parseFrontendRelease(value) ? ["/about/", "/da/about/"] : [];
}

/** Fail for missing enabled/new routes; allow only the deliberately disabled candidate. */
export function checkUnseenRoutes(value: string | undefined, routes: string[]): void {
  const enabled = parseFrontendRelease(value);
  if (enabled || routes.some((route) => route !== "/[[locale]]/about")) {
    throw new Error(`Unexpected unrendered Svelte routes: ${routes.join(", ")}`);
  }
}
