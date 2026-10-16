import company from '../../data/company.json';
import { FEATURE_COMPANY_IDENTITY, FEATURE_OFFER_COPY } from 'astro:env/server';
import { createBusinessCopy, linkedInUrl } from './business-copy';
export { default as company } from '../../data/company.json';
export { offers, linkedInUrl, type Locale } from './business-copy';

export const companyIdentityEnabled = FEATURE_COMPANY_IDENTITY;
export const inquiryUrl = companyIdentityEnabled ? `mailto:${company.email}` : linkedInUrl;
export const copy = createBusinessCopy(companyIdentityEnabled, FEATURE_OFFER_COPY);
