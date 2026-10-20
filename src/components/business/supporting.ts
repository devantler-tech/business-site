import { FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE } from 'astro:env/server';
import { createSupportingCopy } from './supporting-copy';

export const supporting = createSupportingCopy(FEATURE_OFFER_COPY, FEATURE_CUSTOMER_SCOPE);
