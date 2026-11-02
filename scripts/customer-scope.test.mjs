import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { assertCustomerScope, assertCustomerScopeDisabled } from './check-customer-scope.mjs';

assert.ok(process.argv.length === 3 || process.argv.length === 4, 'Usage: customer-scope.test.mjs <build-directory> [true|false]');
const enabled = process.argv[3] ?? 'true';
assert.ok(['true', 'false'].includes(enabled));
let controls = 0;
// Exercise each independently publishable fragment in both languages, without
// importing the checker inventory: partial leaks must not need an audience or marker.
const unreleasedFragments = [
  'Private customers and businesses in Denmark and the rest of the EU.',
  'We confirm the scope, price and applicable VAT before agreeing a project.',
  'Projects outside the EU need a separate assessment before I can make an offer.',
  'Privatkunder og virksomheder i Danmark og resten af EU.',
  'Vi afklarer opgavens omfang, pris og momsbehandling, før vi aftaler et projekt.',
  'Projekter uden for EU kræver en særskilt vurdering, før jeg kan give et tilbud.',
  'Private customers in Denmark and the rest of the EU, and businesses in Denmark.',
  'I cannot currently take on projects for businesses in other EU countries under the PMV setup.',
  'I cannot currently take on projects for businesses in other EU countries while the requirements are being confirmed.',
  'Work outside the EU is not open while its requirements are being confirmed.',
  'Who I work with',
  'Privatkunder i Danmark og resten af EU samt virksomheder i Danmark.',
  'Under PMV-ordningen kan jeg foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande.',
  'Jeg kan foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande, mens kravene afklares.',
  'Opgaver uden for EU er ikke åbne, mens kravene afklares.',
  'Hvem jeg samarbejder med',
];
for (const locale of ['en', 'da']) for (const route of ['', 'about/', 'projects/']) {
  const html = readFileSync(resolve(process.argv[2], locale === 'da' ? 'da' : '.', route, 'index.html'), 'utf8');
  const check = value => (enabled === 'true' ? assertCustomerScope : assertCustomerScopeDisabled)(value, locale, route === '');
  check(html);
  for (const [name, mutate] of enabled === 'false' ? [
    ['leaked scope', value => value.replace('</main>', '<div data-customer-scope></div></main>')],
    ['leaked audience', value => value.replace('</main>', '<p>Private customers in the EU</p></main>')],
    ['leaked sharing metadata', value => value.replace(/(<meta\b[^>]*property="og:description"[^>]*content=")[^"]*/, '$1Private customers in the EU')],
    ...unreleasedFragments.flatMap((fragment, index) => [
      [`isolated fragment ${index} in body`, value => value.replace('</main>', `<p>${fragment}</p></main>`)],
      [`isolated fragment ${index} in normal metadata`, value => value.replace(/(<meta\b[^>]*name="description"[^>]*content=")[^"]*/, `$1${fragment}`)],
      [`isolated fragment ${index} in sharing metadata`, value => value.replace(/(<meta\b[^>]*property="og:description"[^>]*content=")[^"]*/, `$1${fragment}`)],
    ]),
  ] : [
    ['missing scope', value => value.replace('data-customer-scope', 'data-removed-scope')],
    ['hidden scope', value => value.replace('data-customer-scope', 'data-customer-scope hidden')],
    ['hidden parent', value => value.replace(/<main\b/, '<main hidden')],
    ['wrong heading hierarchy', value => value.replace(/(<div\b[^>]*data-customer-scope[^>]*>[\s\S]*?<)h[23](?=[\s>])/, '$1h4')],
    ['missing EU-business invitation', value => value.replace(locale === 'da' ? 'Privatkunder og virksomheder i Danmark og resten af EU.' : 'Private customers and businesses in Denmark and the rest of the EU.', locale === 'da' ? 'Kun virksomheder i Danmark.' : 'Businesses in Denmark only.')],
    ['missing VAT confirmation', value => value.replace(locale === 'da' ? 'pris og momsbehandling' : 'price and applicable VAT', 'price')],
    ['missing outside-EU assessment', value => value.replace(locale === 'da' ? 'Projekter uden for EU kræver en særskilt vurdering' : 'Projects outside the EU need a separate assessment', 'All countries welcome')],
    ['stale EU-business exclusion', value => value.replace('</main>', `<p>${locale === 'da' ? 'Jeg kan foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande, mens kravene afklares.' : 'I cannot currently take on projects for businesses in other EU countries while the requirements are being confirmed.'}</p></main>`)],
    ['independent worldwide claim', value => value.replace('</main>', `<p>${locale === 'da' ? 'Ethvert land er godkendt' : 'Every country is eligible'}</p></main>`)],
    ['stale invitation', value => value.replace('</main>', '<p>Websites for businesses.</p></main>')],
    ['stale sharing metadata', value => value.replace(/(<meta\b[^>]*property="og:description"[^>]*content=")[^"]*/, '$1Websites for businesses.')],
    ...(route === '' ? [
      ['duplicate normal description', value => value.replace('property="og:description"', 'name="description"')],
      ['duplicate sharing description', value => value.replace('name="description"', 'property="og:description"')],
    ] : []),
  ]) {
    const changed = mutate(html);
    assert.notEqual(changed, html, `${locale} ${route || 'Home'}: mutation reaches ${name}`);
    assert.throws(() => check(changed), undefined, `${locale} ${route || 'Home'}: reject ${name}`);
    controls++;
  }
}
console.log(`Customer scope: ${controls} actual-output mutations rejected across six routes`);
