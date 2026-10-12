import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

// Run the actual served asset. Unrelated DOM/theme setup is inert here;
// assertions exercise the production character counter, not mock callbacks.
const context = vm.createContext({ document: { querySelector: () => null, querySelectorAll: () => [] } });
vm.runInContext(readFileSync(new URL('../server/internal/identity/web/portal.js', import.meta.url), 'utf8'), context);
for (const [name, value, limit, lang, want] of [
  ['title ASCII at boundary', 'a'.repeat(120), 120, 'en', ''],
  ['title supplementary characters at boundary', '🚀'.repeat(120), 120, 'en', ''],
  ['title one over boundary', '🚀'.repeat(121), 120, 'en', 'Use up to 120 characters.'],
  ['brief supplementary characters at boundary', '🚀'.repeat(1200), 1200, 'en', ''],
  ['brief one over boundary', '🚀'.repeat(1201), 1200, 'en', 'Use up to 1200 characters.'],
  ['Danish error', 'ø'.repeat(121), 120, 'da', 'Brug højst 120 tegn.'],
]) {
  test(name, () => {
    assert.equal(typeof context.requestCharacterError, 'function', 'The served asset must supply the real Unicode limit check');
    assert.equal(context.requestCharacterError(value, limit, lang), want);
  });
}
