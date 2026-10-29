import assert from "node:assert/strict";
import { load, YAML11_SCHEMA } from "js-yaml";

/** YAML metadata with native timestamps, shared by every authored collection.
 * Values remain untrusted until the collection validates its own schema.
 * @param {string} source
 * @returns {{frontmatter:Record<string,any>,rawFrontmatter:string,content:string}}
 */
export function parseFrontmatter(source) {
  const match = source.match(/^\uFEFF?---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  if (!match) return {frontmatter:{}, rawFrontmatter:"", content:source};
  const frontmatter = load(match[1], {schema:YAML11_SCHEMA});
  assert.ok(frontmatter && typeof frontmatter === "object" && !Array.isArray(frontmatter), "Metadata object required");
  return {frontmatter, rawFrontmatter:match[1], content:source.slice(match[0].length)};
}
