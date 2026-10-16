import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { assertCustomerScope } from './check-customer-scope.mjs';

assert.equal(process.argv.length, 3, 'Usage: customer-scope.test.mjs <build-directory>');
let controls = 0;
for (const locale of ['en', 'da']) for (const route of ['', 'about/', 'projects/']) {
  const html = readFileSync(resolve(process.argv[2], locale === 'da' ? 'da' : '.', route, 'index.html'), 'utf8');
  const check = value => assertCustomerScope(value, locale, route === '');
  check(html);
  for (const [name, mutate] of [
    ['missing scope', value => value.replace('data-customer-scope', 'data-removed-scope')],
    ['hidden scope', value => value.replace('data-customer-scope', 'data-customer-scope hidden')],
    ['hidden parent', value => value.replace(/<main\b/, '<main hidden')],
    ['wrong heading hierarchy', value => value.replace(/(<div\b[^>]*data-customer-scope[^>]*>[\s\S]*?<)h[23](?=[\s>])/, '$1h4')],
    ['missing EU-business restriction', value => value.replace(locale === 'da' ? 'virksomheder i andre EU-lande' : 'businesses in other EU countries', 'customers')],
    ['missing outside-EU hold', value => value.replace(locale === 'da' ? 'Opgaver uden for EU er ikke åbne' : 'Work outside the EU is not open', 'All countries welcome')],
    ['stale invitation', value => value.replace('</main>', '<p>Websites for businesses.</p></main>')],
    ['stale sharing metadata', value => value.replace(/(<meta\b[^>]*property="og:description"[^>]*content=")[^"]*/, '$1Websites for businesses.')],
  ]) {
    assert.throws(() => check(mutate(html)), undefined, `${locale} ${route || 'Home'}: reject ${name}`);
    controls++;
  }
}
console.log(`Customer scope: ${controls} actual-output mutations rejected across six routes`);
