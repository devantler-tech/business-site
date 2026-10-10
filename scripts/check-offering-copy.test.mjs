import assert from 'node:assert/strict';
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

const [directory, ...extraArguments] = process.argv.slice(2);
assert.ok(directory && extraArguments.length === 0, 'Usage: check-offering-copy.test.mjs <build-directory>');
const fixture = mkdtempSync(resolve(tmpdir(), 'business-offering-copy-'));
const checker = new URL('./check-business-site.mjs', import.meta.url);
const run = () => spawnSync(process.execPath, [checker.pathname, fixture], { encoding: 'utf8' });
try {
  // Mutate a separate copy of real output, never the publication artifact.
  cpSync(resolve(directory), fixture, { recursive: true });
  const homePath = resolve(fixture, 'index.html');
  const home = readFileSync(homePath, 'utf8');
  const failures = [];
  for (const [name, attribute] of [
    ['English image description', 'alt="A small web app"'],
    ['English accessible name', 'aria-label="A small web app"'],
    ['Encoded image description', 'alt="A sm&#97;ll web app"'],
    ['Encoded accessible name', 'aria-label="A sm&#97;ll web app"'],
    ['Danish image description', 'alt="En lille webapp"'],
    ['Danish accessible name', 'aria-label="Sm&#229; webapps"'],
  ]) {
    writeFileSync(homePath, home.replace(/<body\b[^>]*>/, '$&<img ' + attribute + '>'));
    const result = run();
    const rejected = result.status !== 0 && /offering copy must not undersell project size/.test(result.stderr);
    console.log(`${rejected ? 'PASS' : 'FAIL'} ${name}`);
    if (!rejected) failures.push(name);
  }
  assert.deepEqual(failures, [], 'The actual visitor checker must reject size qualifiers in accessibility copy');
  writeFileSync(homePath, home.replace(/<body\b[^>]*>/,
    '$&<img alt="Personally owned small business (PMV)"><span aria-label="Smallest available icon"></span>'));
  const positive = run();
  assert.equal(positive.status, 0, positive.stderr);
  console.log('PASS official registration wording and non-word matches remain valid');
} finally {
  rmSync(fixture, { recursive: true, force: true });
}
