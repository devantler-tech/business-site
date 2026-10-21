import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { dump, load } from 'js-yaml';

const root = fileURLToPath(new URL('../', import.meta.url));
const mega = load(readFileSync(join(root, '.mega-linter.yml'), 'utf8'));
const expected = ['go', 'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0'];
// Exact rule snapshot used by the pinned MegaLinter 10.1.0 container. This is
// test input, not a project configuration that replaces the container's rules.
// https://github.com/oxsecurity/megalinter/blob/v10.1.0/TEMPLATES/.golangci.yml
const nativeRules = {
  version: '2',
  linters: {
    default: 'none',
    enable: ['gocritic', 'govet', 'ineffassign', 'staticcheck', 'unused'],
    exclusions: {
      generated: 'lax',
      presets: ['comments', 'common-false-positives', 'legacy', 'std-error-handling'],
      paths: ['third_party$', 'builtin$', 'examples$'],
    },
  },
  formatters: {
    enable: ['gofmt'],
    exclusions: { generated: 'lax', paths: ['third_party$', 'builtin$', 'examples$'] },
  },
};

function command(candidate) {
  assert.deepEqual(candidate.GO_GOLANGCI_LINT_CLI_EXECUTABLE, expected,
    'the embedded analyzer must use the exact compatible executable and version');
  return candidate.GO_GOLANGCI_LINT_CLI_EXECUTABLE;
}

function execute(args, cwd) {
  const [executable, ...prefix] = command(mega);
  const result = spawnSync(executable, [...prefix, ...args], {
    cwd, encoding: 'utf8', timeout: 300000,
    env: { ...process.env, GOTOOLCHAIN: 'local' },
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  return { status: result.status, output: result.stdout + result.stderr };
}

test('the exact analyzer pin is present without changing scanner rules or scope', () => command(mega));
for (const [name, value] of [
  ['missing executable', undefined],
  ['old decoder', ['go', 'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2']],
  ['unbounded latest', ['go', 'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest']],
  ['hidden failures', [...expected, 'run', '--issues-exit-code=0']],
]) {
  test(`${name} is rejected`, () => assert.throws(() => command({ ...mega, GO_GOLANGCI_LINT_CLI_EXECUTABLE: value })));
}

test('the actual executable uses the declared compiler and compatible analyzer', () => {
  const compiler = spawnSync('go', ['version'], { encoding: 'utf8', timeout: 30000 });
  assert.equal(compiler.status, 0);
  assert.match(compiler.stdout, /^go version go1\.27\.3 /);
  const result = execute(['version'], join(root, 'server'));
  assert.equal(result.status, 0, result.output);
  assert.match(result.output, /golangci-lint has version 2\.14\.0 built with go1\.27\.3 /);
});

test('all actual module packages pass the unchanged native rule set', () => {
  const directory = mkdtempSync(join(tmpdir(), 'portal-go-analyzer-'));
  try {
    const config = join(directory, '.golangci.yml');
    writeFileSync(config, dump(nativeRules), 'utf8');
    const result = execute(['run', '-c', config, '--max-same-issues=0', '--max-issues-per-linter=0', '--uniq-by-line=false', './...'], join(root, 'server'));
    assert.equal(result.status, 0, result.output);
    assert.match(result.output, /0 issues\./);
  } finally {
    rmSync(directory, { recursive: true });
  }
});

test('a genuine format defect still fails the actual native govet rule', () => {
  const directory = mkdtempSync(join(tmpdir(), 'portal-go-analyzer-finding-'));
  try {
    const config = join(directory, '.golangci.yml');
    writeFileSync(config, dump(nativeRules), 'utf8');
    writeFileSync(join(directory, 'go.mod'), 'module fixture.invalid/analyzer\n\ngo 1.27.3\n', 'utf8');
    writeFileSync(join(directory, 'fixture.go'), '// Package analyzer tests a real format diagnostic.\npackage analyzer\n\nimport "fmt"\n\nfunc Render() string { return fmt.Sprintf("%d", "not-a-number") }\n', 'utf8');
    const result = execute(['run', '-c', config, './...'], directory);
    assert.equal(result.status, 1, result.output);
    assert.match(result.output, /fmt\.Sprintf format %d has arg [^\n]* of wrong type string \(govet\)/);
    assert.doesNotMatch(result.output, /export data version|cannot decode|can't load config/);
  } finally {
    rmSync(directory, { recursive: true });
  }
});
