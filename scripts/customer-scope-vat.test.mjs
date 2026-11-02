import assert from 'node:assert/strict';
import { test } from 'node:test';
import { customerScope, customerScopeCopy } from '../src/components/business/customer-scope.ts';
import { assertCustomerScope } from './check-customer-scope.mjs';

// These visitor expectations follow #149's ordinary-VAT premise. They do not
// authorize a country, quote, invoice or payment in the private portal.
for (const locale of ['en', 'da']) {
  test(`${locale}: the invitation includes EU businesses without the former PMV restriction`, () => {
    assert.equal(customerScope[locale].audience, locale === 'en'
      ? 'Private customers and businesses in Denmark and the rest of the EU.'
      : 'Privatkunder og virksomheder i Danmark og resten af EU.');
    const text = [customerScope[locale].boundary, ...Object.values(customerScopeCopy[locale])].join(' ');
    assert.doesNotMatch(text, /businesses in Denmark(?=[.—]|$)|Danish businesses|virksomheder i Danmark\.|danske virksomheder|cannot currently take on projects for businesses|foreløbig ikke påtage mig projekter for virksomheder|PMV/iu);
    for (const field of ['description', 'footer']) assert.match(customerScopeCopy[locale][field], locale === 'en'
      ? /private customers and businesses in Denmark and the EU/
      : /privatkunder og virksomheder i Danmark og EU/);
  });
  test(`${locale}: project scope, VAT and outside-EU eligibility are confirmed before an offer`, () => {
    assert.equal(customerScope[locale].boundary, locale === 'en'
      ? 'We confirm the scope, price and applicable VAT before agreeing a project. Projects outside the EU need a separate assessment before I can make an offer.'
      : 'Vi afklarer opgavens omfang, pris og momsbehandling, før vi aftaler et projekt. Projekter uden for EU kræver en særskilt vurdering, før jeg kan give et tilbud.');
  });
}

test('the visitor check rejects an independent worldwide claim without losing required scope text', () => {
  const html = '<html lang="en"><head><meta name="description" content="Websites for private customers and businesses in Denmark and the EU"><meta property="og:description" content="Websites for private customers and businesses in Denmark and the EU"></head><body><main><section id="contact"><div data-customer-scope><h3>Who I work with</h3><p>Private customers and businesses in Denmark and the rest of the EU.</p><p>We confirm the scope, price and applicable VAT before agreeing a project. Projects outside the EU need a separate assessment before I can make an offer.</p></div></section></main></body></html>';
  assertCustomerScope(html, 'en', true);
  assert.throws(() => assertCustomerScope(html.replace('</main>', '<p>Every country is eligible</p></main>'), 'en', true), /worldwide eligibility/);
});
