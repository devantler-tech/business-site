import assert from 'node:assert/strict';
import test from 'node:test';
import { createBusinessCopy } from '../src/components/business/business-copy.ts';
import { createSupportingCopy } from '../src/components/business/supporting-copy.ts';

for (const identity of [false, true]) for (const offer of [false, true]) {
  test(`customer copy preserves default-off wording independently of identity=${identity}, offer=${offer}`, () => {
    const baseline = createBusinessCopy(identity, offer);
    assert.deepEqual(createBusinessCopy(identity, offer, false), baseline);
    const selected = createBusinessCopy(identity, offer, true);
    for (const locale of ['en', 'da']) {
      assert.match(baseline[locale].title, locale === 'da' ? /til virksomheder/ : /for businesses/);
      assert.match(baseline[locale].intro, locale === 'da' ? /til virksomheder—/ : /for businesses—and/);
      assert.match(baseline[locale].contactText, locale === 'da' ? /din virksomhed/ : /your business/);
      assert.match(baseline[locale].steps[0].text, locale === 'da' ? /din virksomhed/ : /your business/);
      assert.match(baseline[locale].offerCopy[0].description, locale === 'da' ? /din virksomhed/ : /your business/);
      assert.doesNotMatch(JSON.stringify(baseline[locale]), /private customers|privatkunder/iu);
      assert.match(selected[locale].description, locale === 'da' ? /privatkunder i EU og virksomheder i Danmark/ : /private customers in the EU and businesses in Denmark/);
      assert.match(selected[locale].intro, locale === 'da' ? /privatkunder og danske virksomheder/ : /private customers and Danish businesses/);
      assert.doesNotMatch(selected[locale].title, /for businesses|til virksomheder/);
      assert.doesNotMatch(selected[locale].steps[0].text + selected[locale].contactText + selected[locale].offerCopy[0].description, /your business|din virksomhed/);
      for (const key of ['priceNote', 'hostingText', 'fitText', 'contactAction', 'contactNote', 'availability']) assert.equal(selected[locale][key], baseline[locale][key], `${key} is not activated by customer-scope release`);
      assert.deepEqual(selected[locale].steps.slice(1), baseline[locale].steps.slice(1));
      assert.deepEqual(selected[locale].offerCopy.slice(1), baseline[locale].offerCopy.slice(1));
    }
  });
}
for (const offer of [false, true]) test(`supporting copy changes only the neutral principle, offer=${offer}`, () => {
  const baseline = createSupportingCopy(offer);
  assert.deepEqual(createSupportingCopy(offer, false), baseline);
  const selected = createSupportingCopy(offer, true);
  for (const locale of ['en', 'da']) {
    assert.match(baseline[locale].principles[2].text, locale === 'da' ? /din virksomhed/ : /your business/);
    assert.doesNotMatch(selected[locale].principles[2].text, /your business|din virksomhed/);
    assert.deepEqual(selected[locale].principles.slice(0, 2), baseline[locale].principles.slice(0, 2));
    assert.equal(selected[locale].engineeringScope, baseline[locale].engineeringScope);
  }
});
