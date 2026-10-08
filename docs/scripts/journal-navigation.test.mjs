import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

// Execute the actual inline head script. Only browser event/element boundaries
// are simulated; navigation, card selection and accessibility are production code.
const config = readFileSync(new URL('../astro.config.mjs', import.meta.url), 'utf8');
const script = config.match(/content:\s*`(document\.addEventListener\('DOMContentLoaded'[\s\S]*?)`,/)?.[1];
assert.ok(script, 'The journal navigation script must be wired into the page head');

class Element {
  constructor(tag, attributes = {}, parent = null) {
    this.tagName = tag;
    this.attributes = { ...attributes };
    this.parentElement = parent;
    this.children = [];
    this.listeners = new Map();
    parent?.children.push(this);
  }
  matches(selector) {
    return selector.split(',').some((part) => {
      const pieces = part.trim().split(/\s+/);
      const last = pieces.pop();
      const match = last.match(/^([a-z][a-z0-9]*)?(?:\.([\w-]+))?(?:\[([\w-]+)(?:=["']?([^"'\]]+)["']?)?\])?$/i);
      assert.ok(match, `Unimplemented browser selector boundary: ${last}`);
      const [, tag, className, attribute, value] = match;
      if (tag && tag !== this.tagName) return false;
      if (className && !this.attributes.class?.split(' ').includes(className)) return false;
      if (attribute && !(attribute in this.attributes)) return false;
      if (value !== undefined && this.attributes[attribute] !== value) return false;
      return !pieces.length || Boolean(this.parentElement?.closest(pieces.join(' ')));
    });
  }
  closest(selector) { return this.matches(selector) ? this : this.parentElement?.closest(selector) ?? null; }
  querySelector(selector) { return this.querySelectorAll(selector)[0] ?? null; }
  querySelectorAll(selector) {
    return this.children.flatMap((child) => [...(child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]);
  }
  setAttribute(name, value) { this.attributes[name] = value; }
  getAttribute(name) { return this.attributes[name] ?? null; }
  addEventListener(name, listener) { this.listeners.set(name, listener); }
}

function page({ card = true, destination = true, href = 'https://devantler.tech/blog/example/' } = {}) {
  const main = new Element('main');
  const article = new Element('article', card ? { class: 'sl-blog-preview' } : {}, main);
  const header = new Element('header', {}, article);
  // Starlight Blog wraps its title in the link, rather than placing an anchor in H2.
  const link = new Element('a', { class: 'sl-blog-preview-link' }, header);
  link.href = href;
  link.textContent = 'A useful journal post';
  const heading = new Element('h2', {}, link);
  const excerpt = new Element('p', {}, article);
  // Articles and excerpts may also contain heading permalinks. They are never
  // a substitute for a preview card's actual destination.
  const section = new Element('h2', {}, article);
  const sectionLink = new Element('a', {}, section);
  sectionLink.href = `${href}#details`;
  sectionLink.textContent = 'Details';
  if (!destination) header.children = [];
  const listeners = new Map();
  const document = {
    addEventListener(name, listener) { listeners.set(name, listener); },
    querySelectorAll(selector) { return main.querySelectorAll(selector); },
  };
  const window = { location: { href } };
  runInNewContext(script, { document, window });
  listeners.get('DOMContentLoaded')();
  const start = window.location.href;
  return {
    article, excerpt, heading, link, start, window,
    click(target) { listeners.get('click')({ target }); },
    key(target, key) {
      let prevented = false;
      article.listeners.get('keydown')?.({ target, key, code: key === ' ' ? 'Space' : key, preventDefault() { prevented = true; } });
      return prevented;
    },
  };
}

for (const href of ['https://devantler.tech/blog/example/', 'https://devantler.tech/da/blog/example/']) {
  test(`list-card excerpt clicks navigate to the real title link: ${href}`, () => {
    const view = page({ href });
    view.window.location.href = 'https://devantler.tech/blog/';
    view.click(view.excerpt);
    assert.equal(view.window.location.href, href);
    assert.equal(view.article.getAttribute('role'), null);
    assert.equal(view.article.getAttribute('tabindex'), null);
    assert.equal(view.article.getAttribute('aria-label'), null);
  });
  test(`list cards retain one native destination without a focusable link ancestor: ${href}`, () => {
    const view = page({ href });
    assert.equal(view.article.getAttribute('tabindex'), null, 'The native title link must not gain a duplicate ancestor focus stop');
    assert.equal(view.article.getAttribute('role'), null, 'The title anchor must not be nested inside another link role');
    assert.equal(view.link.href, href);
    assert.equal(view.link.getAttribute('tabindex'), null, 'The native title anchor stays keyboard reachable');
    assert.equal(view.link.listeners.size, 0, 'Native anchor keyboard activation must not be replaced');
  });
}

for (const key of ['Enter', ' ', 'Spacebar']) {
  test(`non-interactive card wrappers do not consume ${JSON.stringify(key)}`, () => {
    const view = page();
    view.window.location.href = 'https://devantler.tech/blog/';
    assert.equal(view.key(view.article, key), false);
    assert.equal(view.window.location.href, 'https://devantler.tech/blog/');
  });
}

for (const [tag, attributes] of [
  ['a', {}], ['button', {}], ['input', {}], ['select', {}], ['textarea', {}], ['summary', {}],
  ['div', { role: 'button' }], ['div', { role: 'link' }], ['div', { contenteditable: 'true' }],
]) {
  test(`list-card ${tag} ${JSON.stringify(attributes)} descendants keep their own interaction`, () => {
    const view = page();
    const control = new Element(tag, attributes, view.article);
    const child = new Element('span', {}, control);
    view.window.location.href = 'https://devantler.tech/blog/';
    view.click(child);
    assert.equal(view.window.location.href, 'https://devantler.tech/blog/');
    assert.equal(view.key(child, 'Enter'), false);
  });
}

for (const href of ['https://devantler.tech/blog/example/', 'https://devantler.tech/technical/example/']) {
  test(`individual articles never become list-card links: ${href}`, () => {
    const view = page({ card: false, href });
    const copy = new Element('button', {}, view.article);
    view.click(view.excerpt);
    view.click(copy);
    assert.equal(view.window.location.href, view.start);
    assert.equal(view.article.getAttribute('role'), null);
    assert.equal(view.article.getAttribute('tabindex'), null);
    assert.equal(view.key(view.article, 'Enter'), false);
  });
}

test('a card without a destination remains inert and does not consume keyboard input', () => {
  const view = page({ destination: false });
  view.window.location.href = 'https://devantler.tech/blog/';
  view.click(view.excerpt);
  assert.equal(view.window.location.href, 'https://devantler.tech/blog/');
  assert.equal(view.article.getAttribute('role'), null);
  assert.equal(view.article.getAttribute('tabindex'), null);
  assert.equal(view.key(view.article, 'Enter'), false);
});
