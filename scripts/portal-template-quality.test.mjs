import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { load } from 'js-yaml';
import manifest from '../package.json' with { type: 'json' };

const root = fileURLToPath(new URL('../', import.meta.url));
const configPath = join(root, '.htmlhintrc');
const rules = JSON.parse(readFileSync(configPath, 'utf8'));
const mega = load(readFileSync(join(root, '.mega-linter.yml'), 'utf8'));
const workflow = load(readFileSync(join(root, '.github/workflows/ci.yaml'), 'utf8'));

function mandatoryQualityChecks(candidate) {
  const job = candidate.jobs['test-portal-quality'];
  assert.equal(job.if, undefined);
  assert.deepEqual(job.permissions, { contents: 'read' });
  assert.ok(job['continue-on-error'] == null || job['continue-on-error'] === false);
  for (const command of [
    `npx --yes --package="htmlhint@$(node -p 'require("./package.json").devDependencies.htmlhint')" -- node --test scripts/portal-template-quality.test.mjs`,
    `npx --yes --package="cspell@$(node -p 'require("./package.json").devDependencies.cspell')" -- node --test scripts/portal-spelling-quality.test.mjs`,
    'node --test scripts/portal-go-analyzer-quality.test.mjs',
    'bash scripts/portal-links-preprocess.test.sh',
    'bash scripts/check-portal-links.sh --github-records',
    `npx --yes --package="secretlint@$(node -p 'require("./package.json").devDependencies.secretlint')" -- node --test scripts/portal-credential-quality.test.mjs`,
    'go install github.com/mgechev/revive@v1.16.0\nPORTAL_REVIVE="$GOBIN/revive" bash scripts/portal-package-quality.test.sh\n',
  ]) {
    const steps = job.steps.filter(step => step.run === command);
    assert.equal(steps.length, 1, 'the actual quality entrypoint must occur exactly once');
    assert.equal(steps[0]['working-directory'], '.');
    assert.equal(steps[0].if, undefined);
    assert.ok(steps[0]['continue-on-error'] == null || steps[0]['continue-on-error'] === false);
  }
  const githubRecords = job.steps.find(step => step.run === 'bash scripts/check-portal-links.sh --github-records');
  assert.deepEqual(githubRecords.env, { GITHUB_TOKEN: '${{ github.token }}' });
}

