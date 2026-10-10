import assert from 'node:assert/strict';
import { parse } from 'parse5';

// This is a content-policy assertion, not a URL security sanitizer. Read the
// actual destination, including HTML references and browser URL normalization.
export function assertNoRetiredRepositoryLinks(html) {
  const inspect = (node) => {
    for (const attribute of node.attrs ?? []) {
      if (attribute.name !== 'href') continue;
      const target = new URL(attribute.value, 'https://devantler.tech/');
      if (target.hostname !== 'github.com') continue;
      const path = decodeURIComponent(target.pathname).toLowerCase();
      assert.ok(!/^\/devantler-tech\/reusable-workflows(?:\/|$)/.test(path),
        'Rendered public pages must not link to retired repositories');
    }
    for (const child of node.childNodes ?? []) inspect(child);
  };
  inspect(parse(html));
}
