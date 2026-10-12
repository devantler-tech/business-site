import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { load } from 'js-yaml';

const root = fileURLToPath(new URL('../', import.meta.url));
const configPath = join(root, '.htmlhintrc');
const rules = JSON.parse(readFileSync(configPath, 'utf8'));
const mega = load(readFileSync(join(root, '.mega-linter.yml'), 'utf8'));

test('package grouping retains selected-file analysis and every HTML rule', () => {
  assert.deepEqual(mega, {
    GO_REVIVE_ARGUMENTS: ['./...'],
    GO_REVIVE_CLI_LINT_MODE: 'list_of_files',
    HTML_HTMLHINT_CONFIG_FILE: '.htmlhintrc',
    HTML_HTMLHINT_RULES_PATH: '/github/workspace',
  });
  assert.deepEqual(rules, {
    'tagname-lowercase': true,
    'attr-lowercase': ['.Request.Product'],
    'attr-value-double-quotes': true,
    'attr-value-not-empty': false,
    'attr-no-duplication': true,
    'doctype-first': true,
    'tag-pair': true,
    'tag-self-close': false,
    'spec-char-escape': true,
    'id-unique': true,
    'src-not-empty': true,
    'title-require': true,
    'alt-require': true,
    'doctype-html5': true,
    'id-class-value': 'dash',
    'style-disabled': false,
    'inline-style-disabled': false,
    'inline-script-disabled': false,
    'space-tab-mixed-disabled': 'space',
    'id-class-ad-disabled': false,
    'href-abs-or-rel': false,
    'attr-unsafe-chars': true,
    'head-script-disabled': true,
  });
});

function lint(files) {
  const result = spawnSync('htmlhint', ['--config', configPath, ...files], {
    cwd: root, encoding: 'utf8', timeout: 30000,
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  return { status: result.status, output: result.stdout + result.stderr };
}

test('the pinned checker analyzes all four actual Go HTML templates', () => {
  const version = spawnSync('htmlhint', ['--version'], { encoding: 'utf8', timeout: 30000 });
  assert.ifError(version.error);
  assert.equal(version.status, 0);
  assert.equal(version.stdout.trim(), '1.9.2');
  const result = lint(['portal', 'contact', 'requests', 'triage'].map(name =>
    join(root, 'server/internal/identity/web', `${name}.html`)));
  assert.equal(result.status, 0, result.output);
  assert.match(result.output, /Scanned 4 files/);
});

for (const [name, content] of [
  ['ordinary uppercase attribute', '<p CLASS="message">Healthy.</p>'],
  ['different template field', '<option {{if eq .Request.Other `website`}}selected{{end}}>Website</option>'],
]) {
  test(`${name} still fails the actual attribute rule`, () => {
    const directory = mkdtempSync(join(tmpdir(), 'portal-template-quality-'));
    try {
      const file = join(directory, 'fixture.html');
      writeFileSync(file, `<!doctype html><html><head><title>Fixture</title></head><body>${content}</body></html>`, 'utf8');
      const result = lint([file]);
      assert.equal(result.status, 1, result.output);
      assert.match(result.output, /attr-lowercase/);
    } finally {
      rmSync(directory, { recursive: true });
    }
  });
}
