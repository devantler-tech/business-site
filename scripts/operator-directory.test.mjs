import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const asset = readFileSync(new URL('../server/internal/identity/web/portal.js', import.meta.url), 'utf8');

function directoryFixture() {
  const member = (textContent) => ({
    hidden: false,
    querySelector: () => ({ textContent }),
  });
  const workspace = (textContent, open, members) => ({
    hidden: false, open,
    querySelector: () => ({ textContent }),
    querySelectorAll: () => members,
  });
  const rosa = member('Rosa r@example.invalid');
  const astrid = member('Astrid a@example.invalid');
  const robin = member('Robin robin@example.invalid');
  const studio = workspace('North Studio', false, [rosa, astrid]);
  const shop = workspace('South Shop', true, [robin]);
  const search = { hidden: true };
  const status = { textContent: '', dataset: { noMatches: 'No workspaces or members match your search.' } };
  let onInput;
  const input = {
    value: '',
    addEventListener: (type, listener) => {
      if (type === 'input') onInput = listener;
    },
  };
  const directory = {
    querySelector: (selector) => ({
      '[data-directory-search]': search,
      'input[type="search"]': input,
      '[data-directory-status]': status,
    })[selector],
    querySelectorAll: () => [studio, shop],
  };
  const document = {
    querySelector: (selector) => selector === '[data-workspace-directory]' ? directory : null,
    querySelectorAll: () => [],
  };
  vm.runInNewContext(asset, { document });
  return {
    search, status, studio, shop, rosa, astrid, robin,
    type: (value) => {
      assert.equal(typeof onInput, 'function', 'The served asset must bind search input');
      input.value = value;
      onInput();
    },
  };
}

test('member name or email reveals the matching customer and restores the original workspace state on clear', () => {
  const page = directoryFixture();
  assert.equal(page.search.hidden, false);
  page.type(' roSA ');
  assert.equal(page.studio.open, true);
  assert.equal(page.studio.hidden, false);
  assert.equal(page.rosa.hidden, false);
  assert.equal(page.astrid.hidden, true);
  assert.equal(page.shop.hidden, true);

  page.type('Robin@EXAMPLE');
  assert.equal(page.studio.hidden, true);
  assert.equal(page.shop.hidden, false);
  assert.equal(page.robin.hidden, false);

  page.type('');
  assert.equal(page.studio.open, false);
  assert.equal(page.shop.open, true);
  assert.equal(page.studio.hidden, false);
  assert.equal(page.shop.hidden, false);
  assert.equal(page.astrid.hidden, false);
});

test('a workspace match keeps its members visible and an empty result announces its status', () => {
  const page = directoryFixture();
  page.type('north studio');
  assert.equal(page.studio.hidden, false);
  assert.equal(page.rosa.hidden, false);
  assert.equal(page.astrid.hidden, false);
  assert.equal(page.shop.hidden, true);

  page.type('unlisted');
  assert.equal(page.studio.hidden, true);
  assert.equal(page.shop.hidden, true);
  assert.equal(page.status.textContent, page.status.dataset.noMatches);

  page.type('  ');
  assert.equal(page.status.textContent, '');
  assert.equal(page.studio.hidden, false);
  assert.equal(page.shop.hidden, false);
});
