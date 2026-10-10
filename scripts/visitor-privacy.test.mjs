import assert from 'node:assert/strict';
import test from 'node:test';
import { assertVisitorPrivacy } from './visitor-privacy.mjs';

for (const [name, html] of [
  ['double quotes', '<script src="https://example.invalid/stats.js"></script>'],
  ['single quotes', "<script src='https://example.invalid/stats.js'></script>"],
  ['unquoted attributes', '<script src=//example.invalid/stats.js></script>'],
  ['uppercase names', '<SCRIPT SRC="https://example.invalid/stats.js"></SCRIPT>'],
  ['HTML references', '<script src="https&#58;//example.invalid/stats.js"></script>'],
  ['statistics registration', '<script src="/stats.js" data-website-id="example"></script>'],
  ['event registration', '<a href="https://example.invalid/" DATA-UMAMI-EVENT="cta">Visit</a>'],
]) {
  test(`rejects ${name}`, () => assert.throws(() => assertVisitorPrivacy(html, 'fixture.html')));
}

for (const [name, html] of [
  ['local scripts', '<script src="/_astro/theme.js"></script>'],
  ['inline appearance script', '<script>document.documentElement.dataset.theme="light";</script>'],
  ['ordinary external links', '<a href="https://example.invalid/">Visit</a>'],
  ['inert example prose', '<pre>&lt;script src="https://example.invalid/stats.js"&gt;</pre>'],
  ['unrelated data attribute', '<script data-src="https://example.invalid/" src="/theme.js"></script>'],
]) {
  test(`preserves ${name}`, () => assert.doesNotThrow(() => assertVisitorPrivacy(html, 'fixture.html')));
}
