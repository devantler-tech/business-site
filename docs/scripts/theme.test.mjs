import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

const script = readFileSync(new URL('../src/components/business/theme.js', import.meta.url), 'utf8');

// Only browser boundaries are simulated; every test executes the actual page script.
function page({ stored = null, light = false, blocked = false } = {}) {
  const values = new Map([['starlight-theme', stored]]);
  const root = { dataset: {}, style: {} };
  const listeners = new Map();
  const listen = (name, listener) => listeners.set(name, listener);
  const select = { value: 'auto', addEventListener: listen };
  const control = { hidden: true };
  const media = { matches: light, addEventListener(name, listener) {
    assert.equal(name, 'change');
    listeners.set('media-change', listener);
  } };
  const localStorage = {
    getItem(key) { if (blocked) throw new Error('Storage unavailable'); return values.get(key) ?? null; },
    setItem(key, value) { if (blocked) throw new Error('Storage unavailable'); values.set(key, value); },
  };
  const document = {
    documentElement: root, readyState: 'loading', addEventListener: listen,
    getElementById(id) { return { 'theme-select': select, 'theme-control': control }[id] ?? null; },
  };
  const window = {
    localStorage, addEventListener: listen,
    matchMedia(query) { assert.equal(query, '(prefers-color-scheme: light)'); return media; },
  };
  runInNewContext(script, { document, window });
  return {
    root, select, control, values,
    ready() { listeners.get('DOMContentLoaded')?.(); },
    choose(value) { select.value = value; listeners.get('change')?.({ currentTarget: select }); },
    system(value) { media.matches = value; listeners.get('media-change')?.(); },
    storage(value, key = 'starlight-theme') { values.set(key, value); listeners.get('storage')?.({ key, newValue: value }); },
    media,
  };
}

test('system preference is applied in the head, before controls exist', () => {
  const dark = page();
  assert.equal(dark.root.dataset.theme, 'dark');
  assert.equal(dark.root.style.colorScheme, 'dark');
  assert.equal(dark.control.hidden, true);
  dark.ready();
  assert.equal(dark.select.value, 'auto');
  assert.equal(dark.control.hidden, false);
  assert.equal(page({ light: true }).root.dataset.theme, 'light');
});

for (const choice of ['light', 'dark']) {
  test(`restores the documentation site's ${choice} preference`, () => {
    const view = page({ stored: choice, light: choice !== 'light' });
    assert.equal(view.root.dataset.theme, choice);
    view.ready();
    assert.equal(view.select.value, choice);
  });
}

for (const stored of ['', 'auto', 'unexpected', null]) {
  test(`treats ${JSON.stringify(stored)} as system mode`, () => {
    const view = page({ stored, light: true });
    assert.equal(view.root.dataset.theme, 'light');
    view.ready();
    assert.equal(view.select.value, 'auto');
  });
}

test('manual selection persists with the native documentation key and system representation', () => {
  const view = page();
  view.ready();
  view.choose('light');
  assert.equal(view.root.dataset.theme, 'light');
  assert.equal(view.root.style.colorScheme, 'light');
  assert.equal(view.values.get('starlight-theme'), 'light');
  view.choose('dark');
  assert.equal(view.root.dataset.theme, 'dark');
  assert.equal(view.values.get('starlight-theme'), 'dark');
  view.choose('auto');
  assert.equal(view.root.dataset.theme, 'dark');
  assert.equal(view.select.value, 'auto');
  assert.equal(view.values.get('starlight-theme'), '');
});

test('system changes are live but never override an explicit choice', () => {
  const view = page();
  view.ready();
  view.system(true);
  assert.equal(view.root.dataset.theme, 'light');
  view.choose('dark');
  view.system(false);
  view.system(true);
  assert.equal(view.root.dataset.theme, 'dark');
  view.choose('auto');
  assert.equal(view.root.dataset.theme, 'light');
  view.system(false);
  assert.equal(view.root.dataset.theme, 'dark');
});

test('blocked storage still allows switching and retains the in-page explicit choice', () => {
  const view = page({ blocked: true, light: true });
  view.ready();
  assert.equal(view.root.dataset.theme, 'light');
  view.choose('dark');
  view.system(false);
  view.system(true);
  assert.equal(view.root.dataset.theme, 'dark');
  view.choose('auto');
  assert.equal(view.root.dataset.theme, 'light');
});

test('a change from another tab synchronizes theme and control, unrelated storage does not', () => {
  const view = page();
  view.ready();
  view.storage('light');
  assert.equal(view.root.dataset.theme, 'light');
  assert.equal(view.select.value, 'light');
  view.storage('dark', 'unrelated');
  assert.equal(view.root.dataset.theme, 'light');
  view.storage('');
  assert.equal(view.root.dataset.theme, 'dark');
  assert.equal(view.select.value, 'auto');
});
