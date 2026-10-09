import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import postcss from 'postcss';

const css = postcss.parse(readFileSync(new URL('../src/styles/journal.css', import.meta.url), 'utf8'));
const browserList = '.journal-page .journal-browser > sl-sidebar-state-persist > ul';
const sidebar = '.journal-page .right-sidebar-container';

/** Project this stylesheet's exact-selector declarations at a viewport, including media bounds. */
function declarations(selector, width) {
  const result = {};
  css.walkRules(rule => {
    if (rule.selector !== selector) return;
    for (let parent = rule.parent; parent; parent = parent.parent) {
      if (parent.type !== 'atrule') continue;
      assert.equal(parent.name, 'media', 'extend the projection when other conditional rules are added');
      const bound = parent.params.match(/^\((min|max)-width: (\d+)(px|rem)\)$/);
      assert.ok(bound, 'every media condition must be examined');
      const limit = Number(bound[2]) * (bound[3] === 'rem' ? 16 : 1);
      if (bound[1] === 'min' ? width < limit : width > limit) return;
    }
    rule.walkDecls(declaration => { result[declaration.prop] = declaration.value; });
  });
  return result;
}

test('the browse grid targets the actual installed Starlight persister wrapper', () => {
  const persister = readFileSync(new URL('../node_modules/@astrojs/starlight/dist/components/SidebarPersister.astro', import.meta.url), 'utf8');
  assert.match(persister, /<sl-sidebar-state-persist\b/);
  assert.equal(declarations(browserList, 1280).display, 'grid');
  assert.equal(declarations(browserList, 1280)['grid-template-columns'], 'repeat(3, minmax(0, 1fr))');
});

test('browsing becomes a single column on a narrow screen', () => {
  assert.equal(declarations(browserList, 390).display, 'grid');
  assert.equal(declarations(browserList, 390)['grid-template-columns'], '1fr');
});

test('only the desktop contents rail is suppressed; mobile contents remain available', () => {
  const installed = readFileSync(new URL('../node_modules/@astrojs/starlight/dist/components/TwoColumnContent.astro', import.meta.url), 'utf8');
  assert.match(installed, /@media \(min-width: 72rem\)/);
  assert.notEqual(declarations(sidebar, 390).display, 'none');
  assert.notEqual(declarations(sidebar, 1151).display, 'none');
  assert.equal(declarations(sidebar, 1152).display, 'none');
});
