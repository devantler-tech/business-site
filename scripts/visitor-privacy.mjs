import assert from 'node:assert/strict';
import { parse } from 'parse5';

// This is the published content policy, not a general script sandbox or a
// legal assessment. Inspect parsed attributes rather than source spelling.
export function assertVisitorPrivacy(html, path) {
  const inspectDocument = (source, fallbackBase) => {
    const document = parse(source);
    const findBase = (node) => {
      if (node.tagName === 'base' && node.namespaceURI === 'http://www.w3.org/1999/xhtml') {
        const href = node.attrs.find((attribute) => attribute.name === 'href');
        if (href) return href.value;
      }
      for (const child of node.childNodes ?? []) {
        const href = findBase(child);
        if (href !== undefined) return href;
      }
    };
    const documentBase = new URL(findBase(document) ?? '', fallbackBase);
    const inspect = (node) => {
      for (const attribute of node.attrs ?? []) {
        assert.ok(!['data-website-id', 'data-umami-event'].includes(attribute.name),
          `${path}: visitor statistics collection remains inactive pending the privacy assessment`);
        if (node.tagName === 'script' && attribute.name === 'src') {
          assert.equal(new URL(attribute.value, documentBase).origin, 'https://devantler.tech',
            `${path}: automatic scripts stay on the website until external processing is assessed`);
        }
        if (node.tagName === 'iframe' && attribute.name === 'srcdoc') {
          inspectDocument(attribute.value, documentBase);
        }
      }
      for (const child of node.childNodes ?? []) inspect(child);
    };
    inspect(document);
  };
  inspectDocument(html, 'https://devantler.tech/');
}