test('the actual source CI unconditionally executes every quality observer', () => mandatoryQualityChecks(workflow));
for (const observer of ['portal-template-quality.test.mjs', 'portal-spelling-quality.test.mjs', 'portal-package-quality.test.sh', 'portal-go-analyzer-quality.test.mjs', 'portal-credential-quality.test.mjs', 'portal-links-preprocess.test.sh', 'check-portal-links.sh']) {
  for (const mutation of ['missing', 'conditional', 'ignored failure']) {
    test(`${observer} cannot be ${mutation}`, () => {
      const candidate = structuredClone(workflow);
      const steps = candidate.jobs['test-portal-quality'].steps;
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
    REPOSITORY_OSV_SCANNER_CLI_EXECUTABLE: ['bash', '/github/workspace/scripts/scan-go-source.sh'],
    HTML_HTMLHINT_CONFIG_FILE: '.htmlhintrc',
    HTML_HTMLHINT_RULES_PATH: '/github/workspace',
    SPELL_CSPELL_CONFIG_FILE: '.cspell.json',
    SPELL_CSPELL_RULES_PATH: '/github/workspace',
    REPOSITORY_SECRETLINT_RULES_PATH: '/github/workspace',
    SPELL_LYCHEE_CLI_EXECUTABLE: ['bash', '/github/workspace/scripts/check-portal-links.sh'],
    SPELL_LYCHEE_ARGUMENTS: [
      '--preprocess', '/github/workspace/scripts/portal-links-preprocess.sh',
      '--host-concurrency', '2', '--host-request-interval', '500ms',
      '--remap',
      '^https://github\\.com/devantler-tech/business-site/blob/([a-f0-9]{40})/(server/[A-Za-z0-9._/-]+\\.md)$ ' +
        'https://raw.githubusercontent.com/devantler-tech/business-site/$1/$2',
      '--remap',
      '^https://github\\.com/bufbuild/buf/blob/v1\\.72\\.0/private/bufpkg/bufmodule/digest\\.go$ ' +
        'https://raw.githubusercontent.com/bufbuild/buf/v1.72.0/private/bufpkg/bufmodule/digest.go',
      '--remap',
      '^https://github\\.com/devantler-tech/(business-site|monorepo)/issues/([1-9][0-9]*)$ ' +
        'https://api.github.com/repos/devantler-tech/$1/issues/$2',
      '--remap',
      '^https://github\\.com/devantler-tech/(business-site|monorepo)/pull/([1-9][0-9]*)$ ' +
        'https://api.github.com/repos/devantler-tech/$1/pulls/$2',
      '--remap',
      '^https://github\\.com/justinas/nosurf/security/advisories/(GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$ ' +
        'https://api.github.com/repos/justinas/nosurf/security-advisories/$1',
      '--exclude', '^https://www\\.retsinformation\\.dk/',
    ],
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

test('plain GitHub references keep the same record in the API checker', () => {
  const args = mega.SPELL_LYCHEE_ARGUMENTS;
  const remaps = args.flatMap((flag, index) => {
    if (flag !== '--remap') return [];
    const [pattern, target] = args[index + 1].split(' ');
    return [{ pattern: new RegExp(pattern), target }];
  });
  for (const [url, expected] of [
    ['https://github.com/devantler-tech/business-site/issues/659', 'https://api.github.com/repos/devantler-tech/business-site/issues/659'],
    ['https://github.com/devantler-tech/monorepo/issues/3981', 'https://api.github.com/repos/devantler-tech/monorepo/issues/3981'],
    ['https://github.com/devantler-tech/business-site/pull/621', 'https://api.github.com/repos/devantler-tech/business-site/pulls/621'],
    ['https://github.com/justinas/nosurf/security/advisories/GHSA-w9hf-35q4-vcjw',
      'https://api.github.com/repos/justinas/nosurf/security-advisories/GHSA-w9hf-35q4-vcjw'],
    ['https://github.com/devantler-tech/business-site/issues/659#issuecomment-1'],
    ['https://github.com/devantler-tech/business-site/pull/621?diff=split'],
    ['https://github.com/example/business-site/issues/659'],
    ['https://github.com/devantler-tech/business-site/issues/0'],
  ]) {
    const remap = remaps.find(({ pattern }) => pattern.test(url));
    assert.equal(remap ? url.replace(remap.pattern, remap.target) : url, expected ?? url);
  }
});

test('GitHub records are authenticated once and ordinary links still use Lychee', t => {
  const directory = mkdtempSync(join(tmpdir(), 'portal-link-check-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const trace = join(directory, 'trace');
  const record = 'https://api.github.com/repos/devantler-tech/business-site/issues/659';
  const credential = 'fixture-read-token';
  writeFileSync(join(directory, 'lychee'), `#!/bin/bash
set -euo pipefail
case " $* " in
  *" --dump "*)
    test -z "\${GITHUB_TOKEN:-}"
    printf 'discover\\n' >> "$LINK_TEST_TRACE"
    printf '%s\\n' "$LINK_TEST_RECORD"
    if [[ -n "$LINK_TEST_DISCOVERY_WARNING" ]]; then printf '%s\\n' "$LINK_TEST_DISCOVERY_WARNING" >&2; fi
    ;;
  *" --exclude "*) printf 'check\\n' >> "$LINK_TEST_TRACE"; exit "$LINK_TEST_ORDINARY_STATUS" ;;
  *) printf 'anonymous\\n' >> "$LINK_TEST_TRACE" ;;
esac
`, { mode: 0o700 });
  writeFileSync(join(directory, 'curl'), `#!/bin/bash
set -euo pipefail
test "$1 $2 $3" = "--disable --config -"
test -z "\${GITHUB_TOKEN:-}"
test "\${@: -1}" = "${record}"
case "$*" in
  *" --proto =https "*) ;;
  *) exit 70 ;;
esac
case "$*" in
  *${credential}*|*" --location "*) exit 71 ;;
esac
IFS= read -r config
test "$config" = 'header = "Authorization: Bearer ${credential}"'
printf 'github\\n' >> "$LINK_TEST_TRACE"
printf '%s' "$LINK_TEST_HTTP_STATUS"
`, { mode: 0o700 });

  const run = (overrides = {}, mode = ['--github-records']) => {
    writeFileSync(trace, '');
    const result = spawnSync('bash', [join(root, 'scripts/check-portal-links.sh'), ...mode, 'fixture.md'], {
      cwd: root, encoding: 'utf8', timeout: 15000,
      env: {
        ...process.env,
        PATH: `${directory}:${process.env.PATH}`,
        GITHUB_TOKEN: credential,
        LINK_TEST_TRACE: trace,
        LINK_TEST_RECORD: `${record}\nfile:///fixture/related.md\nhttps://example.invalid/issue/659\n${record}`,
        LINK_TEST_DISCOVERY_WARNING: '',
        LINK_TEST_HTTP_STATUS: '200',
        LINK_TEST_ORDINARY_STATUS: '0',
        ...overrides,
      },
    });
    assert.ifError(result.error);
    assert.equal(result.signal, null);
    assert.ok(!(result.stdout + result.stderr).includes(credential));
    return { status: result.status, trace: readFileSync(trace, 'utf8').trim().split('\n').filter(Boolean) };
  };
  assert.deepEqual(run(), { status: 0, trace: ['discover', 'github'] });
  assert.deepEqual(run({ LINK_TEST_DISCOVERY_WARNING: 'preprocess: skipped fixture input' }), { status: 1, trace: ['discover'] });
  assert.deepEqual(run({ LINK_TEST_HTTP_STATUS: '404' }), { status: 1, trace: ['discover', 'github'] });
  assert.deepEqual(run({ LINK_TEST_RECORD: 'https://example.invalid/issue/659' }), { status: 0, trace: ['discover'] });
  assert.deepEqual(run({ GITHUB_TOKEN: '' }), { status: 1, trace: [] });
  assert.deepEqual(run({ GITHUB_TOKEN: 'HIDDEN_BY_MEGALINTER' }), { status: 1, trace: [] });
  assert.deepEqual(run({ GITHUB_TOKEN: 'HIDDEN_BY_MEGALINTER' }, []), { status: 0, trace: ['check'] });
  assert.deepEqual(run({ LINK_TEST_ORDINARY_STATUS: '1' }, []), { status: 1, trace: ['check'] });
});

function lint(files) {
  const result = spawnSync('htmlhint', ['--config', configPath, ...files], {
    cwd: root, encoding: 'utf8', timeout: 30000,
  });
  assert.ifError(result.error);
  assert.equal(result.signal, null);
  return { status: result.status, output: result.stdout + result.stderr };
}

test('the pinned checker analyzes all eight actual Go HTML templates', () => {
  const version = spawnSync('htmlhint', ['--version'], { encoding: 'utf8', timeout: 30000 });
  assert.ifError(version.error);
  assert.equal(version.status, 0);
  assert.equal(version.stdout.trim(), manifest.devDependencies.htmlhint);
  const result = lint(['portal', 'operator', 'operator-workspaces', 'operator-customers', 'contact', 'requests', 'triage', 'work-review'].map(name =>
    join(root, 'server/internal/identity/web', `${name}.html`)));
  assert.equal(result.status, 0, result.output);
  assert.match(result.output, /Scanned 8 files/);
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
