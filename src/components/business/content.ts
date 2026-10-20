import { companyFacts } from './company-copy';
import { FEATURE_COMPANY_IDENTITY, FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE, FEATURE_VAT_PRICES } from 'astro:env/server';
import { createBusinessCopy, guideOffers, linkedInUrl, vatPriceMarker, type Locale } from './business-copy';
export const company = companyFacts(FEATURE_VAT_PRICES);
export const vatPricesEnabled = FEATURE_VAT_PRICES;
export { linkedInUrl, type Locale } from './business-copy';

export const companyIdentityEnabled = FEATURE_COMPANY_IDENTITY;
export const inquiryUrl = companyIdentityEnabled ? `mailto:${company.email}` : linkedInUrl;
export const copy = createBusinessCopy(companyIdentityEnabled, FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE, FEATURE_VAT_PRICES);
export const offers = guideOffers(FEATURE_VAT_PRICES);
export const vatMarker = (locale: Locale) => vatPriceMarker(locale, FEATURE_VAT_PRICES);
