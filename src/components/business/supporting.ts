import { FEATURE_OFFER_COPY } from 'astro:env/server';
import { createSupportingCopy } from './supporting-copy';

export const supporting = createSupportingCopy(FEATURE_OFFER_COPY);
