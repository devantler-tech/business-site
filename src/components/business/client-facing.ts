import { FEATURE_OFFER_COPY } from 'astro:env/server';
import { createClientCopy } from './client-copy';
export { clientProducts } from './client-products';

export const clientCopy = createClientCopy(FEATURE_OFFER_COPY);
