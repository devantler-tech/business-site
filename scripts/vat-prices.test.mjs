import assert from 'node:assert/strict';
import test from 'node:test';
import { companyCopyFor, companyFacts } from '../src/components/business/company-copy.ts';
import {
  createBusinessCopy, guideOffers, netOf, vatInclusiveOffers, vatPriceMarker, vatRatePercent,
} from '../src/components/business/business-copy.ts';

// Historical net floors are regression expectations, never a visitor price state.
const offers = [{id:'website',setup:2995,monthly:99},{id:'app',setup:7995,monthly:299},{id:'service',setup:4995,monthly:199}];

// The maintainer's #154 decision: rounded VAT-inclusive totals whose net amount
// never falls below the earlier price, which now acts as a floor.
const decided = {
  website: { setup: 3750, monthly: 125 },
  app: { setup: 10000, monthly: 375 },
  service: { setup: 6250, monthly: 250 },
};

test('current identity has the confirmed effective date and no serialized PMV facts', () => {
  assert.equal(companyFacts().businessType, 'Enkeltmandsvirksomhed');
  assert.equal(companyFacts().vatEffectiveDate, '2026-10-20');
  assert.doesNotMatch(JSON.stringify([companyFacts(), companyCopyFor()]), /PMV|personally owned small business|personligt ejet mindre virksomhed/i);
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

test('ordinary builds always select the approved VAT-inclusive totals', () => {
  assert.deepEqual(Object.fromEntries(guideOffers().map(({ id, setup, monthly }) => [id, { setup, monthly }])), decided);
  assert.equal(vatPriceMarker('en'), 'incl. VAT');
  assert.equal(vatPriceMarker('da'), 'inkl. moms');
});

test('ordinary builds cannot select historical company facts', () => {
  assert.equal(companyFacts().businessType, 'Enkeltmandsvirksomhed');
  assert.equal(companyFacts().vatEffectiveDate, '2026-10-20');
  assert.doesNotMatch(JSON.stringify(companyCopyFor()), /PMV|personally owned small business|personligt ejet mindre virksomhed/i);
});

for (const identity of [false, true]) for (const offer of [false, true]) {
  test(`ordinary pricing is permanent without changing other releases (${identity}/${offer}/permanent)`, () => {
    const copy = createBusinessCopy(identity, offer);
    assert.match(copy.en.priceNote, /Prices include 25% Danish VAT[.]/);
    assert.match(copy.da.priceNote, /Priserne er inkl[.] 25 % moms[.]/);
    assert.equal(copy.en.registrationType, 'Sole proprietorship');
    assert.equal(copy.da.registrationType, 'Enkeltmandsvirksomhed');
  });
}
