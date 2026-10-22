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
  ['postgres://portal_runtime:fixture-only@127.0.0.1:55444/portal_trial_0123456789abcdef0123456789abcdef?sslmode=disable",', 'server/internal/contacttrialhost/config_test.go'],
  ['postgres://portal_runtime:fixture-only@127.0.0.1:%d/%s?sslmode=disable",', 'server/internal/identity/contact_trial_host_test.go'],
  ['postgres://portal_runtime:fixture-only@127.0.0.1:55444,remote.example.invalid:5432/portal_identity_test?sslmode=disable",', 'server/internal/identity/contact_trial_host_test.go'],
  ['postgres://portal_runtime:fixture-only@127.0.0.1:55444/portal_identity_test?sslmode=disable&options=-c+role=portal_runtime",', 'server/internal/identity/contact_trial_host_test.go'],
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
      cwd: root, encoding: 'utf8', timeout: 30000, maxBuffer: 16 * 1024 * 1024,
    });
    assert.ifError(result.error);
    assert.equal(result.signal, null);
    assert.ok(result.status === 0 || result.status === 1, 'credential scanner failed; private diagnostics withheld');
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

test('only nine complete fictional strings are recognized; the full preset is retained', () => {
  assert.deepEqual(config, expected);
  for (const [value, file] of examples) assert.ok(readFileSync(join(root, file), 'utf8').includes(value));
});

test('the actual pinned scanner examines the complete tracked server', () => {
  const version = spawnSync('secretlint', ['--version'], { encoding: 'utf8', timeout: 30000 });
  assert.equal(version.status, 0);
  assert.equal(version.stdout.trim(), '13.0.7');
  const tracked = spawnSync('git', ['ls-files', '-z', '--', 'server'], { cwd: root, encoding: 'utf8' });
  assert.equal(tracked.status, 0);
  const files = tracked.stdout.split('\0').filter(Boolean);
  assert.ok(files.length >= 46);
  const result = scan(files);
  assert.equal(result.results.length, files.length);
  assert.equal(new Set(result.results.map(entry => entry.filePath)).size, files.length);
  assert.equal(result.status, 0, JSON.stringify(result.results.filter(entry => entry.messages.length)));
});

test('scanner retains complete findings beyond the default subprocess buffer', () => {
  fixture('ordinary public documentation\n'.repeat(50000), file => {
    const result = scan([file]);
    assert.equal(result.status, 0);
    assert.equal(result.results.length, 1);
    assert.deepEqual(result.results[0].messages, []);
  });
});

test('scanner failures stay closed without echoing private diagnostics', () => {
  const marker = 'fictional-private-diagnostic';
  fixture(marker, file => {
    assert.throws(() => scan([file], { rules: [{ id: marker }] }), error => {
      assert.ok(!error.message.includes(marker), 'scanner failure echoed private data');
      return true;
    });
  });
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
      // Preserve literal format placeholders and intentionally invalid multi-host
      // refusal inputs too; URL serialization cannot represent those fixtures.
      const fields = /^(postgres:\/\/)([^:]+):([^@]+)@([^/]+)\/([^?]+)(\?.*)$/u.exec(value);
      assert.ok(fields);
      const [, scheme, role, password, host, database, query] = fields;
      const changed = scheme + (part === 'role' ? 'changed_runtime' : role) + ':'
        + (part === 'password' ? 'DistinctFixture83' : password) + '@'
        + (part === 'host' ? 'changed.example.invalid' : host) + '/'
        + (part === 'database' ? 'changed_database' : database) + query;
      assert.notEqual(changed, value);
      fixture(changed, file => {
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
