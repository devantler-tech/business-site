import type { Handle } from "@sveltejs/kit/hooks";
import { FEATURE_COMPANY_IDENTITY, COMPANY_POSTAL_ADDRESS, PUBLIC_CONTENT_PROFILE } from "$app/env/private";
import { companyAddress } from "../../src/components/business/company-address.mjs";
import { companyHtml } from "./lib/company-html";

export const handle: Handle = async ({ event, resolve }) => {
  const disclosure = FEATURE_COMPANY_IDENTITY && !event.isDataRequest
    && ["/company/", "/da/company/"].includes(event.url.pathname);
  // Data requests and unrelated routes never read the address. Buffer the two
  // disclosure documents so slot substitution cannot be split by HTML chunks.
  const lines = disclosure ? companyAddress(PUBLIC_CONTENT_PROFILE === "production-template"
    ? "__BUSINESS_ADDRESS_LINE_1__\n__BUSINESS_ADDRESS_LINE_2__" : COMPANY_POSTAL_ADDRESS, true) : null;
  let buffered = "";
  return resolve(event, {
    transformPageChunk: ({ html, done }) => {
      const localized = html.replace("%locale%", event.url.pathname.startsWith("/da/") ? "da" : "en");
      if (!lines) return localized;
      buffered += localized;
      return done ? companyHtml(buffered, lines) : "";
    },
  });
};
