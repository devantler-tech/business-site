import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

test('retired VAT release setting cannot return in production adapters or build entrypoints', () => {
  for (const path of [
    'web/src/env.ts',
    'web/src/lib/business.server.ts',
    'web/src/routes/[[locale]]/company/+page.server.ts', 'scripts/package-runtime.sh',
    '.github/workflows/ci.yaml', '.github/workflows/publish-site.yaml',
  ]) {
    assert.doesNotMatch(readFileSync(new URL(`../${path}`, import.meta.url), 'utf8'),
      /FEATURE_VAT_PRICES|vatPricesEnabled/, `${path} must not select a historical pricing state`);
  }
});
