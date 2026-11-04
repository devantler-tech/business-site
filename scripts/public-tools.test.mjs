import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as products from '../src/data/public-products.ts';

const catalogue = ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'old', 'game', 'template'].map((repository) => ({ repository, showcaseTool: !['old', 'game', 'template'].includes(repository) }));
const snapshot = () => ({ observedAt: '2026-10-09', fetchedAt: '2026-10-09T12:00:00Z', repositories: { a: 70, b: 60, c: 50, d: 40, e: 30, f: 20, g: 10, old: 900, game: 800, template: 700 }, metadata: Object.fromEntries(catalogue.map(({ repository }) => [repository, { fork: false, archived: false, private: false }])) });
const select = (data, items = catalogue) => { assert.equal(typeof products.selectPublicTools, 'function', 'A real curated tool selector is required'); return products.selectPublicTools(items, data); };
test('shows at most six maintained tools, not popular legacy software, games or starters', () => {
  assert.deepEqual(select(snapshot()).map(({ repository }) => repository), ['a', 'b', 'c', 'd', 'e', 'f']);
});
test('fresh metadata changes both order and sixth/seventh membership', () => {
  const next = snapshot(); next.repositories.g = 100;
  assert.deepEqual(select(next).map(({ repository }) => repository), ['g', 'a', 'b', 'c', 'd', 'e']);
  next.repositories.g = 20;
  assert.deepEqual(select(next).map(({ repository }) => repository), ['a', 'b', 'c', 'd', 'e', 'f']);
});
test('a curated showcase smaller than six is published without filler', () => {
  const curated = catalogue.map((item) => ({ ...item, showcaseTool: ['a', 'c', 'e', 'g'].includes(item.repository) }));
  assert.deepEqual(select(snapshot(), curated).map(({ repository }) => repository), ['a', 'c', 'e', 'g']);
});
test('forked, archived and private entries cannot appear even with more stars', () => {
  for (const field of ['fork', 'archived', 'private']) {
    const next = snapshot(); next.metadata.a[field] = true;
    assert.deepEqual(select(next).map(({ repository }) => repository), ['b', 'c', 'd', 'e', 'f', 'g']);
  }
});
test('partial, malformed or unobserved metadata cannot silently choose winners', () => {
  for (const field of ['fork', 'archived', 'private']) { const next = snapshot(); delete next.metadata.a[field]; assert.throws(() => select(next), /metadata/); }
  const partial = snapshot(); delete partial.metadata.g; assert.throws(() => select(partial), /metadata/);
  const undated = snapshot(); delete undated.fetchedAt; assert.throws(() => select(undated), /observation/);
});
test('a read that leaves fewer than three eligible tools keeps the previous publication', () => {
  const insufficient = snapshot();
  for (const repository of ['a', 'b', 'c', 'd']) insufficient.metadata[repository].archived = true;
  insufficient.metadata.e.fork = true;
  assert.throws(() => select(insufficient), /three/);
  const minimal = snapshot();
  for (const repository of ['a', 'b', 'c', 'd']) minimal.metadata[repository].archived = true;
  assert.deepEqual(select(minimal).map(({ repository }) => repository), ['e', 'f', 'g']);
});
test('the displayed observation timestamp must be real and match the snapshot day', () => {
  for (const fetchedAt of ['2026-02-30T12:00:00Z', '2026-10-08T12:00:00Z', '2026-10-09T99:00:00Z']) {
    assert.throws(() => select({ ...snapshot(), fetchedAt }), /observation/);
  }
});
