import products from './public-products.json' with { type: 'json' };
import snapshot from './github-stars.json' with { type: 'json' };

type Product = typeof products[number];
type StarSnapshot = { observedAt: string; repositories: Record<string, number> };

// Missing reads must fail the build, not make a popular product look unstarred.
export function rankPublicProducts(catalogue: Product[], stars: StarSnapshot) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(stars.observedAt) || !Number.isFinite(Date.parse(stars.observedAt)) || new Date(stars.observedAt).toISOString().slice(0, 10) !== stars.observedAt) {
    throw new Error('Public product stars need a valid observation date');
  }
  const names = catalogue.map((product) => product.repository);
  if (new Set(names).size !== names.length || Object.keys(stars.repositories).length !== names.length) {
    throw new Error('Public product stars must match the complete catalogue');
  }
  return catalogue.map((product) => {
    const count = stars.repositories[product.repository];
    if (!Object.hasOwn(stars.repositories, product.repository) || !Number.isSafeInteger(count) || count < 0) {
      throw new Error(`Missing or invalid GitHub stars for ${product.repository}; refresh the complete snapshot`);
    }
    return { ...product, stars: count };
  }).sort((a, b) => b.stars - a.stars || (a.repository < b.repository ? -1 : a.repository > b.repository ? 1 : 0));
}

export const publicProducts = rankPublicProducts(products, snapshot);
export const starsObservedAt = snapshot.observedAt;
