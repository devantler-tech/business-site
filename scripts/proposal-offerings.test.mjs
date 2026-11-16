import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { createBusinessCopy, launchDiscount, vatInclusiveOffers } from '../src/components/business/business-copy.ts';
import { standardAddons } from '../src/components/business/addons.ts';

// The portal generates proposals from server/internal/identity/offerings.json
// (ADR 0030, #552). It must say exactly what the published site says, at the
// same VAT-inclusive prices, or a generated quote would contradict the offer.
const catalogue = JSON.parse(readFileSync(new URL('../server/internal/identity/offerings.json', import.meta.url), 'utf8'));
const published = createBusinessCopy(true, false);

test('generated proposals use the published VAT-inclusive prices', () => {
  assert.deepEqual(catalogue.offerings.map(({ id, setup, monthly }) => ({ id, setup, monthly })),
    vatInclusiveOffers.map(({ id, setup, monthly }) => ({ id, setup, monthly })));
});

test('generated proposals use the published offering copy in both languages', () => {
  for (const lang of ['en', 'da']) {
    assert.deepEqual(catalogue.offerings.map(offering => offering.text[lang]),
      published[lang].offerCopy.map(({ title, description, includes, boundary }) => ({ title, description, includes, boundary })), lang);
    assert.equal(catalogue.hosting[lang], published[lang].hostingText, `${lang} hosting`);
    assert.ok(published[lang].priceNote.endsWith(catalogue.vat[lang]), `${lang} VAT note`);
  }
});

test('the drift check notices a changed price', () => {
  const changed = structuredClone(catalogue);
  changed.offerings[0].setup += 1;
  assert.notDeepEqual(changed.offerings.map(({ id, setup, monthly }) => ({ id, setup, monthly })),
    vatInclusiveOffers.map(({ id, setup, monthly }) => ({ id, setup, monthly })));
});

test('generated proposals carry the published launch offer and the free first consultation', () => {
  const { percent, limit, appliesTo, requiresHosting, text } = catalogue.launchDiscount;
  assert.deepEqual({ percent, limit }, launchDiscount, 'same discount and customer limit as the site');
  assert.equal(appliesTo, 'setup', 'the discount applies to the one-time price only');
  assert.equal(requiresHosting, true, 'only orders with hosting qualify');
  for (const lang of ['en', 'da']) {
    assert.ok(published[lang].launchNote.startsWith(text[lang]), `${lang} proposal launch text matches the site`);
    assert.ok(published[lang].servicesIntro.includes(catalogue.consultation[lang].replace(/\.$/, '')), `${lang} site and proposals both offer a free first consultation`);
  }
});

test('the portal sells exactly the published add-ons at the published prices', () => {
  assert.deepEqual(catalogue.addons, JSON.parse(JSON.stringify(standardAddons)));
});

test('the add-on drift check notices a changed add-on price', () => {
  const changed = structuredClone(catalogue);
  changed.addons[0].price += 1;
  assert.notDeepEqual(changed.addons, JSON.parse(JSON.stringify(standardAddons)));
});
