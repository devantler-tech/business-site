import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND, FEATURE_COMPANY_IDENTITY, FEATURE_VAT_PRICES } from "$app/env/private";
import { companyCopyFor, companyFacts } from "../../../../../src/components/business/company-copy";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || !FEATURE_COMPANY_IDENTITY || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  return {
    locale, company: companyFacts(FEATURE_VAT_PRICES), copy: companyCopyFor(FEATURE_VAT_PRICES)[locale],
  };
};
