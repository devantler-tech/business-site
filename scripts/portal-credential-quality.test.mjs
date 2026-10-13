import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const root = fileURLToPath(new URL('../', import.meta.url));
const path = join(root, '.secretlintrc.json');
const config = existsSync(path) ? JSON.parse(readFileSync(path, 'utf8')) : {
  rules: [{ id: '@secretlint/secretlint-rule-preset-recommend' }],
};
const examples = [
  ["postgres://portal_fixture_owner:fixture-only@localhost:5432/postgres?sslmode=disable'", 'server/README.md'],
  ["postgres://portal_migrator:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'", 'server/README.md'],
  ["postgres://portal_runtime:fixture-only@localhost:5432/portal_identity_test?sslmode=disable'", 'server/README.md'],
  ['postgres://runtime:private@db.example.invalid/app?sslmode=verify-full",', 'server/internal/runtimeapp/config_test.go'],
  ['postgres://runtime:private@db.example.invalid/app?sslmode=disable",', 'server/internal/runtimeapp/config_test.go'],
];
const exactPattern = value => `/^${value.replace(/[.*+?^${}()|[\]\\/]/g, '\\$&')}$/u`;
const expected = {
  rules: [{ id: '@secretlint/secretlint-rule-preset-recommend', rules: [{
    id: '@secretlint/secretlint-rule-database-connection-string',
    options: { allows: examples.map(([value]) => exactPattern(value)) },
  }] }],
};

function scan(files, candidate = config) {
  const directory = mkdtempSync(join(tmpdir(), 'portal-credential-config-'));
  try {
    const configFile = join(directory, '.secretlintrc.json');
    writeFileSync(configFile, JSON.stringify(candidate), 'utf8');
    const result = spawnSync('secretlint', ['--secretlintrc', configFile, '--format', 'json', ...files], {
      cwd: root, encoding: 'utf8', timeout: 30000,
    });
    assert.ifError(result.error);
    assert.equal(result.signal, null);
    assert.ok(result.status === 0 || result.status === 1, result.stdout + result.stderr);
    // Preserve every finding and file identity without printing complete source
    // or the detected credential itself into a public CI failure diagnostic.
    const results = JSON.parse(result.stdout).map(entry => ({
      filePath: entry.filePath,
      messages: entry.messages.map(({ ruleId, messageId, range, loc }) => ({ ruleId, messageId, range, loc })),
    }));
    return { status: result.status, results };
  } finally {
    rmSync(directory, { recursive: true });
  }
}

function fixture(content, callback) {
  const directory = mkdtempSync(join(tmpdir(), 'portal-credential-example-'));
  try {
    const file = join(directory, 'fixture.txt');
    writeFileSync(file, content, 'utf8');
    return callback(file);
  } finally {
    rmSync(directory, { recursive: true });
  }
}

test('only five complete fictional strings are recognized; the full preset is retained', () => {
  assert.deepEqual(config, expected);
  for (const [value, file] of examples) assert.ok(readFileSync(join(root, file), 'utf8').includes(value));
});

test('the actual pinned scanner examines the complete tracked server', () => {
  const version = spawnSync('secretlint', ['--version'], { encoding: 'utf8', timeout: 30000 });
  assert.equal(version.status, 0);
  assert.equal(version.stdout.trim(), '13.0.5');
  const tracked = spawnSync('git', ['ls-files', '-z', '--', 'server'], { cwd: root, encoding: 'utf8' });
  assert.equal(tracked.status, 0);
  const files = tracked.stdout.split('\0').filter(Boolean);
  assert.ok(files.length >= 46);
  const result = scan(files);
  assert.equal(result.results.length, files.length);
  assert.equal(new Set(result.results.map(entry => entry.filePath)).size, files.length);
  assert.equal(result.status, 0, JSON.stringify(result.results.filter(entry => entry.messages.length)));
});

for (const [index, [value]] of examples.entries()) {
  test(`fictional example ${index + 1} passes but fails without its exact recognition`, () => fixture(value, file => {
    assert.equal(scan([file]).status, 0);
    const result = scan([file], { rules: [{ id: '@secretlint/secretlint-rule-preset-recommend' }] });
    assert.equal(result.status, 1);
    assert.ok(result.results.some(entry => entry.messages.some(message => message.ruleId === '@secretlint/secretlint-rule-database-connection-string')));
  }));
  for (const part of ['host', 'password', 'role', 'database']) {
    test(`changed ${part} beside fictional example ${index + 1} still fails`, () => {
      const suffix = value.endsWith("'") ? "'" : '",';
      const uri = new URL(value.slice(0, -suffix.length));
      assert.equal(uri.href + suffix, value, 'serialization must not change the unmutated control');
      if (part === 'host') uri.hostname = 'changed.example.invalid';
      else if (part === 'password') uri.password = 'DistinctFixture83';
      else if (part === 'role') uri.username = 'changed_runtime';
      else uri.pathname = '/changed_database';
      fixture(uri.href + suffix, file => {
        const result = scan([file]);
        assert.equal(result.status, 1);
        assert.ok(result.results.some(entry => entry.messages.some(message => message.ruleId === '@secretlint/secretlint-rule-database-connection-string')));
      });
    });
  }
}

test('an unrelated private-key rule still reports a synthetic marker', () => {
  // Deliberately scanner-shaped but not a usable key: recognizable magic bytes
  // and a repeated fictional payload, never generated customer key material.
  const marker = `-----BEGIN PRIVATE KEY-----\nMI${'SyntheticFixture83'.repeat(12)}\n-----END PRIVATE KEY-----\n`;
  fixture(marker, file => {
    const result = scan([file]);
    assert.equal(result.status, 1);
    assert.ok(result.results.some(entry => entry.messages.some(message => message.ruleId === '@secretlint/secretlint-rule-privatekey')));
  });
});
