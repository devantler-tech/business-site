import assert from 'node:assert/strict';
import test from 'node:test';
import { companyCopyFor, companyFacts } from '../src/components/business/company-copy.ts';
import {
  createBusinessCopy, guideOffers, netOf, offers, vatInclusiveOffers, vatPriceMarker, vatPriceNote, vatRatePercent,
} from '../src/components/business/business-copy.ts';

// The maintainer's #154 decision: rounded VAT-inclusive totals whose net amount
// never falls below the earlier price, which now acts as a floor.
const decided = {
  website: { setup: 3750, monthly: 125 },
  app: { setup: 10000, monthly: 375 },
  service: { setup: 6250, monthly: 250 },
};

test('current identity has the confirmed effective date and no serialized PMV facts', () => {
  assert.equal(companyFacts(true).businessType, 'Enkeltmandsvirksomhed');
  assert.equal(companyFacts(true).vatEffectiveDate, '2026-10-20');
  assert.doesNotMatch(JSON.stringify([companyFacts(true), companyCopyFor(true)]), /PMV|personally owned small business|personligt ejet mindre virksomhed/i);
  assert.equal(companyFacts(false).businessType, 'PMV', 'false selects only the historical comparison');
  assert.equal(companyFacts(false).vatEffectiveDate, undefined);
});

test('VAT-inclusive totals are exactly the decided prices', () => {
  assert.equal(vatRatePercent, 25);
  assert.deepEqual(Object.fromEntries(vatInclusiveOffers.map(({ id, setup, monthly }) => [id, { setup, monthly }])), decided);
  assert.deepEqual(vatInclusiveOffers.map(offer => offer.id), offers.map(offer => offer.id), 'same offers in the same order');
});

test('no VAT-inclusive total lowers the net amount below the earlier price', () => {
  for (const [i, offer] of vatInclusiveOffers.entries()) {
    for (const field of ['setup', 'monthly']) {
      const net = netOf(offer[field]);
      assert.ok(net >= offers[i][field], `${offer.id} ${field}: net ${net} must be at least ${offers[i][field]}`);
      assert.equal(net + (net * vatRatePercent) / 100, offer[field], `${offer.id} ${field}: net plus VAT is the advertised total`);
    }
  }
});

test('VAT is never added to an already VAT-inclusive total', () => {
  // A net amount as large as the earlier price plus VAT would mean VAT had
  // already been added once before this total added it again.
  for (const [i, offer] of vatInclusiveOffers.entries()) {
    for (const field of ['setup', 'monthly']) {
      const ceiling = offers[i][field] * (100 + vatRatePercent) / 100;
      assert.ok(netOf(offer[field]) < ceiling, `${offer.id} ${field}: net ${netOf(offer[field])} must stay below ${ceiling}`);
    }
  }
});

test('all six gross totals contain the exact approved net amount and VAT', () => {
  const expected = [[375000, 300000, 75000], [12500, 10000, 2500],
    [1000000, 800000, 200000], [37500, 30000, 7500],
    [625000, 500000, 125000], [25000, 20000, 5000]];
  const actual = vatInclusiveOffers.flatMap(offer => ['setup', 'monthly'].map(field => {
    const gross = offer[field] * 100, net = netOf(offer[field]) * 100;
    return [gross, net, gross - net];
  }));
  assert.deepEqual(actual, expected, 'integer minor-unit gross = net + VAT for every price');
});

test('net amounts must be exact whole kroner', () => {
  assert.equal(netOf(3750), 3000);
  assert.throws(() => netOf(3749), /no whole-krone net amount/);
});

test('the release decision selects the guide prices and the marker', () => {
  assert.equal(guideOffers(false), offers);
  assert.equal(guideOffers(true), vatInclusiveOffers);
  assert.equal(vatPriceMarker('en', false), null);
  assert.equal(vatPriceMarker('da', false), null);
  assert.equal(vatPriceMarker('en', true), 'incl. VAT');
  assert.equal(vatPriceMarker('da', true), 'inkl. moms');
});

for (const identity of [false, true]) for (const offer of [false, true]) for (const scope of [false, true]) {
  test(`VAT release selects consistent price and ordinary registration (identity=${identity}, offer=${offer}, scope=${scope})`, () => {
    const off = createBusinessCopy(identity, offer, scope);
    assert.deepEqual(createBusinessCopy(identity, offer, scope, false), off, 'off is the default');
    const on = createBusinessCopy(identity, offer, scope, true);
    for (const [locale, pmv, vat] of [
      ['en', 'No VAT is charged under the current PMV setup.', 'Prices include 25% Danish VAT.'],
      ['da', 'Der opkræves ikke moms under den nuværende PMV-ordning.', 'Priserne er inkl. 25 % moms.'],
    ]) {
      assert.ok(off[locale].priceNote.includes(pmv));
      assert.ok(!on[locale].priceNote.includes(pmv));
      assert.ok(on[locale].priceNote.includes(vat));
      assert.equal(on[locale].priceNote, off[locale].priceNote.replace(pmv, vat));
      assert.equal(on[locale].registrationType, locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship');
      assert.deepEqual({ ...on[locale], priceNote: off[locale].priceNote, registrationType: off[locale].registrationType }, off[locale], 'unrelated offer and audience decisions stay unchanged');
    }
  });
}

test('a price note that no longer states the PMV position fails the build instead of shipping mixed wording', () => {
  assert.throws(() => vatPriceNote('en', 'Prices are indicative and in DKK.'), /review the VAT wording/);
  assert.throws(() => vatPriceNote('da', 'Priserne er vejledende.'), /review the VAT wording/);
});
