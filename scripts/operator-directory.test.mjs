import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import vm from 'node:vm';

const asset = readFileSync(new URL('../server/internal/identity/web/portal.js', import.meta.url), 'utf8');

function workspaceDialogFixture(supported) {
  const windowEvents = new Map();
  const cancelEvents = new Map();
  const triggerEvents = new Map();
  const attributes = new Map();
  let opened = false;
  let focused = '';
  const input = { focus: () => { focused = 'name'; } };
  const cancel = { addEventListener: (name, handler) => cancelEvents.set(name, handler) };
  const trigger = {
    form: { addEventListener: (name, handler) => triggerEvents.set(name, handler) },
    focus: () => { focused = 'trigger'; },
    setAttribute: (name, value) => attributes.set(name, value),
  };
  const dialog = {
    showModal: supported ? () => { opened = true; } : undefined,
    close: () => {
      opened = false;
      windowEvents.get('close')();
    },
    querySelector: selector => selector === '[name="display_name"]' ? input : cancel,
    addEventListener: (name, handler) => windowEvents.set(name, handler),
  };
  const document = {
    querySelector: selector => ({
      '[data-workspace-create]': trigger,
      '#create-workspace-dialog': dialog,
    })[selector],
    querySelectorAll: () => [],
  };
  vm.runInNewContext(asset, { document });
  function dispatch(events, name) {
    let prevented = false;
    events.get(name)?.({ preventDefault: () => { prevented = true; } });
    return prevented;
  }
  return {
    open: () => dispatch(triggerEvents, 'submit'),
    cancel: () => dispatch(cancelEvents, 'click'),
    state: () => ({ opened, focused, popup: attributes.get('aria-haspopup') }),
  };
}

test('the creation control opens one modal and returns focus after cancellation', () => {
  const page = workspaceDialogFixture(true);
  assert.equal(page.open(), true);
  assert.deepEqual(page.state(), { opened: true, focused: 'name', popup: 'dialog' });
  page.cancel();
  assert.deepEqual(page.state(), { opened: false, focused: 'trigger', popup: 'dialog' });
});

test('a browser without native dialog support keeps the server form link', () => {
  const page = workspaceDialogFixture(false);
  assert.equal(page.open(), false);
  assert.deepEqual(page.state(), { opened: false, focused: '', popup: undefined });
});
