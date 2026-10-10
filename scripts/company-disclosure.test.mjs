import assert from 'node:assert/strict';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';
import { parse } from 'parse5';

// Exercise the actual generated visitor pages, not source-text or mock markup.
const root = process.env.COMPANY_DISCLOSURE_DIST ?? 'dist';
const enabled = process.env.FEATURE_COMPANY_IDENTITY === 'true';
const html = (path) => existsSync(join(root, path))
  ? readFileSync(join(root, path), 'utf8') : '';
const walk = (node) => [node, ...(node.childNodes ?? []).flatMap(walk)];
const attr = (node, name) => node.attrs?.find((item) => item.name === name)?.value;
const text = (node) => walk(node).filter((item) => item.nodeName === '#text')
  .map((item) => item.value).join(' ').replace(/\s+/g, ' ').trim();

for (const [prefix, locale] of [['', 'en'], ['da/', 'da']]) {
  test(`${locale}: complete company details are grouped on a directly reachable page`, () => {
    const source = html(`${prefix}company/index.html`);
    const nodes = walk(parse(source));
    const details = nodes.find((node) => attr(node, 'data-company-disclosure') !== undefined);
    if (!enabled) {
      assert.equal(details, undefined, 'The explicit rollback does not reveal registration');
      return;
    }
    assert.ok(details, 'The enabled release needs a grouped company-information page');
    const value = text(details);
    for (const fact of ['Devantler Tech', '46830385', 'PMV', 'Nikolai Emil Damm', 'ned@devantler.tech']) {
      assert.ok(value.includes(fact), `Missing registered company fact: ${fact}`);
    }
    const address = nodes.find((node) => attr(node, 'data-company-address') !== undefined);
    assert.ok(address, 'A physical business address is required, not merely a CVR');
    assert.ok(attr(address, 'data-pagefind-ignore') !== undefined, 'Do not duplicate the residential address in the site search index');
    assert.ok(walk(address).every((node) => !node.tagName || ['address', 'span'].includes(node.tagName)), 'Address values are escaped text, not executable markup');
    const lines = (process.env.COMPANY_POSTAL_ADDRESS ?? '').replace(/\r\n/g, '\n').trim().split('\n').map((line) => line.trim());
    assert.equal(lines.length, 2, 'The test requires an explicitly supplied two-line address');
    for (const line of lines) assert.ok(line && text(address).includes(line), 'The configured address is rendered');
    assert.ok(walk(details).some((node) => attr(node, 'href') === 'mailto:ned@devantler.tech'), 'Email is directly usable');
    assert.ok(nodes.some((node) => node.tagName === 'html' && attr(node, 'lang') === locale));
    assert.ok(nodes.some((node) => attr(node, 'rel') === 'canonical' &&
      attr(node, 'href') === `https://devantler.tech/${prefix}company/`));
  });
  for (const path of ['index.html', 'about/index.html', 'projects/index.html']) {
    test(`${locale}: ${path} has a native footer link without repeating the address`, () => {
      const source = html(prefix + path), nodes = walk(parse(source));
      const footer = nodes.find((node) => attr(node, 'data-business-footer') !== undefined);
      assert.ok(footer, 'The actual visitor page has its shared business footer');
      assert.equal(walk(footer).some((node) => attr(node, 'href') === `/${prefix}company/`), enabled);
      assert.equal(nodes.some((node) => attr(node, 'data-company-address') !== undefined), false,
        'The residential address belongs only on the required company-information page');
    });
  }
}

test('the address is confined to the two required company-information pages', () => {
  const files = (dir) => readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory() ? files(join(dir, entry.name)) : [join(dir, entry.name)]);
  const disclosed = files(root).filter((file) => file.endsWith('.html') &&
    walk(parse(readFileSync(file, 'utf8'))).some((node) => attr(node, 'data-company-address') !== undefined));
  assert.deepEqual(disclosed.sort(), enabled
    ? [join(root, 'company/index.html'), join(root, 'da/company/index.html')].sort() : []);
});
