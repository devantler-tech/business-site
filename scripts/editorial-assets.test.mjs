import assert from 'node:assert/strict';
import { test } from 'node:test';
import sharp from 'sharp';
import { assertUniqueIllustrations, imageDigest } from './check-editorial-assets.mjs';

const first = { slug: 'first', path: '/assets/first.webp', digest: 'first-pixels' };
const second = { slug: 'second', path: '/assets/second.webp', digest: 'second-pixels' };
test('distinct Journal subjects and a separate site illustration are accepted', () => {
  assert.doesNotThrow(() => assertUniqueIllustrations([first, second], [{ path: '/assets/site.webp', digest: 'site-pixels' }]));
});
test('two posts cannot share a cover path', () => assert.throws(() => assertUniqueIllustrations([first, { ...second, path: first.path }]), /duplicate Journal cover path/));
test('renaming the same artwork does not make it unique', () => assert.throws(() => assertUniqueIllustrations([first, { ...second, digest: first.digest }]), /duplicate Journal cover pixels/));
test('a Journal cover cannot also illustrate an unrelated site subject', () => assert.throws(() => assertUniqueIllustrations([first], [{ ...first }]), /unrelated site subject/));
test('renamed artwork cannot be repurposed elsewhere on the site', () => assert.throws(() => assertUniqueIllustrations([first], [{ ...second, digest: first.digest }]), /unrelated site subject/));
test('an empty Journal inventory fails closed', () => assert.throws(() => assertUniqueIllustrations([]), /must not be empty/));
test('separate research subjects cannot share an illustration', () => assert.throws(() => assertUniqueIllustrations([first, { ...second, digest: first.digest }], [], 'research illustration'), /duplicate research illustration pixels/));
test('different encodings of identical pixels have the same digest', async () => {
  const pixels = { create: { width: 8, height: 8, channels: 4, background: '#101a15' } };
  const png = await sharp(pixels).png().toBuffer();
  const webp = await sharp(pixels).webp({ lossless: true }).toBuffer();
  assert.equal(await imageDigest(png), await imageDigest(webp));
});
