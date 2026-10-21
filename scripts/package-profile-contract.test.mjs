import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';

test('actual default packager pins safe presentation inputs rather than inheriting dotenv values', () => {
  const root = mkdtempSync(path.join(tmpdir(), 'package-profile-'));
  try {
    mkdirSync(path.join(root, 'scripts')); mkdirSync(path.join(root, 'bin'));
    cpSync('scripts/package-runtime.sh', path.join(root, 'scripts/package-runtime.sh'));
    cpSync('.node-version', path.join(root, '.node-version'));
    writeFileSync(path.join(root, 'bin/git'), '#!/bin/sh\nprintf "%s\\n" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n', { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/node'), '#!/bin/sh\nprintf "v24.19.0\\n"\n', { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/go'), '#!/bin/sh\nprintf "go version go1.27.3 linux/amd64\\n"\n', { mode: 0o700 });
    writeFileSync(path.join(root, 'bin/npm'), `#!/bin/bash
if [[ "$1" == --version ]]; then printf '11.19.1\\n'; exit 0; fi
"${process.execPath}" -e 'require("node:fs").writeFileSync(process.env.CAPTURE, JSON.stringify({profile:process.env.PUBLIC_CONTENT_PROFILE,company:process.env.FEATURE_COMPANY_IDENTITY,address:process.env.COMPANY_POSTAL_ADDRESS,preview:process.env.FEATURE_PREVIEW_BANNER,offer:process.env.FEATURE_OFFER_COPY,customer:process.env.FEATURE_CUSTOMER_SCOPE,vat:process.env.FEATURE_VAT_PRICES,frontend:process.env.FEATURE_SVELTE_FRONTEND}))'
exit 42
`, { mode: 0o700 });
    const capture = path.join(root, 'captured.json');
    const boundaryEnv = { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, CAPTURE: capture };
    for (const declaration of [null, 'latest\n', '26\n']) {
      if (declaration === null) rmSync(path.join(root, '.node-version'));
      else writeFileSync(path.join(root, '.node-version'), declaration);
      const rejected = spawnSync('bash', ['scripts/package-runtime.sh', '--development'], {
        cwd: root, timeout: 10000, env: boundaryEnv,
      });
      assert.equal(rejected.status, 1, `Reject missing, malformed or mismatched Node declaration: ${declaration}`);
      assert.equal(existsSync(capture), false, 'Never invoke the build with an unsupported runtime');
    }
    cpSync('.node-version', path.join(root, '.node-version'));
    const result = spawnSync('bash', ['scripts/package-runtime.sh', '--development'], {
      cwd: root, timeout: 10000,
      env: { ...process.env, PATH: `${root}/bin:${process.env.PATH}`, CAPTURE: capture,
        COMPANY_POSTAL_ADDRESS: 'Fictional private input', PUBLIC_CONTENT_PROFILE: 'production-template', FEATURE_COMPANY_IDENTITY: 'true', FEATURE_OFFER_COPY: 'true', FEATURE_PREVIEW_BANNER: 'true', FEATURE_CUSTOMER_SCOPE: 'true', FEATURE_VAT_PRICES: 'true', FEATURE_SVELTE_FRONTEND: 'true' },
    });
    assert.equal(result.status, 42, 'The boundary double stops immediately after capturing the actual build invocation');
    assert.deepEqual(JSON.parse(readFileSync(capture)), { profile: 'default', company: 'false', address: '', preview: 'false', offer: 'false', customer: 'false', vat: 'false', frontend:'false' });
    const selected = spawnSync('bash',['scripts/package-runtime.sh','--development','--default','--svelte'], {cwd:root,timeout:10000,env:boundaryEnv});
    assert.equal(selected.status,42);
    assert.equal(JSON.parse(readFileSync(capture)).frontend,'true','Only the explicit package option selects Svelte');
    writeFileSync(path.join(root, 'scripts/refresh-public-stars.sh'), '#!/bin/sh\nprintf "{}" > "$1"\n');
    const production = spawnSync('bash', ['scripts/package-runtime.sh', '--development', '--production-template', '--svelte'], {
      cwd: root, timeout: 10000, env: boundaryEnv,
    });
    assert.equal(production.status, 42, 'The production boundary double stops at the real build invocation');
    const released = JSON.parse(readFileSync(capture));
    assert.deepEqual(released, { profile: 'production-template', company: 'true', address: '', preview: 'false',
      offer: 'false', customer: 'false', vat: 'true', frontend: 'true' });
  } finally { rmSync(root, { recursive: true }); }
});
