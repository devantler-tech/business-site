import assert from "node:assert/strict";

/** Compare the preserved editorial asset identity across each framework's different optimization hashes.
 * @param {string | undefined} candidate
 * @param {string | undefined} reference
 */
export function assertSameJournalCover(candidate, reference) {
  const identity = /** @param {string | undefined} src */ (src) => {
    assert.ok(src);
    const match = /(?:^|\/)(journal-[a-z0-9-]+)(?:\.[A-Za-z0-9_-]+)?\.(?:webp|jpg|avif)$/.exec(src);
    assert.ok(match, `unrecognized optimized Journal asset: ${src}`);
    return match[1];
  };
  assert.equal(identity(candidate), identity(reference), "post must retain its own authored editorial cover");
}
