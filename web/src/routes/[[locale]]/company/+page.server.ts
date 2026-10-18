import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND, FEATURE_COMPANY_IDENTITY } from "$app/env/private";
import company from "../../../../../src/data/company.json";
import { companyCopy } from "../../../../../src/components/business/company-copy";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || !FEATURE_COMPANY_IDENTITY || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  return {
    locale, company, copy: companyCopy[locale],
  };
};
