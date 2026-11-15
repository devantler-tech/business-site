import { error } from "@sveltejs/kit";
import { FEATURE_COMPANY_IDENTITY } from "$app/env/private";
import { legalPages, type LegalKind } from "../../../src/components/business/legal-copy";

// The privacy notice and customer terms name the registered business, so they
// follow the company-identity release exactly like the company page.
export function legalPageLoad(kind: LegalKind, locale: string | undefined) {
  if (!FEATURE_COMPANY_IDENTITY || (locale !== undefined && locale !== "da")) error(404);
  const selected: "en" | "da" = locale === "da" ? "da" : "en";
  return { locale: selected, kind, copy: legalPages[kind][selected] };
}
