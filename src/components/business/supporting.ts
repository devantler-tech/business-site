import { FEATURE_OFFER_COPY } from 'astro:env/server';
import { publishedOffer } from './published-offer';
import { previewSupporting } from './supporting-copy';

export const supporting = FEATURE_OFFER_COPY ? previewSupporting : {
  en: { ...previewSupporting.en, ...publishedOffer.supporting.en },
  da: { ...previewSupporting.da, ...publishedOffer.supporting.da },
} as const;
