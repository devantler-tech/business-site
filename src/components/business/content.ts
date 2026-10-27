import { companyFacts } from './company-copy';
import { FEATURE_COMPANY_IDENTITY, FEATURE_OFFER_COPY } from 'astro:env/server';
import { createBusinessCopy, guideOffers, linkedInUrl, vatPriceMarker, type Locale } from './business-copy';
export const company = companyFacts();
export { linkedInUrl, type Locale } from './business-copy';

export const companyIdentityEnabled = FEATURE_COMPANY_IDENTITY;
export const inquiryUrl = companyIdentityEnabled ? `mailto:${company.email}` : linkedInUrl;
export const copy = createBusinessCopy(companyIdentityEnabled, FEATURE_OFFER_COPY);
export const offers = guideOffers();
export const vatMarker = (locale: Locale) => vatPriceMarker(locale);
