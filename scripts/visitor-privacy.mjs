import assert from 'node:assert/strict';
import { parse } from 'parse5';

// This is the published content policy, not a general script sandbox or a
// legal assessment. Inspect parsed attributes rather than source spelling.
export function assertVisitorPrivacy(html, path) {
  const inspect = (node) => {
    for (const attribute of node.attrs ?? []) {
      assert.ok(!['data-website-id', 'data-umami-event'].includes(attribute.name),
        `${path}: visitor statistics collection remains inactive pending the privacy assessment`);
      if (node.tagName === 'script' && attribute.name === 'src') {
        assert.equal(new URL(attribute.value, 'https://devantler.tech/').origin, 'https://devantler.tech',
          `${path}: automatic scripts stay on the website until external processing is assessed`);
      }
      if (node.tagName === 'iframe' && attribute.name === 'srcdoc') {
        inspect(parse(attribute.value));
      }
    }
    for (const child of node.childNodes ?? []) inspect(child);
  };
  inspect(parse(html));
}
