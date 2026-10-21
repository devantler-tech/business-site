import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../', import.meta.url));
const config = join(root, '.cspell.json');
const glossary = JSON.parse(readFileSync(config, 'utf8'));
const scopedFile = join(root, 'server/internal/webapp/app_test.go');

function check(text, file = scopedFile) {
  const result = spawnSync('cspell', ['lint', '--config', config,
    '--no-config-search', '--no-progress', '--no-color', '--no-cache', `stdin://${file}`], {
    cwd: root, input: text, encoding: 'utf8', timeout: 30000,
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  return { status: result.status, output: result.stdout + result.stderr };
}

test('the glossary retains default checking and only three exact adversarial URL spellings', () => {
  assert.deepEqual(Object.keys(glossary).sort(), ['language', 'overrides', 'version', 'words']);
  assert.equal(glossary.version, '0.2');
  assert.equal(glossary.language, 'en');
  assert.ok(glossary.words.includes('forespørgsel'));
  assert.ok(glossary.words.includes('pgxpool'));
  for (const fragment of ['fportal', 'fsign', 'ortal']) assert.ok(!glossary.words.includes(fragment));
  assert.deepEqual(glossary.overrides, [{
    filename: 'server/internal/webapp/app_test.go',
    ignoreRegExpList: ['/%70ortal/sign-in', '/portal%2fsign-in', '/%2fportal'],
  }]);
});

test('the pinned checker examines the complete tracked server source', () => {
  const version = spawnSync('cspell', ['--version'], { encoding: 'utf8', timeout: 30000 });
  assert.ifError(version.error);
  assert.equal(version.status, 0);
  assert.equal(version.stdout.trim(), '10.1.1');
  const tracked = spawnSync('git', ['ls-files', '-z', '--', 'server'], {
    cwd: root, encoding: 'utf8', timeout: 30000,
  });
  assert.ifError(tracked.error);
  assert.equal(tracked.status, 0);
  const files = tracked.stdout.split('\0').filter(file => /\.(go|html|js|css|md|sql|json)$/.test(file));
  assert.ok(files.length > 0);
  const result = spawnSync('cspell', ['lint', '--config', config, '--no-config-search',
    '--no-progress', '--no-color', '--no-cache', ...files], {
    cwd: root, encoding: 'utf8', timeout: 30000,
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  assert.equal(result.status, 0, result.stdout + result.stderr);
  assert.match(result.stderr + result.stdout, /Issues found: 0/);
});

test('valid Danish copy and project API names pass the real checker', () => {
  const result = check('Send forespørgsel. Dine projektforespørgsler. pgxpool openfeature PKCE. contacttrial contacttrialintegration contactintegration autofocus.');
  assert.equal(result.status, 0, result.output);
});

test('only the three exact encoded paths pass in their actual refusal fixture', () => {
  const result = check('"/%70ortal/sign-in" "/portal%2fsign-in" "/%2fportal"');
  assert.equal(result.status, 0, result.output);
});

for (const [name, text, file] of [
  ['English typo beside the URL fixture', 'authentciation', scopedFile],
  ['Danish typo beside the URL fixture', 'forespøgrsler', scopedFile],
  ['host package typo', 'contacttrialhosst', scopedFile],
  ['timing package typo', 'contaccttrial', scopedFile],
  ['contact build-tag typo', 'contactintegraton', scopedFile],
  ['unrelated bare fragments', 'fportal fsign ortal', scopedFile],
  ['same URL outside the one scoped fixture', '"/%2fportal"', join(root, 'server/internal/runtimeapp/config_test.go')],
]) {
  test(`${name} remains a real spelling failure`, () => {
    const result = check(text, file);
    assert.equal(result.status, 1, result.output);
    assert.match(result.output, /Unknown word/);
  });
}
