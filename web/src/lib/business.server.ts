import { FEATURE_COMPANY_IDENTITY, FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE, FEATURE_VAT_PRICES } from "$app/env/private";
import company from "../../../src/data/company.json";
import { createBusinessCopy, guideOffers, linkedInUrl, vatPriceMarker } from "../../../src/components/business/business-copy";
import { createClientCopy } from "../../../src/components/business/client-copy";
import { createSupportingCopy } from "../../../src/components/business/supporting-copy";

export const business = {
  companyEnabled: FEATURE_COMPANY_IDENTITY,
  customerScopeEnabled: FEATURE_CUSTOMER_SCOPE,
  inquiryUrl: FEATURE_COMPANY_IDENTITY ? `mailto:${company.email}` : linkedInUrl,
  copy: createBusinessCopy(FEATURE_COMPANY_IDENTITY, FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE, FEATURE_VAT_PRICES),
  client: createClientCopy(FEATURE_OFFER_COPY),
  supporting: createSupportingCopy(FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE),
  offers: guideOffers(FEATURE_VAT_PRICES),
  vatMarker: (locale: "en" | "da") => vatPriceMarker(locale, FEATURE_VAT_PRICES),
};

// Only public, rendered footer data crosses the server boundary. In particular,
// the postal address is read exclusively by the two company-page loaders.
export function businessFrame(locale: "en" | "da") {
  const t = business.copy[locale];
  return {
    identity: FEATURE_COMPANY_IDENTITY ? company : null,
    inquiryUrl: business.inquiryUrl,
    customerScopeEnabled: FEATURE_CUSTOMER_SCOPE,
    footer: { identity: t.footerIdentity, note: t.footer, registration: t.registrationType,
      about: t.aboutNav, projects: t.projectsNav, contact: t.contact },
  };
}
