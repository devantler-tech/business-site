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
const workflow = load(readFileSync(join(root, '.github/workflows/ci.yaml'), 'utf8'));

function mandatoryQualityChecks(candidate) {
  const job = candidate.jobs['test-portal-identity'];
  assert.equal(job.if, undefined);
  assert.ok(job['continue-on-error'] == null || job['continue-on-error'] === false);
  for (const command of [
    'npx --yes --package=htmlhint@1.9.2 -- node --test scripts/portal-template-quality.test.mjs',
    'npx --yes --package=cspell@10.1.1 -- node --test scripts/portal-spelling-quality.test.mjs',
    'node --test scripts/portal-go-analyzer-quality.test.mjs',
    'go install github.com/mgechev/revive@v1.16.0\nPORTAL_REVIVE="$GOBIN/revive" bash scripts/portal-package-quality.test.sh\n',
  ]) {
    const steps = job.steps.filter(step => step.run === command);
    assert.equal(steps.length, 1, 'the actual quality entrypoint must occur exactly once');
    assert.equal(steps[0]['working-directory'], '.');
    assert.equal(steps[0].if, undefined);
    assert.ok(steps[0]['continue-on-error'] == null || steps[0]['continue-on-error'] === false);
  }
}

test('the actual source CI unconditionally executes every quality observer', () => mandatoryQualityChecks(workflow));
for (const observer of ['portal-template-quality.test.mjs', 'portal-spelling-quality.test.mjs', 'portal-package-quality.test.sh', 'portal-go-analyzer-quality.test.mjs']) {
  for (const mutation of ['missing', 'conditional', 'ignored failure']) {
    test(`${observer} cannot be ${mutation}`, () => {
      const candidate = structuredClone(workflow);
      const steps = candidate.jobs['test-portal-identity'].steps;
      const selected = steps.find(step => step.run?.includes(observer));
      if (mutation === 'missing') steps.splice(steps.indexOf(selected), 1);
      else if (mutation === 'conditional') selected.if = 'false';
      else selected['continue-on-error'] = true;
      assert.throws(() => mandatoryQualityChecks(candidate));
    });
  }
}

test('package grouping retains selected-file analysis and every HTML rule', () => {
  assert.deepEqual(mega, {
    GO_GOLANGCI_LINT_CLI_EXECUTABLE: ['go', 'run', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0'],
    GO_REVIVE_ARGUMENTS: ['./...'],
    GO_REVIVE_CLI_LINT_MODE: 'list_of_files',
    HTML_HTMLHINT_CONFIG_FILE: '.htmlhintrc',
    HTML_HTMLHINT_RULES_PATH: '/github/workspace',
    SPELL_CSPELL_CONFIG_FILE: '.cspell.json',
    SPELL_CSPELL_RULES_PATH: '/github/workspace',
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
