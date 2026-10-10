import assert from 'node:assert/strict';
import { test } from 'node:test';
import { companyAddress } from '../src/components/business/company-address.mjs';

test('enabled publication requires a configured two-line physical address', () => {
  for (const value of [undefined, '', ' ', 'Example Road 1', '\n1234 Exampletown', 'Example Road 1\n',
    'Example Road 1\n1234 Exampletown\nThird line', 'Example Road\t1\n1234 Exampletown', 'Example\u202e Road 1\n1234 Exampletown',
    `${'x'.repeat(161)}\n1234 Exampletown`]) {
    assert.throws(() => companyAddress(value, true), /COMPANY_POSTAL_ADDRESS/);
  }
});
test('diagnostics do not disclose supplied private values', () => {
  const sensitive = 'Private fixture\t1\n1234 Exampletown';
  assert.throws(() => companyAddress(sensitive, true), (error) => !error.message.includes(sensitive));
});
test('normalizes CRLF and surrounding whitespace without inventing business facts', () => {
  assert.deepEqual(companyAddress(' Example Road 1 \r\n1234 Exampletown ', true), ['Example Road 1', '1234 Exampletown']);
  assert.deepEqual(companyAddress('Example Road 1\n1234 Exampletown\n', true), ['Example Road 1', '1234 Exampletown']);
});
test('rollback does not read or require an address', () => {
  assert.deepEqual(companyAddress(undefined, false), []);
  assert.deepEqual(companyAddress('Private fixture', false), []);
});
