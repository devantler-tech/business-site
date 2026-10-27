import assert from 'node:assert/strict';
import test from 'node:test';
import { createBusinessCopy } from '../src/components/business/business-copy.ts';
import { createSupportingCopy, previewSupporting } from '../src/components/business/supporting-copy.ts';
import { publishedOffer } from '../src/components/business/published-offer.ts';

for (const identity of [false, true]) for (const offer of [false, true]) {
  test(`permanent customer invitation is independent of identity=${identity}, offer=${offer}`, () => {
    const selected = createBusinessCopy(identity, offer);
    for (const locale of ['en', 'da']) {
      assert.match(selected[locale].description, locale === 'da' ? /privatkunder og virksomheder i Danmark og EU/ : /private customers and businesses in Denmark and the EU/);
      assert.match(selected[locale].intro, locale === 'da' ? /privatkunder og virksomheder i Danmark og resten af EU/ : /private customers and businesses across Denmark and the EU/);
      assert.doesNotMatch(selected[locale].title, /for businesses|til virksomheder/);
      assert.doesNotMatch(selected[locale].steps[0].text + selected[locale].contactText + selected[locale].offerCopy[0].description, /your business|din virksomhed/);
      assert.equal(selected[locale].contactAction, locale === 'da' ? identity ? 'Skriv en email til mig' : 'Skriv til mig på LinkedIn' : identity ? 'Email me' : 'Message me on LinkedIn');
      assert.match(selected[locale].priceNote, locale === 'da' ? /inkl\. 25 % moms/ : /include 25% (?:Danish )?VAT/);
    }
  });
}
for (const offer of [false, true]) test(`permanent supporting invitation is independent of offer=${offer}`, () => {
  const selected = createSupportingCopy(offer);
  for (const locale of ['en', 'da']) {
    assert.doesNotMatch(selected[locale].principles[2].text, /your business|din virksomhed/);
    assert.equal(selected[locale].engineeringScope, (offer ? previewSupporting[locale] : publishedOffer.supporting[locale]).engineeringScope);
  }
});
