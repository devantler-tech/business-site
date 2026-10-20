import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { parse } from 'parse5';
import { readerText } from './reader-text.mjs';

// Independent visitor expectations, not strings imported from the copy under test.
const expected = {
  en: [
    'Private customers in Denmark and the rest of the EU, and businesses in Denmark.',
    'I cannot currently take on projects for businesses in other EU countries while the requirements are being confirmed.',
    'Work outside the EU is not open while its requirements are being confirmed.',
  ],
  da: [
    'Privatkunder i Danmark og resten af EU samt virksomheder i Danmark.',
    'Jeg kan foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande, mens kravene afklares.',
    'Opgaver uden for EU er ikke åbne, mens kravene afklares.',
  ],
};
const historicalScope = [
  'I cannot currently take on projects for businesses in other EU countries under the PMV setup.',
  'Under PMV-ordningen kan jeg foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande.',
];
const nodes = node => [node, ...(node.childNodes ?? []).flatMap(nodes)];
const attr = (node, name) => node.attrs?.find(item => item.name === name)?.value;
const text = node => node.nodeName === '#text' ? node.value :
  ['script', 'style', 'template'].includes(node.tagName) ? '' : (node.childNodes ?? []).map(text).join(' ');

/**
 * Assert the emitted page's customer audience, visible invitation and heading.
 * Home pages must also have exactly one normal and one sharing description,
 * each stating the confirmed audience; these checks do not authorize orders.
 * @param {string} html Emitted visitor HTML, including its metadata.
 * @param {'en' | 'da'} locale Language of the expected customer invitation.
 * @param {boolean} home Whether to check Home placement and metadata.
 */
export function assertCustomerScope(html, locale, home = false) {
  const document = nodes(parse(html));
  const section = document.find(node => attr(node, 'data-customer-scope') !== undefined);
  assert.ok(section, `${locale}: customer scope is visible beside the project invitation`);
  for (let ancestor = section.parentNode; ancestor; ancestor = ancestor.parentNode) {
    assert.ok(attr(ancestor, 'hidden') === undefined && attr(ancestor, 'aria-hidden') !== 'true', 'customer scope has no hidden ancestor');
  }
  assert.ok(!nodes(section).some(node => attr(node, 'hidden') !== undefined || attr(node, 'aria-hidden') === 'true'), 'customer scope is not hidden');
  const scope = text(section).replace(/\s+/g, ' ').trim();
  assert.ok(nodes(section).some(node => node.tagName === (home ? 'h3' : 'h2')), 'customer invitation has the correct heading level');
  for (const sentence of expected[locale]) assert.ok(scope.includes(sentence), `${locale}: confirmed customer scope includes ${sentence}`);
  assert.ok(nodes(section).some(node => node.tagName === 'a' && /#contact$/.test(attr(node, 'href') ?? '')) ||
    (home && document.some(node => attr(node, 'id') === 'contact' && nodes(node).includes(section))), 'scope belongs to the actual contact invitation');
  const descriptions = {
    normal: document.filter(node => node.tagName === 'meta' && attr(node, 'name') === 'description').map(node => attr(node, 'content')),
    sharing: document.filter(node => node.tagName === 'meta' && attr(node, 'property') === 'og:description').map(node => attr(node, 'content')),
  };
  const metadata = [...descriptions.normal, ...descriptions.sharing];
  const marketing = [readerText(html), ...metadata].join(' ');
  assert.doesNotMatch(marketing, /for (?:Danish )?businesses(?:[.—]|\s*[|<])|til (?:danske )?virksomheder(?:[.—]|\s*[|<])/iu, 'no unrestricted business invitation remains');
  if (home) {
    assert.equal(descriptions.normal.length, 1, 'Home has exactly one normal description');
    assert.equal(descriptions.sharing.length, 1, 'Home has exactly one sharing description');
    for (const description of metadata) assert.match(description, locale === 'da' ? /privatkunder i EU og virksomheder i Danmark/ : /private customers in the EU and businesses in Denmark/, 'Home metadata includes the confirmed audience');
  }
}

/** Verify the default-off artifact retains the previously published invitation. */
export function assertCustomerScopeDisabled(html, locale, home = false) {
  const document = nodes(parse(html));
  assert.ok(!document.some(node => attr(node, 'data-customer-scope') !== undefined), 'unreleased customer scope is absent');
  assert.doesNotMatch(html, /private customers|privatkunder|Who I work with|Hvem jeg samarbejder med/iu, 'unreleased audience copy and metadata are absent');
  // Check every independent sentence, in either language, in both decoded
  // reader text and metadata. A partial leak need not contain its neighbouring
  // sentence, heading, audience or release marker.
  const emittedCopy = [readerText(html), ...document.filter(node => node.tagName === 'meta')
    .map(node => attr(node, 'content') ?? '')].join(' ').replace(/\s+/g, ' ').toLowerCase();
  for (const sentence of [...Object.values(expected).flat(), ...historicalScope]) {
    assert.ok(!emittedCopy.includes(sentence.toLowerCase()), `unreleased sentence is absent: ${sentence}`);
  }
  if (home) {
    const description = document.filter(node => node.tagName === 'meta' && attr(node, 'name') === 'description');
    const sharing = document.filter(node => node.tagName === 'meta' && attr(node, 'property') === 'og:description');
    assert.equal(description.length, 1);
    assert.equal(sharing.length, 1);
    for (const node of [...description, ...sharing]) assert.match(attr(node, 'content'), locale === 'da' ? /til danske virksomheder/ : /for Danish businesses/, 'published metadata is retained');
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  assert.ok(process.argv.length === 3 || process.argv.length === 4, 'Usage: check-customer-scope.mjs <build-directory> [true|false]');
  const enabled = process.argv[3] ?? 'true';
  assert.ok(['true', 'false'].includes(enabled), 'release state must be explicit true or false');
  const failures = [];
  for (const locale of ['en', 'da']) for (const route of ['', 'about/', 'projects/']) {
    try {
      (enabled === 'true' ? assertCustomerScope : assertCustomerScopeDisabled)(readFileSync(resolve(process.argv[2], locale === 'da' ? 'da' : '.', route, 'index.html'), 'utf8'), locale, route === '');
      console.log(`PASS ${locale} ${route || 'Home'} customer scope ${enabled === 'true' ? 'enabled' : 'disabled'}`);
    } catch (error) { failures.push(`${locale} ${route || 'Home'}: ${error.message}`); }
  }
  assert.deepEqual(failures, [], 'All six emitted visitor routes must explain customer scope');
}
