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
  ['iframe document scripts', '<iframe srcdoc="&lt;script src=&quot;https://example.invalid/stats.js&quot;&gt;&lt;/script&gt;"></iframe>'],
  ['iframe document statistics', '<iframe srcdoc="&lt;a data-umami-event=&quot;cta&quot;&gt;Visit&lt;/a&gt;"></iframe>'],
  ['nested iframe documents', '<iframe srcdoc="&lt;iframe srcdoc=&quot;&amp;lt;script src=&amp;quot;https://example.invalid/stats.js&amp;quot;&amp;gt;&amp;lt;/script&amp;gt;&quot;&gt;&lt;/iframe&gt;"></iframe>'],
  ['relative scripts with an external document base', '<base href="https://example.invalid/"><script src="stats.js"></script>'],
  ['iframe scripts inheriting an external base', '<base href="https://example.invalid/"><iframe srcdoc="&lt;script src=&quot;stats.js&quot;&gt;&lt;/script&gt;"></iframe>'],
  ['iframe scripts with their own external base', '<iframe srcdoc="&lt;base href=&quot;https://example.invalid/&quot;&gt;&lt;script src=&quot;stats.js&quot;&gt;&lt;/script&gt;"></iframe>'],
]) {
  test(`rejects ${name}`, () => assert.throws(() => assertVisitorPrivacy(html, 'fixture.html')));
}

for (const [name, html] of [
  ['local scripts', '<script src="/_astro/theme.js"></script>'],
  ['inline appearance script', '<script>document.documentElement.dataset.theme="light";</script>'],
  ['ordinary external links', '<a href="https://example.invalid/">Visit</a>'],
  ['inert example prose', '<pre>&lt;script src="https://example.invalid/stats.js"&gt;</pre>'],
  ['unrelated data attribute', '<script data-src="https://example.invalid/" src="/theme.js"></script>'],
  ['local iframe document scripts', '<iframe srcdoc="&lt;script src=&quot;/theme.js&quot;&gt;&lt;/script&gt;"></iframe>'],
  ['inert srcdoc on another element', '<div srcdoc="&lt;script src=&quot;https://example.invalid/stats.js&quot;&gt;&lt;/script&gt;"></div>'],
  ['local document base', '<base href="/assets/"><script src="theme.js"></script>'],
  ['only the first document base controls scripts', '<base href="/assets/"><base href="https://example.invalid/"><script src="theme.js"></script>'],
  ['absolute local scripts with an external base', '<base href="https://example.invalid/"><script src="https://devantler.tech/theme.js"></script>'],
]) {
  test(`preserves ${name}`, () => assert.doesNotThrow(() => assertVisitorPrivacy(html, 'fixture.html')));
}
