import assert from 'node:assert/strict';
import { test } from 'node:test';
import { rankPublicProducts } from '../src/data/public-products.ts';
import catalogue from '../src/data/public-products.json' with { type: 'json' };

const snapshot = (repositories) => ({ observedAt: '2026-10-07', repositories });
test('restricted-reuse products identify their own actual licence filename', () => {
  const licensed = catalogue.filter((product) => product.terms);
  assert.deepEqual(licensed.map(({ repository, licenseFile }) => [repository, licenseFile]), [
    ['ksail', 'LICENSE'], ['world-at-ruin', 'LICENSE.md'],
  ]);
});
const products = ['zeta', 'beta', 'alpha', 'zero'].map((repository) => ({ ...catalogue[0], repository }));
test('ranks by stars, resolves ties by repository name and retains zero stars without mutating input', () => {
  const ranked = rankPublicProducts(products, snapshot({ zeta: 2, beta: 2, alpha: 100, zero: 0 }));
  assert.deepEqual(ranked.map(({ repository, stars }) => [repository, stars]), [['alpha', 100], ['beta', 2], ['zeta', 2], ['zero', 0]]);
  assert.deepEqual(products.map(({ repository }) => repository), ['zeta', 'beta', 'alpha', 'zero']);
});
test('a complete all-positive observation does not require or invent a zero-star product', () => {
  const ranked = rankPublicProducts(products, snapshot({ zeta: 2, beta: 2, alpha: 100, zero: 1 }));
  assert.deepEqual(ranked.map(({ repository, stars }) => [repository, stars]), [['alpha', 100], ['beta', 2], ['zeta', 2], ['zero', 1]]);
  assert.equal(ranked.filter(({ stars }) => stars === 0).length, 0);
});
test('a partial snapshot cannot quietly replace missing counts with zero', () => {
  assert.throws(() => rankPublicProducts(products, snapshot({ zeta: 2, beta: 2, alpha: 100 })), /complete catalogue/);
  assert.throws(() => rankPublicProducts(products, snapshot({ zeta: 2, beta: 2, alpha: 100, unexpected: 0 })), /Missing or invalid.*zero/);
});
test('invalid counts and dates fail closed', () => {
  for (const stars of [-1, 1.5, null, '4']) assert.throws(() => rankPublicProducts(products, snapshot({ zeta: 2, beta: 2, alpha: 100, zero: stars })), /Missing or invalid/);
  for (const observedAt of ['', 'not-a-date', '2026-02-30']) assert.throws(() => rankPublicProducts(products, { ...snapshot({}), observedAt }), /valid observation date/);
});
